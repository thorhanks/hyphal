package server

import (
	"encoding/json"
	"unicode/utf16"

	"github.com/thorhanks/hyphal/internal/markup"
	"github.com/thorhanks/hyphal/internal/model"
)

// clientMsg is any client → server message (CONTRACTS §12.1). Offsets are
// UTF-16 code units, as the browser reports them.
type clientMsg struct {
	T       string       `json:"t"`
	Version string       `json:"version"`
	Zoom    model.NodeID `json:"zoom"`
	Seq     int64        `json:"seq"`
	Name    string       `json:"name"`
	ID      model.NodeID `json:"id"`
	Text    string       `json:"text"`
	Off     int          `json:"off"`
	End     int          `json:"end"`
	Dir     int          `json:"dir"`
	Start   int          `json:"start"`
	Count   int          `json:"count"`
}

type welcomeMsg struct {
	T        string          `json:"t"`
	Version  string          `json:"version"`
	Keymap   json.RawMessage `json:"keymap"` // keymap.Table.JSON (CONTRACTS §12.2)
	Config   config          `json:"config"`
	ReadOnly bool            `json:"readOnly"`
	Dev      bool            `json:"dev,omitempty"` // dev builds with --dev: the page loads window.__hyphal (A-013)
}

// config is what the page needs from config.toml (§13). TODO(P1-48): real values.
type config struct {
	Density    string `json:"density"`
	ThreadGlow bool   `json:"threadGlow"`
}

type crumb struct {
	ID   model.NodeID `json:"id"`
	Text string       `json:"text"`
}

type stateMsg struct {
	T        string         `json:"t"`
	Seq      int64          `json:"seq"`
	TextAck  int64          `json:"textAck"`
	Total    int            `json:"total"`
	Start    int            `json:"start"`
	Rows     []rowMsg       `json:"rows"`
	Cursor   model.NodeID   `json:"cursor"`
	Off      int            `json:"off"`
	End      int            `json:"end"`
	Block    []model.NodeID `json:"block"`
	Zoom     model.NodeID   `json:"zoom"`
	Crumbs   []crumb        `json:"crumbs"`
	Message  string         `json:"message"`
	Saving   bool           `json:"saving"`
	ReadOnly bool           `json:"readOnly"`
}

type rowMsg struct {
	ID        model.NodeID `json:"id"`
	Depth     int          `json:"depth"`
	Text      string       `json:"text"`
	Runs      []runMsg     `json:"runs"`
	Markers   [][2]int     `json:"markers,omitempty"` // the focused row only (D-042)
	Kids      bool         `json:"kids"`
	Collapsed bool         `json:"collapsed"`
	Hidden    int          `json:"hidden,omitempty"`
	Done      bool         `json:"done"`
	Task      bool         `json:"task"`
	Heading   bool         `json:"heading"`
	Page      bool         `json:"page"`
	Star      bool         `json:"star"`
}

type runMsg struct {
	Text  string   `json:"text"`
	Src   [2]int   `json:"src"` // UTF-16 range in the source, to map a click or a column to a source offset (A-013)
	Style []string `json:"style,omitempty"`
	Color string   `json:"color,omitempty"`
	Href  string   `json:"href,omitempty"`
}

type errorMsg struct {
	T       string `json:"t"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

var styleNames = []struct {
	s    markup.Style
	name string
}{
	{markup.Bold, "bold"}, {markup.Italic, "italic"}, {markup.Strike, "strike"},
	{markup.Code, "code"}, {markup.Highlight, "highlight"}, {markup.Link, "link"},
}

// runs renders a bullet's concealed runs (§5.8, CONTRACTS §12.2).
func runs(d *markup.Doc) []runMsg {
	rs := d.Concealed()
	src := d.Source()
	out := make([]runMsg, 0, len(rs))
	for _, r := range rs {
		m := runMsg{Text: r.Text, Src: [2]int{utf16Len(src[:r.Src.Start]), utf16Len(src[:r.Src.End])}}
		for _, s := range styleNames {
			if r.Style&s.s != 0 {
				m.Style = append(m.Style, s.name)
			}
		}
		if r.Color != markup.NoColor {
			m.Color = r.Color.String()
		}
		if r.Link != nil {
			m.Href = r.Link.URL
		}
		out = append(out, m)
	}
	return out
}

// markers returns the focused bullet's marker ranges in UTF-16 units (D-042).
func markers(d *markup.Doc) [][2]int {
	ms := d.Markers()
	if len(ms) == 0 {
		return nil
	}
	src := d.Source()
	out := make([][2]int, len(ms))
	for i, m := range ms {
		out[i] = [2]int{utf16Len(src[:m.Start]), utf16Len(src[:m.End])}
	}
	return out
}

// utf16Len is the length of s in UTF-16 code units (CONTRACTS §12).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// byteOffset converts a UTF-16 offset in s to a byte offset, clamped to s and
// moved back to the start of a rune it splits.
func byteOffset(s string, u int) int {
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		n += utf16.RuneLen(r)
		if n > u {
			return i
		}
	}
	return len(s)
}

// clampUTF16 keeps an offset within s and off the middle of a surrogate pair.
func clampUTF16(s string, u int) int {
	if u <= 0 {
		return 0
	}
	return utf16Len(s[:byteOffset(s, u)])
}
