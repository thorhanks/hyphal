package markup

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// escapable are the bytes a backslash escapes (§5.8).
const escapable = "*~`=<[\\"

// linkSchemes are the URL prefixes a link may use (§5.8), compared without
// case.
var linkSchemes = []string{"http://", "https://", "mailto:", "file://"}

type tokKind uint8

const (
	tokEscape tokKind = iota
	tokCode
	tokDelim // a run of *, or exactly two ~ or =
	tokOpenTag
	tokCloseTag
	tokOpenBracket
	tokCloseBracket
)

type token struct {
	kind              tokKind
	start, end        int
	ch                byte  // tokDelim
	color             Color // tags
	canOpen, canClose bool  // tokDelim
	inactive          bool  // tokOpenBracket: a link inside it formed (no nested links)
	consumed          bool  // inside a link destination
}

// span is one matched style pair; outer includes its markers.
type span struct {
	style Style
	outer Range
}

type link struct {
	open, close int // token indexes of [ and ]
	url         string
	label, src  Range
}

// parser resolves markup in three passes: tokens (with code spans and
// escapes, which bind tightest), links (CommonMark's bracket stack), then
// emphasis and color with a delimiter stack, once per link label and once for
// the text around links (A-011).
type parser struct {
	src     string
	toks    []token
	links   []link
	spans   []span
	colors  []colorSpan
	markers []Range

	parenMatch []int // lazily: index of the matching ')' for each '(', or -1
	nextSpace  []int // lazily: first index >= i that is whitespace or a control
}

func isSpaceRune(r rune) bool { return unicode.IsSpace(r) }

func spaceAt(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isSpaceRune(r)
}

func spaceBefore(s string, i int) bool {
	if i <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isSpaceRune(r)
}

func (p *parser) tokenize() {
	s := p.src
	n := len(s)
	var closers []int
	for i := 0; i < n; {
		c := s[i]
		switch c {
		case '\\':
			if i+1 < n && strings.IndexByte(escapable, s[i+1]) >= 0 {
				p.toks = append(p.toks, token{kind: tokEscape, start: i, end: i + 2})
				i += 2
				continue
			}
			i++
		case '`':
			// A code span: content can't start or end with whitespace, and
			// everything inside is literal (§5.8).
			if i+1 < n && s[i+1] != '`' && !spaceAt(s, i+1) {
				if closers == nil {
					closers = codeClosers(s)
				}
				if j := closers[i+2]; j < n {
					p.toks = append(p.toks, token{kind: tokCode, start: i, end: j + 1})
					i = j + 1
					continue
				}
			}
			i++
		case '*', '~', '=':
			j := i
			for j < n && s[j] == c {
				j++
			}
			if c == '*' || j-i == 2 {
				p.toks = append(p.toks, token{
					kind: tokDelim, start: i, end: j, ch: c,
					canOpen:  !spaceAt(s, j),
					canClose: !spaceBefore(s, i),
				})
			}
			i = j
		case '<':
			if col, l, closing, ok := colorTag(s[i:]); ok {
				k := tokOpenTag
				if closing {
					k = tokCloseTag
				}
				p.toks = append(p.toks, token{kind: k, start: i, end: i + l, color: col})
				i += l
				continue
			}
			i++
		case '[':
			p.toks = append(p.toks, token{kind: tokOpenBracket, start: i, end: i + 1})
			i++
		case ']':
			p.toks = append(p.toks, token{kind: tokCloseBracket, start: i, end: i + 1})
			i++
		default:
			i++
		}
	}
}

// codeClosers returns, for each i, the first j >= i where a backtick can
// close a code span (not preceded by whitespace), or len(s).
func codeClosers(s string) []int {
	n := len(s)
	out := make([]int, n+1)
	out[n] = n
	for j := n - 1; j >= 0; j-- {
		if s[j] == '`' && j > 0 && !spaceBefore(s, j) {
			out[j] = j
		} else {
			out[j] = out[j+1]
		}
	}
	return out
}

// colorTag reads `<name>` or `</name>` for one of the eight colors.
func colorTag(s string) (c Color, length int, closing bool, ok bool) {
	start := 1
	if strings.HasPrefix(s, "</") {
		start, closing = 2, true
	}
	limit := min(len(s), start+8)
	k := strings.IndexByte(s[start:limit], '>')
	if k <= 0 {
		return NoColor, 0, false, false
	}
	c, ok = ColorByName(s[start : start+k])
	return c, start + k + 1, closing, ok
}

func (p *parser) resolveLinks() {
	var stack []int
	for i := 0; i < len(p.toks); i++ {
		t := &p.toks[i]
		switch t.kind {
		case tokOpenBracket:
			stack = append(stack, i)
		case tokCloseBracket:
			if len(stack) == 0 {
				continue
			}
			oi := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			o := &p.toks[oi]
			if o.inactive || o.end == t.start {
				continue // nested link or empty label: literal
			}
			url, end, ok := p.destination(t.end)
			if !ok {
				continue
			}
			k := i + 1
			for ; k < len(p.toks) && p.toks[k].start < end; k++ {
				if p.toks[k].end > end {
					ok = false // a code span crosses the closing paren
					break
				}
			}
			if !ok {
				continue
			}
			for m := i + 1; m < k; m++ {
				p.toks[m].consumed = true
			}
			for _, s := range stack {
				p.toks[s].inactive = true
			}
			p.links = append(p.links, link{
				open: oi, close: i, url: url,
				label: Range{Start: o.end, End: t.start},
				src:   Range{Start: o.start, End: end},
			})
			i = k - 1
		}
	}
}

// destination reads `(url)` at offset at: balanced parentheses, no
// whitespace or control characters, and one of the allowed schemes (§5.8).
// It returns the URL and the offset after the closing paren.
func (p *parser) destination(at int) (string, int, bool) {
	s := p.src
	if at >= len(s) || s[at] != '(' {
		return "", 0, false
	}
	if p.parenMatch == nil {
		p.indexDestinations()
	}
	j := p.parenMatch[at]
	if j < 0 || p.nextSpace[at] < j {
		return "", 0, false
	}
	url := s[at+1 : j]
	for _, scheme := range linkSchemes {
		if len(url) > len(scheme) && strings.EqualFold(url[:len(scheme)], scheme) {
			return url, j + 1, true
		}
	}
	return "", 0, false
}

func (p *parser) indexDestinations() {
	s := p.src
	n := len(s)
	p.parenMatch = make([]int, n)
	p.nextSpace = make([]int, n+1)
	var open []int
	for i := 0; i < n; i++ {
		p.parenMatch[i] = -1
		switch s[i] {
		case '(':
			open = append(open, i)
		case ')':
			if len(open) > 0 {
				p.parenMatch[open[len(open)-1]] = i
				open = open[:len(open)-1]
			}
		}
	}
	p.nextSpace[n] = n
	for i := n - 1; i >= 0; i-- {
		p.nextSpace[i] = p.nextSpace[i+1]
		c := s[i]
		switch {
		case c < 0x20 || c == 0x7f || c == ' ':
			p.nextSpace[i] = i
		case c >= 0x80 && utf8.RuneStart(c):
			if r, _ := utf8.DecodeRuneInString(s[i:]); isSpaceRune(r) || unicode.IsControl(r) {
				p.nextSpace[i] = i
			}
		}
	}
}

func (p *parser) resolveInlines() {
	var top []*token
	li := 0
	for i := 0; i < len(p.toks); i++ {
		t := &p.toks[i]
		if t.consumed {
			continue
		}
		if li < len(p.links) && i == p.links[li].open {
			var label []*token
			for j := i + 1; j < p.links[li].close; j++ {
				label = append(label, &p.toks[j])
			}
			p.process(label)
			i = p.links[li].close
			li++
			continue
		}
		top = append(top, t)
	}
	p.process(top)

	for i := range p.toks {
		t := &p.toks[i]
		if t.consumed {
			continue
		}
		switch t.kind {
		case tokEscape:
			p.markers = append(p.markers, Range{Start: t.start, End: t.start + 1})
		case tokCode:
			p.spans = append(p.spans, span{style: Code, outer: Range{Start: t.start, End: t.end}})
			p.markers = append(p.markers, Range{Start: t.start, End: t.start + 1}, Range{Start: t.end - 1, End: t.end})
		}
	}
}

// opener is a delimiter on the stack; [lo, hi) are its unused bytes.
type opener struct {
	t      *token
	lo, hi int
}

var delimStyle = map[byte]Style{'~': Strike, '=': Highlight}

// process matches delimiters and color tags in one sequence. A closer pairs
// with the nearest opener of its kind; openers between them become literal,
// so styles never overlap (§5.8).
func (p *parser) process(seq []*token) {
	var stack []opener
	find := func(match func(*token) bool) int {
		for j := len(stack) - 1; j >= 0; j-- {
			if match(stack[j].t) {
				return j
			}
		}
		return -1
	}
	for _, t := range seq {
		switch t.kind {
		case tokDelim:
			lo, hi := t.start, t.end
			if t.canClose {
				for lo < hi {
					j := find(func(o *token) bool { return o.kind == tokDelim && o.ch == t.ch })
					if j < 0 {
						break
					}
					stack = stack[:j+1]
					o := &stack[j]
					n := hi - lo
					if t.ch == '*' {
						n = 1
						if o.hi-o.lo >= 2 && hi-lo >= 2 {
							n = 2
						}
					}
					style := delimStyle[t.ch]
					if t.ch == '*' {
						style = Italic
						if n == 2 {
							style = Bold
						}
					}
					p.spans = append(p.spans, span{style: style, outer: Range{Start: o.hi - n, End: lo + n}})
					p.markers = append(p.markers, Range{Start: o.hi - n, End: o.hi}, Range{Start: lo, End: lo + n})
					o.hi -= n
					lo += n
					if o.hi == o.lo {
						stack = stack[:j]
					}
				}
			}
			if lo < hi && t.canOpen {
				stack = append(stack, opener{t: t, lo: lo, hi: hi})
			}
		case tokOpenTag:
			stack = append(stack, opener{t: t, lo: t.start, hi: t.end})
		case tokCloseTag:
			j := find(func(o *token) bool { return o.kind == tokOpenTag && o.color == t.color })
			if j < 0 || stack[j].t.end == t.start {
				continue // unmatched or empty: literal
			}
			o := stack[j].t
			p.colors = append(p.colors, colorSpan{
				color: t.color,
				open:  Range{Start: o.start, End: o.end},
				close: Range{Start: t.start, End: t.end},
			})
			stack = stack[:j]
		}
	}
}
