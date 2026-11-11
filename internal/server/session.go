package server

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"time"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/keymap"
	"github.com/thorhanks/hyphal/internal/markup"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// Row window sizes (§9.3). recentreMargin keeps the cursor this many rows
// inside the window before an action reply moves the window to it.
const (
	defaultWindow  = 200
	maxWindow      = 1000
	recentreMargin = 20
)

// SessionOptions configures a Session.
type SessionOptions struct {
	Version string
	Clock   func() time.Time
	Dev     bool // the page may load window.__hyphal and /keyprobe (TESTING §5)
	// Platform resolves Mod in the keymap (§6.4): keymap.Linux or
	// keymap.MacOS. Default: this machine's, as the browser runs here too.
	Platform string
}

// Session is the one editing session of a server: one core.Outline and one
// undo stack shared by every window (D-044, ARCHITECTURE §5). It is not safe
// for concurrent use; the server calls it from one goroutine and the
// servertest harness from the test's.
type Session struct {
	o       *core.Outline
	db      *store.DB
	opts    SessionOptions
	keys    keymap.Table // resolved for opts.Platform
	keyJSON json.RawMessage
	windows []*Window
}

// Window is one browser tab: its own zoom, cursor and row window (D-044).
type Window struct {
	out     func([]byte)
	ready   bool // hello received
	zoom    model.NodeID
	cursor  model.NodeID
	off     int // UTF-16 units in the cursor's text (CONTRACTS §12)
	start   int
	count   int
	textAck int64
	message string
}

// NewSession wraps an outline opened on db.
func NewSession(o *core.Outline, db *store.DB, so SessionOptions) *Session {
	if so.Clock == nil {
		so.Clock = time.Now
	}
	if so.Platform == "" {
		so.Platform = keymap.Platform(runtime.GOOS)
	}
	keys := keymap.Defaults().Resolve(so.Platform) // TODO(P1-01): overrides from config.toml
	return &Session{o: o, db: db, opts: so, keys: keys, keyJSON: keys.JSON()}
}

// Keymap returns the keymap the windows get, resolved for the platform.
func (s *Session) Keymap() keymap.Table { return s.keys }

// Outline returns the session's outline.
func (s *Session) Outline() *core.Outline { return s.o }

// Join adds a window whose messages go to out, each one JSON message.
func (s *Session) Join(out func([]byte)) *Window {
	w := &Window{out: out, count: defaultWindow}
	s.windows = append(s.windows, w)
	return w
}

// Leave removes a window. The last window leaving seals open typing, so
// nothing waits on a timer with no tab to show it (§7.3).
func (s *Session) Leave(ctx context.Context, w *Window) {
	s.windows = slices.DeleteFunc(s.windows, func(x *Window) bool { return x == w })
	if len(s.windows) == 0 {
		_, _, _ = s.o.Seal(ctx)
	}
}

// Cursor returns a window's cursor and its UTF-16 offset.
func (w *Window) Cursor() (model.NodeID, int) { return w.cursor, w.off }

// Tick seals typing that is due (§7.3); the server calls it every 250 ms.
func (s *Session) Tick(ctx context.Context, now time.Time) {
	sealed, err := s.o.SealIfDue(ctx, now)
	if err != nil {
		s.failed(err)
	}
	if sealed || err != nil {
		s.broadcast()
	}
}

// Close seals open typing before the server exits (§7.3).
func (s *Session) Close(ctx context.Context) error {
	_, _, err := s.o.Seal(ctx)
	return err
}

// Handle runs one client message (CONTRACTS §12).
func (s *Session) Handle(ctx context.Context, w *Window, raw []byte) {
	var m clientMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		s.send(w, errorMsg{T: "error", Code: "bad_message", Message: err.Error()})
		return
	}
	switch m.T {
	case "hello":
		w.ready = true
		w.zoom = s.o.Tree().Root()
		if s.liveNode(m.Zoom) {
			w.zoom = m.Zoom
		}
		s.send(w, welcomeMsg{
			T: "welcome", Version: s.opts.Version, Keymap: s.keyJSON,
			Config: config{Density: "comfortable"}, ReadOnly: s.readOnly(), Dev: s.opts.Dev,
		})
		s.sendState(w, 0, true)
	case "text":
		s.text(ctx, w, m)
	case "action":
		s.action(ctx, w, m)
	case "window":
		w.start, w.count = max(0, m.Start), min(max(m.Count, 1), maxWindow)
		s.sendState(w, 0, false)
	case "blur":
		if _, _, err := s.o.Seal(ctx); err != nil {
			s.failed(err)
		}
		s.broadcast()
	default:
		s.send(w, errorMsg{T: "error", Code: "unknown_message", Message: "unknown message: " + m.T})
	}
}

// text takes typing in the focused bullet. It gets no reply of its own: every
// window gets a state, whose textAck tells this one which typing it reflects
// (CONTRACTS §12.3).
func (s *Session) text(ctx context.Context, w *Window, m clientMsg) {
	w.textAck = max(w.textAck, m.Seq)
	if s.readOnly() {
		w.message = msgReadOnly
	} else if err := s.o.TypeInto(ctx, m.ID, m.Text, s.opts.Clock()); err != nil && !errors.Is(err, store.ErrInvalid) {
		s.failed(err)
	}
	// TODO(P1-32): explain refused text (a leading marker or a line break).
	if s.liveNode(m.ID) {
		w.cursor, w.off = m.ID, m.Off
	}
	s.broadcast()
}

// action runs one action and replies with exactly one state echoing its seq
// (CONTRACTS §12.1).
func (s *Session) action(ctx context.Context, w *Window, m clientMsg) {
	switch m.Name {
	case actSetCursor:
		if !s.liveNode(m.ID) {
			break
		}
		sealed := false
		if t := s.o.PendingTyping(); t != nil && t.Node != m.ID {
			var err error
			if _, sealed, err = s.o.Seal(ctx); err != nil {
				s.failed(err)
			}
		}
		w.cursor, w.off = m.ID, m.Off
		s.sendState(w, m.Seq, true)
		if sealed {
			s.broadcastExcept(w)
		}
		return
	default:
		if _, ok := s.keys.Command(keymap.CommandID(m.Name)); ok {
			break // a command whose card hasn't wired it yet: only the state reply
		}
		s.send(w, errorMsg{T: "error", Code: "unknown_action", Message: msgUnknownName + m.Name})
	}
	s.sendState(w, m.Seq, true)
}

func (s *Session) failed(err error) {
	for _, w := range s.windows {
		w.message = msgSaveFailed + err.Error() // TODO(P1-30): the save label and banner
	}
}

func (s *Session) readOnly() bool { return s.db.ReadOnly() != store.ReadOnlyNone }

// liveNode reports whether id is a bullet a cursor can sit on.
func (s *Session) liveNode(id model.NodeID) bool {
	n, ok := s.o.Tree().Node(id)
	return ok && n.DeletedAt == nil && id != s.o.Tree().Root()
}

func (s *Session) send(w *Window, msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		panic(err) // every message type marshals
	}
	w.out(b)
}

func (s *Session) broadcast() { s.broadcastExcept(nil) }

func (s *Session) broadcastExcept(except *Window) {
	for _, w := range s.windows {
		if w != except && w.ready {
			s.sendState(w, 0, false)
		}
	}
}

// visRow is one visible row under a zoom; depth is -1 for the zoom heading.
type visRow struct {
	id    model.NodeID
	depth int
}

// visibleRows lists what a window shows: the zoom heading, then every live
// descendant not under a collapsed node (§4.1).
// TODO(P1-10): Tree.VisibleRows with completed, filter and sticky rules.
func visibleRows(t *core.Tree, zoom model.NodeID) []visRow {
	var out []visRow
	if zoom != t.Root() {
		out = append(out, visRow{zoom, -1})
	}
	var walk func(model.NodeID, int)
	walk = func(id model.NodeID, depth int) {
		for _, c := range t.Children(id) {
			out = append(out, visRow{c, depth})
			if !t.Collapsed(c) {
				walk(c, depth+1)
			}
		}
	}
	walk(zoom, 0)
	return out
}

// descendants counts a node's live descendants (CONTRACTS §12.2 hidden).
// TODO(P1-10): core.Tree keeps subtree sizes.
func descendants(t *core.Tree, id model.NodeID) int {
	n := 0
	for _, c := range t.Children(id) {
		n += 1 + descendants(t, c)
	}
	return n
}

// sendState sends a window its whole state. recentre moves the row window to
// the cursor when the cursor is near its edge or outside it (§9.3).
func (s *Session) sendState(w *Window, seq int64, recentre bool) {
	t := s.o.Tree()
	if !s.liveNode(w.zoom) && w.zoom != t.Root() {
		w.zoom = t.Root()
	}
	rows := visibleRows(t, w.zoom)
	at := slices.IndexFunc(rows, func(r visRow) bool { return r.id == w.cursor })
	if at < 0 && len(rows) > 0 {
		at = 0
		if rows[0].depth < 0 && len(rows) > 1 {
			at = 1
		}
		w.cursor, w.off = rows[at].id, 0
	}
	if at < 0 {
		w.cursor, w.off = "", 0
	} else {
		n, _ := t.Node(w.cursor)
		w.off = clampUTF16(n.Text, w.off)
	}
	if recentre && at >= 0 && (at < w.start+recentreMargin || at >= w.start+w.count-recentreMargin) {
		w.start = max(0, at-w.count/2)
	}
	w.start = max(0, min(w.start, len(rows)-1))
	end := min(len(rows), w.start+w.count)

	st := stateMsg{
		T: "state", Seq: seq, TextAck: w.textAck, Total: len(rows), Start: w.start,
		Rows: make([]rowMsg, 0, end-w.start), Cursor: w.cursor, Off: w.off, End: w.off,
		Block: []model.NodeID{}, Zoom: w.zoom, Message: w.message,
		Saving: s.o.PendingTyping() != nil, ReadOnly: s.readOnly(),
	}
	w.message = ""
	for _, a := range append(t.Ancestors(w.zoom), w.zoom) {
		n, _ := t.Node(a)
		st.Crumbs = append(st.Crumbs, crumb{a, n.Text})
	}
	for _, r := range rows[w.start:end] {
		st.Rows = append(st.Rows, s.row(t, r, r.id == w.cursor))
	}
	s.send(w, st)
}

func (s *Session) row(t *core.Tree, r visRow, focused bool) rowMsg {
	n, _ := t.Node(r.id)
	doc := markup.Parse(n.Text)
	_, page := t.Page(r.id)
	_, star := t.StarredAt(r.id)
	m := rowMsg{
		ID: r.id, Depth: r.depth, Text: n.Text, Runs: runs(doc),
		Kids: len(t.Children(r.id)) > 0, Collapsed: t.Collapsed(r.id),
		Done: n.CompletedAt != nil, Task: n.IsTask, Heading: n.IsHeading, Page: page, Star: star,
	}
	if focused {
		m.Markers = markers(doc)
	}
	if m.Collapsed && m.Kids {
		m.Hidden = descendants(t, r.id)
	}
	return m
}
