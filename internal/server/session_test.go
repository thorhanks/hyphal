package server_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/keymap"
	"github.com/thorhanks/hyphal/internal/server/servertest"
)

// AC2: typing reaches the tree, and each state says which typing it reflects.
func TestSession_TextAck(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸\n- Beta"})
	seq := h.Type("!?")
	if got := h.State().TextAck; got != seq {
		t.Errorf("textAck = %d, want %d", got, seq)
	}
	h.AssertOutline("- Alpha!?‸\n- Beta")

	// An action reply carries the same ack; a second window has its own.
	h.Action("set_cursor", map[string]any{"id": h.ID("Beta"), "off": 0})
	if s := h.State(); s.Seq != 2 || s.TextAck != seq {
		t.Errorf("reply seq %d textAck %d, want 2 and %d", s.Seq, s.TextAck, seq)
	}
	w2 := h.Second()
	if got := w2.State().TextAck; got != 0 {
		t.Errorf("second window textAck = %d, want 0", got)
	}
	if got := w2.State().Rows[0].Text; got != "Alpha!?" {
		t.Errorf("second window sees %q", got)
	}
}

// AC2: every window gets a state after typing, so others see it at once.
func TestSession_TypingBroadcasts(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸"})
	w2 := h.Second()
	before := len(w2.States())
	h.Type("x")
	if len(w2.States()) != before+1 || w2.State().Rows[0].Text != "Alphax" {
		t.Errorf("second window states %d → %d, text %q", before, len(w2.States()), w2.State().Rows[0].Text)
	}
	if w2.State().Seq != 0 {
		t.Errorf("a broadcast echoes seq %d", w2.State().Seq)
	}
}

// Typing seals into one change set after 2 s idle (§7.3) through the tick.
func TestSession_TypingSealsOnTick(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸"})
	h.Type("abc")
	if !h.State().Saving {
		t.Error("open typing isn't shown as saving")
	}
	h.Advance(time.Second)
	if !h.State().Saving {
		t.Error("typing sealed before 2 s idle")
	}
	h.Advance(1500 * time.Millisecond)
	if h.State().Saving {
		t.Error("typing still open after 2 s idle")
	}
	n, err := h.DB().HistorySeq(t.Context())
	if err != nil || n == 0 {
		t.Errorf("history seq %d, %v: the seal wasn't committed", n, err)
	}
}

// Moving the cursor to another bullet seals the typing at once (§7.3).
func TestSession_SetCursorSeals(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸\n- Beta"})
	h.Type("x")
	h.Action("set_cursor", map[string]any{"id": h.ID("Beta"), "off": 2})
	if h.State().Saving {
		t.Error("typing still open after leaving the bullet")
	}
	h.AssertOutline("- Alphax\n- Be‸ta")
}

// AC4: offsets are UTF-16 units; a cursor inside a surrogate pair moves to its start.
func TestSession_UTF16Offsets(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- a😀é‸z"})
	if got := h.State().Off; got != 4 { // a, two units of 😀, é
		t.Errorf("off = %d, want 4", got)
	}
	h.Type("|")
	h.AssertOutline("- a😀é|‸z")
	h.Action("set_cursor", map[string]any{"id": h.ID("a😀é|z"), "off": 2})
	if got := h.State().Off; got != 1 {
		t.Errorf("off inside 😀 = %d, want 1", got)
	}
}

// AC6: the focused row carries marker ranges in UTF-16 units; other rows only runs.
func TestSession_Markers(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- 😀**bold**‸\n- **b**"})
	s := h.State()
	if got := fmt.Sprint(s.Rows[0].Markers); got != "[[2 4] [8 10]]" {
		t.Errorf("markers = %s", got)
	}
	if s.Rows[1].Markers != nil {
		t.Errorf("unfocused row has markers %v", s.Rows[1].Markers)
	}
	runs := s.Rows[1].Runs
	if len(runs) != 1 || runs[0].Text != "b" || !slices.Equal(runs[0].Style, []string{"bold"}) || runs[0].Src != [2]int{2, 3} {
		t.Errorf("runs = %+v", runs)
	}
}

// The welcome carries the keymap resolved for the platform (CONTRACTS §12.2).
func TestSession_WelcomeKeymap(t *testing.T) {
	for _, tt := range []struct{ platform, undo string }{{keymap.Linux, "ctrl+z"}, {keymap.MacOS, "meta+z"}} {
		h := servertest.New(t, servertest.Options{Outline: "- Alpha‸", Platform: tt.platform})
		var w struct {
			T      string `json:"t"`
			Keymap []struct {
				ID   string                          `json:"id"`
				Keys []struct{ Context, Key string } `json:"keys"`
			} `json:"keymap"`
		}
		if err := json.Unmarshal(h.Messages()[0], &w); err != nil || w.T != "welcome" {
			t.Fatalf("first message %s: %v", h.Messages()[0], err)
		}
		found := false
		for _, c := range w.Keymap {
			for _, k := range c.Keys {
				if strings.Contains(k.Key, "mod") {
					t.Errorf("%s: unresolved %s on %s", tt.platform, k.Key, c.ID)
				}
				found = found || (c.ID == "undo" && k.Key == tt.undo)
			}
		}
		if !found {
			t.Errorf("%s: undo is not on %s", tt.platform, tt.undo)
		}
	}
}

// A command whose card hasn't wired it yet gets its state reply and no error.
func TestSession_KnownCommandNotYetWired(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸"})
	n := len(h.Messages())
	h.Action("new_bullet", map[string]any{"off": 5, "end": 5})
	msgs := h.Messages()[n:]
	if len(msgs) != 1 {
		t.Fatalf("got %d messages: %s", len(msgs), msgs)
	}
	if s := h.State(); s.Seq != 2 {
		t.Errorf("reply seq = %d, want 2", s.Seq)
	}
	h.AssertOutline("- Alpha‸")
}

// Unknown action names change nothing, and still get their one reply.
func TestSession_UnknownAction(t *testing.T) {
	h := servertest.New(t, servertest.Options{Outline: "- Alpha‸"})
	h.Action("explode", nil)
	msgs := h.Messages()
	var e struct{ T, Code string }
	_ = json.Unmarshal(msgs[len(msgs)-2], &e)
	if e.T != "error" || e.Code != "unknown_action" {
		t.Errorf("got %s", msgs[len(msgs)-2])
	}
	if s := h.State(); s.Seq != 2 {
		t.Errorf("reply seq = %d, want 2", s.Seq)
	}
	h.AssertOutline("- Alpha‸")
}

// AC5: the row window follows the cursor on action replies, not on scroll.
func TestSession_RowWindow(t *testing.T) {
	var b strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&b, "- Row %d {collapsed}\n  - Kid %d\n", i, i)
	}
	h := servertest.New(t, servertest.Options{Outline: strings.TrimSpace(b.String())})
	s := h.State()
	if s.Total != 1000 || len(s.Rows) != 200 || s.Rows[0].Hidden != 1 {
		t.Fatalf("total %d rows %d hidden %d", s.Total, len(s.Rows), s.Rows[0].Hidden)
	}
	h.Send(map[string]any{"t": "window", "start": 700, "count": 200})
	if s := h.State(); s.Start != 700 || s.Rows[0].Text != "Row 700" {
		t.Errorf("window at %d starts with %q", s.Start, s.Rows[0].Text)
	}
	h.Action("set_cursor", map[string]any{"id": s.Cursor, "off": 0})
	if s := h.State(); s.Start != 0 {
		t.Errorf("the reply didn't return to the cursor: start %d", s.Start)
	}
}
