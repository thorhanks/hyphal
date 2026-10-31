package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

//go:embed assets
var assets embed.FS

// Server owns one Tree and serves it to tabs on loopback. The first connected tab
// is the writer; the others are read-only until it closes (AC9).
type Server struct {
	mu        sync.Mutex
	t         *Tree
	token     string
	port      int
	conns     []*conn
	colors    string
	editorCmd string
	theme     map[string]string
}

type conn struct {
	ws    *websocket.Conn
	send  chan []byte
	start int
	count int
	acked int // last typing message applied, echoed so stale states can't clobber newer text
}

// Msg is a client request.
type Msg struct {
	T     string `json:"t"`
	Name  string `json:"name"`
	ID    string `json:"id"`
	Text  string `json:"text"`
	Off   int    `json:"off"`
	End   int    `json:"end"`
	Dir   int    `json:"dir"`
	Start int    `json:"start"`
	Count int    `json:"count"`
	Seq   int    `json:"seq"`
}

type rowJSON struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Depth     int    `json:"depth"`
	Kids      bool   `json:"kids"`
	Collapsed bool   `json:"collapsed"`
	Done      bool   `json:"done"`
	Task      bool   `json:"task"`
	Heading   bool   `json:"heading"`
	Page      bool   `json:"page"`
}

type stateJSON struct {
	T        string    `json:"t"`
	Seq      int       `json:"seq"`
	Total    int       `json:"total"`
	Start    int       `json:"start"`
	Rows     []rowJSON `json:"rows"`
	Cursor   string    `json:"cursor"`
	Off      int       `json:"off"`
	Block    []string  `json:"block"`
	Crumbs   []string  `json:"crumbs"`
	Filter   string    `json:"filter"`
	Msg      string    `json:"msg"`
	ReadOnly bool      `json:"readOnly"`
	TextAck  int       `json:"textAck"`
}

// NewServer binds a loopback listener and creates the per-launch token (AC8).
func NewServer(t *Tree, port int, colors, editorCmd string) (*Server, net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, nil, err
	}
	s := &Server{
		t: t, token: hex.EncodeToString(raw), port: l.Addr().(*net.TCPAddr).Port,
		colors: colors, editorCmd: editorCmd,
	}
	s.theme = readTheme(colors)
	return s, l, nil
}

// URL is what the launcher opens; the token leaves the address bar on first use.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?token=%s", s.port, s.token)
}

func (s *Server) cookieName() string { return fmt.Sprintf("session_%d", s.port) }

func (s *Server) hostOK(h string) bool {
	return h == fmt.Sprintf("127.0.0.1:%d", s.port) || h == fmt.Sprintf("localhost:%d", s.port)
}

func (s *Server) tokenOK(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(s.token)) == 1
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie(s.cookieName())
	return err == nil && s.tokenOK(c.Value)
}

// Handler enforces Host, Origin and the session cookie on every route (AC8).
func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(assets, "assets")
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !s.hostOK(strings.TrimPrefix(o, "http://")) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		if q := r.URL.Query().Get("token"); q != "" && s.tokenOK(q) {
			http.SetCookie(w, &http.Cookie{
				Name: s.cookieName(), Value: s.token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
			return
		}
		if !s.authed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/ws" {
			s.serveWS(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'")
		files.ServeHTTP(w, r)
	})
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	c := &conn{ws: ws, send: make(chan []byte, 256), count: 200}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			select {
			case b := <-c.send:
				if ws.Write(ctx, websocket.MessageText, b) != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.sendTheme(c)
	s.push(c, 0)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		for i, o := range s.conns {
			if o == c {
				s.conns = append(s.conns[:i], s.conns[i+1:]...)
				break
			}
		}
		s.broadcast(nil, 0)
		s.mu.Unlock()
		_ = ws.CloseNow()
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m Msg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		s.handle(c, m)
	}
}

func (s *Server) handle(c *conn, m Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.T == "window" {
		c.start, c.count = m.Start, m.Count
		s.pushNoRecentre(c, m.Seq)
		return
	}
	if len(s.conns) == 0 || s.conns[0] != c {
		s.t.msg = ""
		s.push(c, m.Seq)
		return
	}
	switch m.T {
	case "text":
		s.t.SetText(m.ID, m.Text, m.Off)
		c.acked = m.Seq
		s.broadcast(c, 0)
		return
	case "action":
		s.apply(c, m)
		s.push(c, m.Seq)
		s.broadcast(c, 0)
	}
}

func (s *Server) apply(c *conn, m Msg) {
	t := s.t
	t.msg = ""
	switch m.Name {
	case "enter":
		t.Enter(m.Off, m.End)
	case "backspace":
		t.Backspace()
	case "delete":
		t.Delete()
	case "indent":
		t.Indent()
	case "outdent":
		t.Outdent()
	case "move":
		t.Move(m.Dir)
	case "done":
		t.ToggleDone()
	case "task", "heading":
		t.Toggle(m.Name)
	case "collapse":
		t.SetCollapsed(true)
	case "expand":
		t.SetCollapsed(false)
	case "setCursor":
		t.seal()
		if t.nodes[m.ID] != nil {
			t.cur = cursor{id: m.ID, off: m.Off}
		}
	case "blockEnter":
		t.BlockEnter()
	case "blockExit":
		t.BlockExit()
	case "blockMove":
		t.BlockMove(m.Dir)
	case "blockExtend":
		t.BlockExtend(m.Dir)
	case "blockDelete":
		t.DeleteBlock()
	case "zoomIn":
		t.ZoomIn()
	case "zoomOut":
		t.ZoomOut(false)
	case "zoomRoot":
		t.ZoomOut(true)
	case "undo":
		t.Undo()
	case "redo":
		t.Redo()
	case "filter":
		t.filter = m.Text
		if m.Text == "" {
			t.stay = map[string]bool{}
		}
		c.start = 0
	case "hint":
		t.msg = m.Text
	case "copy":
		b, _ := json.Marshal(map[string]string{"t": "clip", "text": t.Markdown()})
		c.send <- b
	case "editor":
		go s.runEditor(t.nodes[t.cur.id].id, t.nodes[t.cur.id].text)
		t.msg = "Editing in $EDITOR…"
	}
}

// runEditor is the §6.3 round trip with the lock released while the editor runs.
func (s *Server) runEditor(id, text string) {
	dir, err := os.MkdirTemp("", "webspike-edit")
	if err != nil {
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "subtree.md")
	if os.WriteFile(path, []byte(text+"\n"), 0o600) != nil {
		return
	}
	err = exec.Command("sh", "-c", s.editorCmd, "sh", path).Run()
	b, rerr := os.ReadFile(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && rerr == nil {
		line, _, _ := strings.Cut(string(b), "\n")
		s.t.seal()
		s.t.SetText(id, line, u16len(line))
		s.t.seal()
		s.t.msg = "Applied from $EDITOR"
	} else {
		s.t.msg = "The editor failed; nothing was applied"
	}
	s.broadcast(nil, 0)
}

func (s *Server) broadcast(except *conn, seq int) {
	for _, o := range s.conns {
		if o != except {
			s.push(o, seq)
		}
	}
}

func (s *Server) push(c *conn, seq int) { s.pushState(c, seq, true) }

func (s *Server) pushNoRecentre(c *conn, seq int) { s.pushState(c, seq, false) }

func (s *Server) pushState(c *conn, seq int, recentre bool) {
	t := s.t
	rows := t.Flatten()
	i := indexOf(rows, t.cur.id)
	if recentre && i >= 0 && (i < c.start+20 || i >= c.start+c.count-20) {
		c.start = max(0, i-c.count/2)
	}
	c.start = max(0, min(c.start, max(0, len(rows)-1)))
	end := min(len(rows), c.start+c.count)
	st := stateJSON{
		T: "state", Seq: seq, Total: len(rows), Start: c.start, Cursor: t.cur.id, Off: t.cur.off,
		Block: t.cur.block, Filter: t.filter, Msg: t.msg, ReadOnly: len(s.conns) == 0 || s.conns[0] != c, TextAck: c.acked,
		Rows: make([]rowJSON, 0, end-c.start),
	}
	for _, r := range rows[c.start:end] {
		st.Rows = append(st.Rows, rowJSON{
			ID: r.n.id, Text: r.n.text, Depth: r.depth, Kids: len(r.n.children) > 0,
			Collapsed: r.n.collapsed, Done: r.n.done, Task: r.n.task, Heading: r.n.heading, Page: r.n.page,
		})
	}
	for a := t.zoom; a != nil; a = a.parent {
		st.Crumbs = append([]string{a.text}, st.Crumbs...)
	}
	b, _ := json.Marshal(st)
	select {
	case c.send <- b:
	default:
	}
}

// Theme (AC7).

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// readTheme takes only string values that are hex colors, plus mode, so a theme
// file can't inject CSS.
func readTheme(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if hexColor.MatchString(v) || (k == "mode" && (v == "dark" || v == "light")) {
			out[k] = v
		}
	}
	return out
}

func (s *Server) sendTheme(c *conn) {
	b, _ := json.Marshal(map[string]any{"t": "theme", "colors": s.theme})
	select {
	case c.send <- b:
	default:
	}
}

// WatchTheme polls so that it also survives Omarchy replacing the theme symlink.
func (s *Server) WatchTheme(ctx context.Context) {
	s.mu.Lock()
	seen, _ := json.Marshal(s.theme)
	s.mu.Unlock()
	last := string(seen)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		next := readTheme(s.colors)
		b, _ := json.Marshal(next)
		if string(b) == last {
			continue
		}
		last = string(b)
		s.mu.Lock()
		s.theme = next
		for _, c := range s.conns {
			s.sendTheme(c)
		}
		s.mu.Unlock()
	}
}
