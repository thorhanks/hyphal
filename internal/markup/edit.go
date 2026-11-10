package markup

import (
	"strings"
	"unicode/utf8"
)

// Leading reports which leading markers ConvertLeadingMarker removed (§5.4).
type Leading struct {
	Task      bool // `[ ] ` or `[x] `
	Completed bool // `[x] ` or `[X] `
	Heading   bool // `## `
}

// ConvertLeadingMarker removes the leading markers of §5.4 and reports them:
// `[ ] ` makes a task, `[x] ` (or `[X] `) a completed task and `## ` a
// heading. A task marker and a heading marker combine (D-035). It repeats
// until no marker is left, so the result never starts with one (A-011).
// Text without a leading marker comes back unchanged.
func ConvertLeadingMarker(src string) (rest string, m Leading) {
	for {
		switch {
		case strings.HasPrefix(src, "[ ] "):
			m.Task = true
		case strings.HasPrefix(src, "[x] "), strings.HasPrefix(src, "[X] "):
			m.Task, m.Completed = true, true
		case strings.HasPrefix(src, "## "):
			m.Heading = true
			src = src[3:]
			continue
		default:
			return src, m
		}
		src = src[4:]
	}
}

// WrapSelection wraps the selected text in marker on both sides and returns
// the new text with the selection on the inner text, so a second press nests
// the wrap: `*` twice makes bold (§5.8). The selection is clamped to the text
// and widened to whole runes.
func WrapSelection(src string, sel Range, marker string) (string, Range) {
	s, e := clampRange(src, sel)
	out := src[:s] + marker + src[s:e] + marker + src[e:]
	return out, Range{Start: s + len(marker), End: e + len(marker)}
}

func clampRange(src string, r Range) (int, int) {
	s := min(max(r.Start, 0), len(src))
	e := min(max(r.End, s), len(src))
	for s > 0 && s < len(src) && !utf8.RuneStart(src[s]) {
		s--
	}
	for e < len(src) && !utf8.RuneStart(src[e]) {
		e++
	}
	return s, e
}

// SetColor colors the whole text with one color tag (§5.8), replacing any
// color tags already in it; tags that were literal are escaped so they stay
// literal (A-011). NoColor removes the color. Other styles are kept.
func SetColor(src string, c Color) string {
	inner := escapeColorTags(Parse(src).withoutColorTags())
	if c == NoColor || inner == "" {
		return inner
	}
	// A trailing lone backslash would escape the closing tag's `<`; doubling
	// it keeps the rendered text the same.
	if trailingBackslashes(inner)%2 == 1 {
		inner += `\`
	}
	name := c.String()
	return "<" + name + ">" + inner + "</" + name + ">"
}

func trailingBackslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// escapeColorTags puts a backslash before every color tag that could still
// pair up. After the matched pairs are removed, the tags left were literal,
// and escaping keeps them literal without changing what they show.
func escapeColorTags(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	p := &parser{src: s}
	p.tokenize()
	p.resolveLinks()
	var b strings.Builder
	at, escaped := 0, false
	for _, t := range p.toks {
		if t.consumed || (t.kind != tokOpenTag && t.kind != tokCloseTag) {
			continue
		}
		b.WriteString(s[at:t.start])
		b.WriteByte('\\')
		at, escaped = t.start, true
	}
	if !escaped {
		return s
	}
	b.WriteString(s[at:])
	return b.String()
}

// withoutColorTags returns the source with every matched color tag removed.
func (d *Doc) withoutColorTags() string {
	return d.replaceColorTags(func(colorSpan, bool) string { return "" })
}

// replaceColorTags rebuilds the source with each matched color tag replaced
// by repl(tag pair, isOpen).
func (d *Doc) replaceColorTags(repl func(c colorSpan, open bool) string) string {
	if len(d.colors) == 0 {
		return d.src
	}
	type edit struct {
		r    Range
		text string
	}
	edits := make([]edit, 0, 2*len(d.colors))
	for _, c := range d.colors {
		edits = append(edits, edit{c.open, repl(c, true)}, edit{c.close, repl(c, false)})
	}
	// Color pairs don't nest, so sorting the pairs by start sorts the tags.
	var b strings.Builder
	b.Grow(len(d.src))
	at := 0
	for _, e := range edits {
		b.WriteString(d.src[at:e.r.Start])
		b.WriteString(e.text)
		at = e.r.End
	}
	b.WriteString(d.src[at:])
	return b.String()
}

// ExportColors converts color tags to `<span style="color: …">` with the CSS
// color names, for Markdown export (§5.8, §7.5). Unmatched tags stay literal.
func ExportColors(src string) string {
	return Parse(src).replaceColorTags(func(c colorSpan, open bool) string {
		if !open {
			return "</span>"
		}
		return `<span style="color: ` + c.color.String() + `">`
	})
}
