// Package servertest drives a server.Session with protocol messages and no
// browser (TESTING §4).
package servertest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/server"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

// Options configures a harness.
type Options struct {
	Outline  string    // fixture notation (CONTRACTS §11); ‸ places the cursor
	Clock    time.Time // default testgen.FixtureTime
	Platform string    // keymap platform; default this machine's
}

// State is a state message as the browser receives it (CONTRACTS §12.2).
type State struct {
	Seq     int64        `json:"seq"`
	TextAck int64        `json:"textAck"`
	Total   int          `json:"total"`
	Start   int          `json:"start"`
	Cursor  model.NodeID `json:"cursor"`
	Off     int          `json:"off"`
	Message string       `json:"message"`
	Saving  bool         `json:"saving"`
	Rows    []Row        `json:"rows"`
}

// Row is one row of a state.
type Row struct {
	ID      model.NodeID `json:"id"`
	Depth   int          `json:"depth"`
	Text    string       `json:"text"`
	Markers [][2]int     `json:"markers"`
	Runs    []struct {
		Text  string   `json:"text"`
		Src   [2]int   `json:"src"`
		Style []string `json:"style"`
	} `json:"runs"`
	Hidden int `json:"hidden"`
}

// Harness is one window of a session over a temp database.
type Harness struct {
	t       testing.TB
	ctx     context.Context
	sess    *server.Session
	db      *store.DB
	win     *server.Window
	now     *time.Time
	msgs    []json.RawMessage
	textSeq int64
	actSeq  int64
}

// New opens a session on a temp database holding the fixture, and says hello
// as a browser would.
func New(t testing.TB, o Options) *Harness {
	t.Helper()
	ctx := context.Background()
	f := testgen.MustParseOutline(t, o.Outline)
	b, err := testgen.ExportJSON(f.Dump)
	if err != nil {
		t.Fatal(err)
	}
	db := testgen.TempDB(t)
	if err := db.ImportJSON(ctx, bytes.NewReader(b), store.ImportReplace); err != nil {
		t.Fatal(err)
	}
	now := o.Clock
	if now.IsZero() {
		now = testgen.FixtureTime
	}
	h := &Harness{t: t, ctx: ctx, db: db, now: &now}
	clock := func() time.Time { return *h.now }
	outline, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Clock: clock, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	h.sess = server.NewSession(outline, db, server.SessionOptions{Version: "test", Clock: clock, Platform: o.Platform})
	h.join()
	if f.State.Cursor != "" {
		n, _ := outline.Tree().Node(f.State.Cursor)
		h.Action("set_cursor", map[string]any{"id": f.State.Cursor, "off": utf16Len(n.Text[:f.State.CursorOff])})
	}
	return h
}

func (h *Harness) join() {
	h.win = h.sess.Join(func(b []byte) { h.msgs = append(h.msgs, json.RawMessage(bytes.Clone(b))) })
	h.Send(map[string]any{"t": "hello", "version": "test"})
}

// Second opens another window on the same session (D-044).
func (h *Harness) Second() *Harness {
	s := &Harness{t: h.t, ctx: h.ctx, sess: h.sess, db: h.db, now: h.now}
	s.join()
	return s
}

// Send sends one raw protocol message.
func (h *Harness) Send(m map[string]any) {
	h.t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		h.t.Fatal(err)
	}
	h.sess.Handle(h.ctx, h.win, b)
}

// Action sends an action with the next action seq.
func (h *Harness) Action(name string, fields map[string]any) {
	h.actSeq++
	m := map[string]any{"t": "action", "seq": h.actSeq, "name": name}
	for k, v := range fields {
		m[k] = v
	}
	h.Send(m)
}

// Type inserts text at this window's cursor, one text message per character,
// as the browser sends them. It returns the last text seq.
func (h *Harness) Type(s string) int64 {
	h.t.Helper()
	for _, r := range s {
		st := h.State()
		row := h.row(st, st.Cursor)
		u := utf16.Encode([]rune(row.Text))
		at := min(st.Off, len(u))
		text := string(utf16.Decode(u[:at])) + string(r) + string(utf16.Decode(u[at:]))
		h.textSeq++
		h.Send(map[string]any{"t": "text", "seq": h.textSeq, "id": st.Cursor, "text": text, "off": at + utf16.RuneLen(r)})
	}
	return h.textSeq
}

// Advance moves the fake clock and runs the session's tick (§7.3).
func (h *Harness) Advance(d time.Duration) {
	*h.now = h.now.Add(d)
	h.sess.Tick(h.ctx, *h.now)
}

// Messages returns every message this window received.
func (h *Harness) Messages() []json.RawMessage { return h.msgs }

// States returns every state this window received.
func (h *Harness) States() []State {
	var out []State
	for _, m := range h.msgs {
		var probe struct{ T string }
		_ = json.Unmarshal(m, &probe)
		if probe.T == "state" {
			var s State
			if err := json.Unmarshal(m, &s); err != nil {
				h.t.Fatal(err)
			}
			out = append(out, s)
		}
	}
	return out
}

// State returns the latest state.
func (h *Harness) State() State {
	h.t.Helper()
	ss := h.States()
	if len(ss) == 0 {
		h.t.Fatal("no state received")
	}
	return ss[len(ss)-1]
}

func (h *Harness) row(s State, id model.NodeID) Row {
	h.t.Helper()
	for _, r := range s.Rows {
		if r.ID == id {
			return r
		}
	}
	h.t.Fatalf("row %s isn't in the window", id)
	return Row{}
}

// ID finds a node by its text.
func (h *Harness) ID(text string) model.NodeID {
	h.t.Helper()
	for _, id := range h.sess.Outline().Tree().AllNodes() {
		if n, _ := h.sess.Outline().Tree().Node(id); n.Text == text {
			return id
		}
	}
	h.t.Fatalf("no node %q", text)
	return ""
}

// AssertOutline compares the tree and this window's cursor with a fixture.
func (h *Harness) AssertOutline(want string) {
	h.t.Helper()
	tr := h.sess.Outline().Tree()
	id, off := h.win.Cursor()
	st := testgen.State{Cursor: id}
	if n, ok := tr.Node(id); ok {
		st.CursorOff = byteOffset(n.Text, off)
	}
	testgen.Equal(h.t, strings.TrimSpace(want), testgen.FormatOutline(tr, st))
}

// DB returns the database, to assert history rows.
func (h *Harness) DB() *store.DB { return h.db }

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func byteOffset(s string, u int) int {
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(s)
}
