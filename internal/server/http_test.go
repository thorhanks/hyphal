package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/keymap"
	"github.com/thorhanks/hyphal/internal/testgen"
)

// httpServer starts a server for a fresh session on a loopback port.
func httpServer(t *testing.T, now *time.Time) (*Server, string) {
	t.Helper()
	return httpServerWith(t, now, SessionOptions{})
}

func httpServerWith(t *testing.T, now *time.Time, so SessionOptions) (*Server, string) {
	t.Helper()
	db := testgen.TempDB(t)
	ctx := context.Background()
	clock := func() time.Time { return *now }
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Clock: clock, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(nil)
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	so.Clock = clock
	s := New(NewSession(o, db, so), []byte(strings.Repeat("k", 32)), port, false, clock)
	ts.Config.Handler = s.Handler()
	ts.Start()
	stop, cancel := context.WithCancel(ctx)
	s.stop = stop
	go func() {
		for {
			select {
			case f := <-s.in:
				f()
			case <-stop.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { ts.Close(); cancel() })
	return s, "127.0.0.1:" + strconv.Itoa(port)
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// AC1: loopback only; bad Host, Origin, ticket or cookie is refused; a ticket
// works once; the cookie is HttpOnly and SameSite=Strict; CSP is set.
func TestHTTP_Security(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	s, host := httpServer(t, &now)
	client := &http.Client{CheckRedirect: noRedirect}
	get := func(t *testing.T, path, hostHeader string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "http://"+host+path, nil)
		if hostHeader != "" {
			req.Host = hostHeader
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res
	}

	ticket := s.NewTicket()
	res := get(t, "/?ticket="+ticket, "", nil)
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("ticket exchange: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == s.CookieName() {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 365*24*3600 {
		t.Fatalf("cookie %+v", cookie)
	}
	if s.CookieName() != "hyphal_"+strings.Split(host, ":")[1] {
		t.Errorf("cookie name %q doesn't name the port", s.CookieName())
	}
	forged := &http.Cookie{Name: cookie.Name, Value: strings.Split(cookie.Value, ".")[0] + ".AAAA"}
	stale := &http.Cookie{Name: cookie.Name, Value: (&auth{key: s.auth.key, port: s.port, clock: func() time.Time { return now.Add(-366 * 24 * time.Hour) }}).cookieValue()}
	expired := s.NewTicket()

	cases := []struct {
		name, path, host string
		cookie           *http.Cookie
		want             int
		before           func()
	}{
		{"page with cookie", "/", "", cookie, 200, nil},
		{"asset with cookie", "/assets/base.css", "", cookie, 200, nil},
		{"localhost host", "/", "localhost:" + strings.Split(host, ":")[1], cookie, 200, nil},
		{"no cookie", "/", "", nil, 403, nil},
		{"forged cookie", "/", "", forged, 403, nil},
		{"cookie older than a year", "/", "", stale, 403, nil},
		{"bad host", "/", "evil.example:80", cookie, 403, nil},
		{"rebinding host", "/", "attacker.test:" + strings.Split(host, ":")[1], cookie, 403, nil},
		{"wrong port", "/", "127.0.0.1:1", cookie, 403, nil},
		{"ticket used twice", "/?ticket=" + ticket, "", nil, 403, nil},
		{"unknown ticket", "/?ticket=00ff", "", nil, 403, nil},
		{"ticket after 30 s", "/?ticket=" + expired, "", nil, 403, func() { now = now.Add(31 * time.Second) }},
		{"missing asset", "/assets/nope.js", "", cookie, 404, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.before != nil {
				c.before()
			}
			res := get(t, c.path, c.host, c.cookie)
			if res.StatusCode != c.want {
				t.Errorf("status %d, want %d", res.StatusCode, c.want)
			}
			if res.Header.Get("Content-Security-Policy") != csp {
				t.Errorf("CSP %q", res.Header.Get("Content-Security-Policy"))
			}
		})
	}
	now = now.Add(-31 * time.Second)

	t.Run("ticket endpoint needs the secret", func(t *testing.T) {
		post := func(body, origin string) int {
			req, _ := http.NewRequest(http.MethodPost, "http://"+host+"/internal/ticket", strings.NewReader(body))
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			return res.StatusCode
		}
		if got := post(s.Secret(), ""); got != 200 {
			t.Errorf("with the secret: %d", got)
		}
		if got := post("wrong", ""); got != 403 {
			t.Errorf("wrong secret: %d", got)
		}
		if got := post(s.Secret(), "http://evil.example"); got != 403 {
			t.Errorf("cross-site origin: %d", got)
		}
	})

	t.Run("websocket checks origin and cookie", func(t *testing.T) {
		dial := func(origin string, c *http.Cookie) error {
			h := http.Header{}
			if origin != "" {
				h.Set("Origin", origin)
			}
			if c != nil {
				h.Set("Cookie", c.String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ws, _, err := websocket.Dial(ctx, (&url.URL{Scheme: "ws", Host: host, Path: "/ws"}).String(), &websocket.DialOptions{HTTPHeader: h})
			if err == nil {
				_ = ws.CloseNow()
			}
			return err
		}
		if err := dial("http://"+host, cookie); err != nil {
			t.Errorf("same origin with cookie refused: %v", err)
		}
		if dial("http://evil.example", cookie) == nil {
			t.Error("cross-site origin accepted")
		}
		if dial("", cookie) == nil {
			t.Error("missing origin accepted")
		}
		if dial("http://"+host, nil) == nil {
			t.Error("no cookie accepted")
		}
	})
}

// The server listens on loopback only, at the database's stable port (§9.3).
func TestListen_LoopbackStablePort(t *testing.T) {
	path := "/tmp/some/outline.db"
	l, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	a := l.Addr().(*net.TCPAddr)
	if !a.IP.IsLoopback() || a.IP.To4() == nil {
		t.Errorf("listening on %v", a.IP)
	}
	if a.Port != firstPort(path) {
		t.Errorf("port %d, want %d", a.Port, firstPort(path))
	}
	// The next server for the same path takes the next port.
	l2, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l2.Close() }()
	if got := l2.Addr().(*net.TCPAddr).Port; got != firstPort(path)+1 {
		t.Errorf("fallback port %d", got)
	}
}

// /keyprobe and the dev assets exist only with --dev in a dev build
// (§6.4 "Human key matrix").
func TestHTTP_KeyprobeNeedsDev(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	get := func(t *testing.T, s *Server, host, path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "http://"+host+path, nil)
		req.AddCookie(&http.Cookie{Name: s.auth.cookieName(), Value: s.auth.cookieValue()})
		res, err := (&http.Client{CheckRedirect: noRedirect}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = res.Body.Close() })
		return res
	}
	s, host := httpServer(t, &now)
	for _, p := range []string{"/keyprobe", "/keyprobe.json", "/assets/dev/keyprobe.html", "/assets/dev/hooks.js"} {
		if res := get(t, s, host, p); res.StatusCode != http.StatusNotFound {
			t.Errorf("without --dev, %s: %d", p, res.StatusCode)
		}
	}

	s, host = httpServerWith(t, &now, SessionOptions{Dev: true, Platform: keymap.MacOS})
	if res := get(t, s, host, "/keyprobe"); res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/assets/dev/keyprobe.html" {
		t.Errorf("/keyprobe: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res := get(t, s, host, "/keyprobe.json")
	var probe struct {
		Platform string            `json:"platform"`
		Reserved []string          `json:"reserved"`
		Keymap   []json.RawMessage `json:"keymap"`
	}
	if err := json.NewDecoder(res.Body).Decode(&probe); err != nil {
		t.Fatal(err)
	}
	if probe.Platform != keymap.MacOS || !slices.Contains(probe.Reserved, "meta+w") || len(probe.Keymap) != len(keymap.Defaults().Commands()) {
		t.Errorf("keyprobe.json: %s, %v, %d commands", probe.Platform, probe.Reserved, len(probe.Keymap))
	}
}
