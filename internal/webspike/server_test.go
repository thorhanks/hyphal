package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, l, err := NewServer(parseSeed("a||A\n"), 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	return s
}

func TestNewServer_BindsLoopbackOnly(t *testing.T) {
	s, l, err := NewServer(parseSeed("a||A\n"), 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if !strings.HasPrefix(l.Addr().String(), "127.0.0.1:") {
		t.Fatalf("listener on %s", l.Addr())
	}
	if len(s.token) != 64 {
		t.Fatalf("token length %d", len(s.token))
	}
}

func TestHandler_RefusesBadRequests(t *testing.T) {
	s := testServer(t)
	host := strings.TrimPrefix(strings.Split(s.URL(), "/?")[0], "http://")
	cookie := &http.Cookie{Name: s.cookieName(), Value: s.token}
	tests := []struct {
		name   string
		path   string
		host   string
		origin string
		cookie *http.Cookie
		want   int
	}{
		{"no token", "/", host, "", nil, 403},
		{"wrong token", "/?token=nope", host, "", nil, 403},
		{"wrong cookie", "/", host, "", &http.Cookie{Name: s.cookieName(), Value: "x"}, 403},
		{"dns rebinding host", "/", "evil.example:80", "", cookie, 403},
		{"rebinding host with token", "/?token=" + s.token, "evil.example", "", nil, 403},
		{"cross-site websocket", "/ws", host, "https://evil.example", cookie, 403},
		{"cross-site page", "/", host, "http://evil.example", cookie, 403},
		{"websocket without cookie", "/ws", host, "http://" + host, nil, 403},
		{"token sets cookie and leaves the URL", "/?token=" + s.token, host, "", nil, 302},
		{"cookie opens the page", "/", host, "", cookie, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.path, nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestReadTheme_RejectsNonColors(t *testing.T) {
	p := t.TempDir() + "/colors.toml"
	writeFile(t, p, "mode = \"dark\"\naccent = \"#b1667a\"\nevil = \"red;}body{display:none\"\nmode2 = \"x\"\n")
	got := readTheme(p)
	if got["accent"] != "#b1667a" || got["mode"] != "dark" || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestSplit_UsesBrowserOffsets(t *testing.T) {
	a, b := u16split("a😀b", 3)
	if a != "a😀" || b != "b" {
		t.Fatalf("got %q %q", a, b)
	}
}

func TestUndo_RestoresStructureAndRedoReapplies(t *testing.T) {
	tr := parseSeed("a||Hello\n  b||Kid\n")
	tr.cur = cursor{id: "a", off: 2}
	tr.Enter(2, 2)
	if tr.nodes["a"].text != "He" || len(tr.root.children) != 2 {
		t.Fatalf("split: %q %d", tr.nodes["a"].text, len(tr.root.children))
	}
	tr.Undo()
	if tr.nodes["a"].text != "Hello" || len(tr.root.children) != 1 || tr.cur.off != 2 {
		t.Fatalf("undo: %q %d off %d", tr.nodes["a"].text, len(tr.root.children), tr.cur.off)
	}
	tr.Redo()
	if tr.nodes["a"].text != "He" || len(tr.root.children) != 2 {
		t.Fatalf("redo: %q %d", tr.nodes["a"].text, len(tr.root.children))
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}
