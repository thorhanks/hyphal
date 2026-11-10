package markup

import (
	"slices"
	"strings"

	"github.com/thorhanks/hyphal/internal/textfold"
)

// Range is a half-open byte range [Start, End) in the source text.
type Range = textfold.Range

// Style is a bitset of inline styles (§5.8).
type Style uint16

// Styles. Marker marks marker bytes in Revealed runs; Concealed runs never
// carry it.
const (
	Bold Style = 1 << iota
	Italic
	Strike
	Code
	Highlight
	Link
	Marker
)

// Color is one of the eight text colors of §5.8, or NoColor.
type Color uint8

// The eight text colors (§5.8), in color picker order.
const (
	NoColor Color = iota
	Red
	Orange
	Yellow
	Green
	Cyan
	Blue
	Purple
	Gray
)

var colorNames = [...]string{"", "red", "orange", "yellow", "green", "cyan", "blue", "purple", "gray"}

// String returns the color's tag name (`red`), or "" for NoColor.
func (c Color) String() string {
	if int(c) < len(colorNames) {
		return colorNames[c]
	}
	return ""
}

// ColorByName returns the color whose tag name is name (§5.8). Names are
// lowercase; anything else is an unknown tag.
func ColorByName(name string) (Color, bool) {
	for i := 1; i < len(colorNames); i++ {
		if colorNames[i] == name {
			return Color(i), true
		}
	}
	return NoColor, false
}

// Colors returns the eight text colors in order (§5.8).
func Colors() []Color {
	return []Color{Red, Orange, Yellow, Green, Cyan, Blue, Purple, Gray}
}

// LinkTarget is where a link points (§5.8). Phase 2 adds node links.
type LinkTarget struct {
	URL string
}

// Run is a stretch of text with one style. Text always equals the source
// bytes in Src, so offsets map straight back to the stored text.
type Run struct {
	Text  string
	Style Style
	Color Color
	Src   Range
	Link  *LinkTarget // set when Style has Link
}

// LinkRef is one link in the source (§5.8).
type LinkRef struct {
	URL   string
	Label Range // the label text, without brackets
	Src   Range // the whole `[label](url)`
}

// attr is the style of one source byte.
type attr struct {
	style Style
	color Color
	link  int32 // index+1 into Doc.targets, 0 for none
}

// colorSpan is one matched pair of color tags.
type colorSpan struct {
	color       Color
	open, close Range
}

// Doc is parsed bullet text (§5.8). It is immutable.
type Doc struct {
	src     string
	attrs   []attr // per source byte; nil when the text has no markup
	markers []Range
	links   []LinkRef
	targets []*LinkTarget
	colors  []colorSpan
}

// specialBytes are the bytes that can start markup; text without them is
// plain and skips the parser.
const specialBytes = "\\`*~=<["

// Parse parses bullet text with the inline grammar of §5.8. It never fails:
// anything that doesn't form markup is literal text. It runs in time linear
// in the length of src for all but contrived inputs.
func Parse(src string) *Doc {
	d := &Doc{src: src}
	if !strings.ContainsAny(src, specialBytes) {
		return d
	}
	p := &parser{src: src}
	p.tokenize()
	p.resolveLinks()
	p.resolveInlines()
	d.build(p)
	return d
}

// Source returns the text the Doc was parsed from.
func (d *Doc) Source() string { return d.src }

// Concealed returns the runs shown on a bullet that isn't being edited, with
// markers hidden (§4.6, §5.8). Runs are split at every hidden marker, so each
// run's Text equals the source in its Src.
func (d *Doc) Concealed() []Run { return d.runs(true) }

// Revealed returns the runs of the focused bullet: every source byte, with
// marker bytes styled Marker (§4.6, D-042). Concatenating Text gives the
// source byte for byte.
func (d *Doc) Revealed() []Run { return d.runs(false) }

// Markers returns the marker ranges, sorted and disjoint. The server sends
// them for the focused row, which paints them with the Custom Highlight API
// (D-042).
func (d *Doc) Markers() []Range { return slices.Clone(d.markers) }

// Links returns the links in the Doc, in source order (§5.8).
func (d *Doc) Links() []LinkRef { return slices.Clone(d.links) }

func (d *Doc) runs(conceal bool) []Run {
	n := len(d.src)
	if n == 0 {
		return nil
	}
	if d.attrs == nil {
		return []Run{{Text: d.src, Src: Range{Start: 0, End: n}}}
	}
	var out []Run
	start := -1
	for i := 0; i <= n; i++ {
		if start >= 0 && (i == n || d.attrs[i] != d.attrs[start]) {
			out = append(out, d.run(start, i))
			start = -1
		}
		if i == n || (conceal && d.attrs[i].style&Marker != 0) {
			continue
		}
		if start < 0 {
			start = i
		}
	}
	return out
}

func (d *Doc) run(start, end int) Run {
	a := d.attrs[start]
	r := Run{Text: d.src[start:end], Style: a.style, Color: a.color, Src: Range{Start: start, End: end}}
	if a.link > 0 {
		r.Link = d.targets[a.link-1]
	}
	return r
}

// Plain returns src with markup removed, as the bullet renders when
// concealed (§6.2): markers dropped, links as their labels. offsets has
// len(plain)+1 entries; offsets[i] is the source offset of plain byte i, and
// the last entry is the source offset just after the last plain byte (0 for
// empty plain text). Search and path lookup match against it.
func Plain(src string) (plain string, offsets []int) {
	d := Parse(src)
	if d.attrs == nil {
		offsets = make([]int, len(src)+1)
		for i := range offsets {
			offsets[i] = i
		}
		return src, offsets
	}
	var b strings.Builder
	b.Grow(len(src))
	offsets = make([]int, 0, len(src)+1)
	end := 0
	for _, r := range d.Concealed() {
		b.WriteString(r.Text)
		for i := r.Src.Start; i < r.Src.End; i++ {
			offsets = append(offsets, i)
		}
		end = r.Src.End
	}
	return b.String(), append(offsets, end)
}

// Links returns the links in src, in source order (§5.8).
func Links(src string) []LinkRef { return Parse(src).links }

// build turns the parser's spans into per-byte attributes.
func (d *Doc) build(p *parser) {
	d.colors = dropNestedColors(p.colors)
	for _, c := range d.colors {
		p.markers = append(p.markers, c.open, c.close)
	}
	if len(p.spans) == 0 && len(p.markers) == 0 && len(p.links) == 0 {
		return
	}
	n := len(d.src)
	d.attrs = make([]attr, n)

	// Styles nest, so count them with difference arrays to stay linear in
	// the nesting depth.
	var delta [5][]int32
	for _, s := range p.spans {
		k := styleIndex(s.style)
		if delta[k] == nil {
			delta[k] = make([]int32, n+1)
		}
		delta[k][s.outer.Start]++
		delta[k][s.outer.End]--
	}
	for k := range delta {
		if delta[k] == nil {
			continue
		}
		bit := Style(1) << k
		var c int32
		for i := 0; i < n; i++ {
			c += delta[k][i]
			if c > 0 {
				d.attrs[i].style |= bit
			}
		}
	}
	for _, c := range d.colors {
		for i := c.open.Start; i < c.close.End; i++ {
			d.attrs[i].color = c.color
		}
	}
	for li, l := range p.links {
		d.links = append(d.links, LinkRef{URL: l.url, Label: l.label, Src: l.src})
		d.targets = append(d.targets, &LinkTarget{URL: l.url})
		p.markers = append(p.markers, Range{Start: l.src.Start, End: l.label.Start}, Range{Start: l.label.End, End: l.src.End})
		for i := l.src.Start; i < l.src.End; i++ {
			d.attrs[i].style |= Link
			d.attrs[i].link = int32(li + 1)
		}
	}
	slices.SortFunc(p.markers, func(a, b Range) int { return a.Start - b.Start })
	d.markers = p.markers
	for _, m := range d.markers {
		for i := m.Start; i < m.End; i++ {
			d.attrs[i].style |= Marker
		}
	}
}

// styleIndex returns the bit position of a single style bit below Link.
func styleIndex(s Style) int {
	for k := 0; k < 5; k++ {
		if s == Style(1)<<k {
			return k
		}
	}
	panic("markup: style index out of range")
}

// dropNestedColors removes color pairs inside another color pair: color tags
// don't nest, and the inner pair stays literal (§5.8). Pairs nest properly,
// so a sweep by start finds them.
func dropNestedColors(cs []colorSpan) []colorSpan {
	slices.SortFunc(cs, func(a, b colorSpan) int { return a.open.Start - b.open.Start })
	out := cs[:0]
	end := -1
	for _, c := range cs {
		if c.open.Start < end {
			continue
		}
		out = append(out, c)
		end = c.close.End
	}
	return out
}
