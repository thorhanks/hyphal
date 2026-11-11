// Package server serves one database to browser windows on loopback: HTTP,
// auth, the WebSocket protocol and the editing session (§9.3, CONTRACTS §12,
// ARCHITECTURE §5).
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/keymap"
	"github.com/thorhanks/hyphal/internal/platform"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/web"
)

// Timings of the session goroutine (ARCHITECTURE §5).
const (
	tickEvery  = 250 * time.Millisecond
	sendBuffer = 256
)

// Options configures Run.
type Options struct {
	DBPath     string // absolute; default DefaultDB
	DataDir    string // session.key; default DataDir
	RuntimeDir string // the state file; default RuntimeDir
	Version    string
	Dev        bool      // --dev: no-store caching; with the dev build tag also window.__hyphal
	Seed       io.Reader // dev build only: a JSON export that replaces the database's contents
	Platform   string    // dev build only: resolve the keymap for keymap.Linux or keymap.MacOS
	Stdout     io.Writer // receives "URL <launch URL>"; default os.Stdout
	Clock      func() time.Time
}

// stateFile is the server state file's content (CONTRACTS §9, D-041).
type stateFile struct {
	Port    int    `json:"port"`
	Secret  string `json:"secret"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

// Server is the HTTP side of one running server.
type Server struct {
	auth    *auth
	port    int
	dev     bool
	assets  fs.FS
	etags   map[string]string
	in      chan func() // runs on the session goroutine
	session *Session
	stop    context.Context
}

// Run serves the database until ctx is done, then seals typing and exits
// (§9.3). It prints the first launch URL, holding a ticket, on Stdout.
func Run(ctx context.Context, o Options) error {
	if err := o.defaults(); err != nil {
		return err
	}
	if o.Seed != nil && !devBuild {
		return errors.New("--seed needs a dev build")
	}
	if o.Platform != "" && !devBuild {
		return errors.New("--platform needs a dev build")
	}
	if o.Platform != "" && o.Platform != keymap.Linux && o.Platform != keymap.MacOS {
		return fmt.Errorf("--platform %q: want %s or %s", o.Platform, keymap.Linux, keymap.MacOS)
	}
	if err := ensureDir(filepath.Dir(o.DBPath)); err != nil {
		return err
	}
	db, err := store.Open(o.DBPath, store.OpenOptions{Create: true, Clock: o.Clock})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if o.Seed != nil {
		if err := db.ImportJSON(ctx, o.Seed, store.ImportReplace); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	outline, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Clock: o.Clock, Sync: true})
	if err != nil {
		return err
	}
	if err := outline.LoadPages(ctx); err != nil { // TODO(P1-19): load pages after the first paint
		return err
	}
	key, err := loadKey(o.DataDir)
	if err != nil {
		return err
	}
	l, err := listen(o.DBPath)
	if err != nil {
		return err
	}
	port := l.Addr().(*net.TCPAddr).Port
	s := New(NewSession(outline, db, SessionOptions{Version: o.Version, Clock: o.Clock, Dev: o.Dev && devBuild, Platform: o.Platform}),
		key, port, o.Dev, o.Clock)
	s.stop = ctx

	state := StateFile(o.RuntimeDir, o.DBPath)
	if err := writeState(state, stateFile{port, s.auth.secret, os.Getpid(), o.Version}); err != nil {
		_ = l.Close()
		return err
	}
	defer func() { _ = os.Remove(state) }()

	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(l) }()
	if _, err := fmt.Fprintf(o.Stdout, "URL http://127.0.0.1:%d/?ticket=%s\n", port, s.auth.newTicket()); err != nil {
		_ = hs.Close()
		return err
	}

	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case f := <-s.in:
			f()
		case <-ticker.C:
			s.session.Tick(ctx, o.Clock())
		case err := <-served:
			return err
		case <-ctx.Done():
			// TODO(P1-30): exit 10 s after the last tab closes; drain with a 3 s limit.
			return s.shutdown(ctx, hs)
		}
	}
}

// shutdown stops serving and seals open typing. Shutdown waits for sockets'
// readers, which wait on the session goroutine, so it runs here while the
// session's calls are refused through s.stop.
func (s *Server) shutdown(ctx context.Context, hs *http.Server) error {
	shut, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = hs.Shutdown(shut)
	return s.session.Close(context.WithoutCancel(ctx))
}

func (o *Options) defaults() error {
	var err error
	if o.DBPath == "" {
		if o.DBPath, err = DefaultDB(); err != nil {
			return err
		}
	}
	if o.DataDir == "" {
		if o.DataDir, err = DataDir(); err != nil {
			return err
		}
	}
	if o.RuntimeDir == "" {
		o.RuntimeDir = RuntimeDir(o.DataDir)
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	return nil
}

// listen binds 127.0.0.1 on the database's stable port, or the next free one
// of 16 (§9.3, D-047).
func listen(dbPath string) (net.Listener, error) {
	first := firstPort(dbPath)
	for i := range portTries {
		port := portBase + (first-portBase+i)%portSpan
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return l, nil
		}
	}
	return nil, errNoPort
}

func writeState(path string, st stateFile) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, 0o600, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(st)
	})
}

// New makes a server for a session. Run uses it; HTTP tests use it with
// httptest. The session's calls go through Server.in, which Run drains.
func New(sess *Session, key []byte, port int, dev bool, clock func() time.Time) *Server {
	if clock == nil {
		clock = time.Now
	}
	s := &Server{
		auth:    &auth{key: key, secret: randomHex(32), port: port, clock: clock, tickets: map[string]time.Time{}},
		port:    port,
		dev:     dev,
		assets:  web.Assets(),
		etags:   map[string]string{},
		in:      make(chan func()),
		session: sess,
		stop:    context.Background(),
	}
	_ = fs.WalkDir(s.assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(s.assets, p)
			sum := sha256.Sum256(b)
			s.etags[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	return s
}

// Calls returns the channel of session calls; tests that use New without Run
// drain it themselves.
func (s *Server) Calls() <-chan func() { return s.in }

// NewTicket returns a launch ticket, as /internal/ticket does.
func (s *Server) NewTicket() string { return s.auth.newTicket() }

// Secret returns the secret that /internal/ticket accepts.
func (s *Server) Secret() string { return s.auth.secret }

// CookieName is the session cookie's name (D-047).
func (s *Server) CookieName() string { return s.auth.cookieName() }

func (s *Server) hostOK(h string) bool {
	p := strconv.Itoa(s.port)
	return h == "127.0.0.1:"+p || h == "localhost:"+p
}

// originOK checks Origin: required on /ws, and must match when present
// elsewhere (CONTRACTS §12.4).
func (s *Server) originOK(r *http.Request, required bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return !required
	}
	rest, ok := strings.CutPrefix(o, "http://")
	return ok && s.hostOK(rest)
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie(s.auth.cookieName())
	return err == nil && s.auth.cookieOK(c.Value)
}

// Handler serves every route of CONTRACTS §12.4 that exists so far.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if !s.hostOK(r.Host) {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/internal/ticket" {
			s.serveTicket(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if t := r.URL.Query().Get("ticket"); t != "" && r.URL.Path == "/" {
			if !s.auth.useTicket(t) {
				s.refuse(w)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: s.auth.cookieName(), Value: s.auth.cookieValue(), Path: "/",
				MaxAge: int(cookieLife / time.Second), HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if !s.authed(r) {
			s.refuse(w)
			return
		}
		if r.URL.Path == "/keyprobe" || r.URL.Path == "/keyprobe.json" {
			s.serveKeyprobe(w, r)
			return
		}
		if r.URL.Path == "/ws" {
			if !s.originOK(r, true) {
				http.Error(w, "bad origin", http.StatusForbidden)
				return
			}
			s.serveWS(w, r)
			return
		}
		s.serveAsset(w, r)
	})
}

// refuse answers a request without a valid ticket or cookie (D-047).
func (s *Server) refuse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, msgNoSession+"\n")
}

// serveTicket trades the state file's secret for a launch ticket (D-041).
func (s *Server) serveTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.originOK(r, false) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil || !s.auth.secretOK(strings.TrimSpace(string(b))) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, s.auth.newTicket())
}

// serveKeyprobe serves the key matrix page and its data, in dev builds run
// with --dev only (§6.4 "Human key matrix", docs/keyprobe.md). The page
// lives with the other dev assets, so its relative links resolve.
func (s *Server) serveKeyprobe(w http.ResponseWriter, r *http.Request) {
	if !s.session.opts.Dev {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/keyprobe" {
		http.Redirect(w, r, "/assets/dev/keyprobe.html", http.StatusFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	keys := s.session.Keymap()
	reserved := make([]string, len(keymap.Reserved))
	for i, k := range keymap.Reserved {
		reserved[i] = keymap.Resolve(k, keys.Platform())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Platform string          `json:"platform"`
		Keymap   json.RawMessage `json:"keymap"`
		Reserved []string        `json:"reserved"`
	}{keys.Platform(), s.session.keyJSON, reserved})
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := "index.html"
	if p, ok := strings.CutPrefix(r.URL.Path, "/assets/"); ok {
		name = path.Clean(p)
	} else if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(s.assets, name)
	if err != nil || (strings.HasPrefix(name, "dev/") && !s.session.opts.Dev) {
		http.NotFound(w, r)
		return
	}
	if s.dev {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", s.etags[name])
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// serveWS runs one tab's socket: a reader that hands messages to the session
// goroutine and a writer that drains the tab's send queue (ARCHITECTURE §5).
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{r.Host}})
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	q := newQueue()
	var win *Window
	if !s.call(ctx, func() { win = s.session.Join(q.push) }) {
		_ = ws.CloseNow()
		return
	}
	defer s.call(context.WithoutCancel(ctx), func() { s.session.Leave(context.WithoutCancel(ctx), win) })
	go func() {
		defer cancel()
		for {
			b, ok := q.pop(ctx)
			if !ok || ws.Write(ctx, websocket.MessageText, b) != nil {
				return
			}
		}
	}()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			_ = ws.CloseNow()
			return
		}
		if !s.call(ctx, func() { s.session.Handle(s.stop, win, data) }) {
			return
		}
	}
}

// call runs f on the session goroutine and waits for it.
func (s *Server) call(ctx context.Context, f func()) bool {
	done := make(chan struct{})
	select {
	case s.in <- func() { f(); close(done) }:
	case <-ctx.Done():
		return false
	case <-s.stop.Done():
		return false
	}
	select {
	case <-done:
		return true
	case <-s.stop.Done():
		return false
	}
}
