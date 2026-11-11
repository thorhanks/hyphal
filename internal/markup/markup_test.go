package markup_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/markup"
	"github.com/thorhanks/hyphal/internal/store"
)

// fmtRuns writes runs as `styles:text|…`, merging neighbours with the same
// style. Style letters: b bold, i italic, s strike, c code, h highlight,
// l link; `@red` a color.
func fmtRuns(runs []markup.Run) string {
	type seg struct {
		key, text string
	}
	var segs []seg
	for _, r := range runs {
		var k strings.Builder
		for i, l := range "bischl" {
			if r.Style&(markup.Style(1)<<i) != 0 {
				k.WriteRune(l)
			}
		}
		if r.Color != markup.NoColor {
			k.WriteString("@" + r.Color.String())
		}
		if r.Link != nil {
			k.WriteString("<" + r.Link.URL + ">")
		}
		if n := len(segs); n > 0 && segs[n-1].key == k.String() {
			segs[n-1].text += r.Text
			continue
		}
		segs = append(segs, seg{k.String(), r.Text})
	}
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.key + ":" + s.text
	}
	return strings.Join(out, "|")
}

// stripURLs drops the `<url>` part of link keys, for table readability.
func stripURLs(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<' && depth == 0 && strings.HasSuffix(b.String(), "l"):
			depth = 1
		case r == '>' && depth == 1:
			depth = 0
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestParse_Table(t *testing.T) {
	cases := []struct{ src, want string }{
		{"", ""},
		{"plain", ":plain"},
		{"**bold** text", "b:bold|: text"},
		{"*it*", "i:it"},
		{"~~s~~", "s:s"},
		{"`c`", "c:c"},
		{"==h==", "h:h"},
		{"<red>r</red>", "@red:r"},
		{"[label](https://x.org)", "l:label"},
		{"2 * 3 * 4", ":2 * 3 * 4"},
		{"**bold *and italic***", "b:bold |bi:and italic"},
		{"***x***", "bi:x"},
		{`\*not\*`, ":*not*"},
		{"`**not bold**`", "c:**not bold**"},
		{"<red>a <blue>b</blue></red>", "@red:a <blue>b</blue>"},
		{"<red><blue>a</blue></red>", "@red:<blue>a</blue>"},
		{"<pink>x</pink>", ":<pink>x</pink>"},
		{"snake_case", ":snake_case"},
		{"__bold__", ":__bold__"},
		{"_it_", ":_it_"},
		{"* a*", ":* a*"},
		{"*a *", ":*a *"},
		{"*a", ":*a"},
		{"a*", ":a*"},
		{"**a", ":**a"},
		{"a*b*c", ":a|i:b|:c"},
		{"*a*b", "i:a|:b"},
		{"**a ~~b** c~~", "b:a ~~b|: c~~"},
		{"~~a **b~~ c**", "s:a **b|: c**"},
		{"~a~", ":~a~"},
		{"~~~a~~~", ":~~~a~~~"},
		{"=a=", ":=a="},
		{"a == b", ":a == b"},
		{"`a` b `c`", "c:a|: b |c:c"},
		{"` a`", ":` a`"},
		{"`a `", ":`a `"},
		{"`a `b`", "c:a `b"},
		{"``", ":``"},
		{"`unclosed", ":`unclosed"},
		{"\\`x\\`", ":`x`"},
		{`\\`, `:\`},
		{`\a`, `:\a`},
		{`a\`, `:a\`},
		{`\~~a~~`, ":~~a~~"},
		{`\==a==`, ":==a=="},
		{`\<red>x</red>`, ":<red>x</red>"},
		{`\[a](https://x)`, ":[a](https://x)"},
		{"[a](ftp://x)", ":[a](ftp://x)"},
		{"[a](https://x y)", ":[a](https://x y)"},
		{"[a](https://)", ":[a](https://)"},
		{"[](https://x)", ":[](https://x)"},
		{"[a] (https://x)", ":[a] (https://x)"},
		{"[a](mailto:me@x.org)", "l:a"},
		{"[a](file:///tmp/x)", "l:a"},
		{"[a](HTTPS://x)", "l:a"},
		{"[**b**](https://x)", "bl:b"},
		{"**[a](https://x) c**", "bl:a|b: c"},
		{"[a](https://en.wikipedia.org/wiki/Foo_(bar))", "l:a"},
		{"[a [b](https://x)", ":[a |l:b"},
		{"[[a](https://x)](https://y)", ":[|l:a|:](https://y)"},
		{"*a [b* c](https://x)", ":*a |l:b* c"},
		{"[`a]`](https://x)", "cl:a]"},
		{"[a](https://x`y)` z", ":[a](https://x|c:y)|: z"},
		{"[a](https://x/*b*)", "l:a"},
		{"<red>**a**</red>", "b@red:a"},
		{"**<red>a</red>**", "b@red:a"},
		{"<red>a</blue>", ":<red>a</blue>"},
		{"<red>a", ":<red>a"},
		{"<red></red>", ":<red></red>"},
		{"<Red>a</Red>", ":<Red>a</Red>"},
		{"<red>a <blue>b</red></blue>", "@red:a <blue>b|:</blue>"},
		{"<red> spaced </red>", "@red: spaced "},
		{"`<red>x</red>`", "c:<red>x</red>"},
		{"**a**b**c**", "b:a|:b|b:c"},
		{"****", ":****"},
		{"a **b** c", ":a |b:b|: c"},
		{"**a* b", ":*|i:a|: b"},
		{"*a** b", "i:a|:* b"},
		{"~~a~~b~~c~~", "s:a|:b|s:c"},
		{"==a **b**==", "h:a |bh:b"},
		{"x <red>y</red> z", ":x |@red:y|: z"},
		{"<red>a</red><blue>b</blue>", "@red:a|@blue:b"},
		{`\*\*a\*\*`, ":**a**"},
		{"**é😀**", "b:é😀"},
		{"**a**　*b*", "b:a|:　|i:b"},
		{"*　a*", ":*　a*"},
		{"<gray>~~old~~</gray> new", "s@gray:old|: new"},
	}
	for _, c := range cases {
		got := stripURLs(fmtRuns(markup.Parse(c.src).Concealed()))
		if got != c.want {
			t.Errorf("Parse(%q).Concealed() = %q, want %q", c.src, got, c.want)
		}
	}
	if len(cases) < 60 {
		t.Fatalf("table has %d cases, the card asks for at least 60", len(cases))
	}
}

func TestParse_LinkTargets(t *testing.T) {
	src := "see [docs](https://x.org/a) and [mail](mailto:a@b.c)"
	got := fmtRuns(markup.Parse(src).Concealed())
	want := ":see |l<https://x.org/a>:docs|: and |l<mailto:a@b.c>:mail"
	if got != want {
		t.Errorf("runs = %q, want %q", got, want)
	}
	links := markup.Links(src)
	if len(links) != 2 {
		t.Fatalf("Links = %v, want 2", links)
	}
	l := links[0]
	if l.URL != "https://x.org/a" || src[l.Label.Start:l.Label.End] != "docs" ||
		src[l.Src.Start:l.Src.End] != "[docs](https://x.org/a)" {
		t.Errorf("Links[0] = %+v", l)
	}
}

func TestRevealed_Markers(t *testing.T) {
	src := "**a** \\* [b](https://x) `c`"
	d := markup.Parse(src)
	var got []string
	for _, m := range d.Markers() {
		got = append(got, src[m.Start:m.End])
	}
	want := []string{"**", "**", `\`, "[", "](https://x)", "`", "`"}
	if !slices.Equal(got, want) {
		t.Errorf("Markers = %q, want %q", got, want)
	}
	var b strings.Builder
	for _, r := range d.Revealed() {
		b.WriteString(r.Text)
		isMarker := r.Style&markup.Marker != 0
		inMarker := false
		for _, m := range d.Markers() {
			if r.Src.Start >= m.Start && r.Src.End <= m.End {
				inMarker = true
			}
		}
		if isMarker != inMarker {
			t.Errorf("run %+v: Marker style %v, inside a marker range %v", r, isMarker, inMarker)
		}
	}
	if b.String() != src {
		t.Errorf("Revealed text = %q, want the source", b.String())
	}
}

func TestPlain(t *testing.T) {
	cases := []struct {
		src, plain string
		offsets    []int
	}{
		{"", "", []int{0}},
		{"abc", "abc", []int{0, 1, 2, 3}},
		{"**bold** text", "bold text", []int{2, 3, 4, 5, 8, 9, 10, 11, 12, 13}},
		{"[label](https://x)", "label", []int{1, 2, 3, 4, 5, 6}},
		{"<red>a</red>", "a", []int{5, 6}},
		{`a\*`, "a*", []int{0, 2, 3}},
		{"****", "****", []int{0, 1, 2, 3, 4}},
	}
	for _, c := range cases {
		plain, offsets := markup.Plain(c.src)
		if plain != c.plain || !slices.Equal(offsets, c.offsets) {
			t.Errorf("Plain(%q) = %q %v, want %q %v", c.src, plain, offsets, c.plain, c.offsets)
		}
	}
}

func TestConvertLeadingMarker(t *testing.T) {
	type L = markup.Leading
	cases := []struct {
		src, rest string
		m         L
	}{
		{"[ ] a", "a", L{Task: true}},
		{"[x] a", "a", L{Task: true, Completed: true}},
		{"[X] a", "a", L{Task: true, Completed: true}},
		{"## a", "a", L{Heading: true}},
		{"[ ] ## a", "a", L{Task: true, Heading: true}},
		{"[x] ## a", "a", L{Task: true, Completed: true, Heading: true}},
		{"## [ ] a", "a", L{Task: true, Heading: true}},
		{"[ ] [ ] a", "a", L{Task: true}},
		{"[ ] ", "", L{Task: true}},
		{"## ", "", L{Heading: true}},
		{"[ ]a", "[ ]a", L{}},
		{"##a", "##a", L{}},
		{"### a", "### a", L{}},
		{"# a", "# a", L{}},
		{"[ ]", "[ ]", L{}},
		{"[y] a", "[y] a", L{}},
		{" [ ] a", " [ ] a", L{}},
		{"a [ ] b", "a [ ] b", L{}},
		{"", "", L{}},
	}
	for _, c := range cases {
		rest, m := markup.ConvertLeadingMarker(c.src)
		if rest != c.rest || m != c.m {
			t.Errorf("ConvertLeadingMarker(%q) = %q %+v, want %q %+v", c.src, rest, m, c.rest, c.m)
		}
		// AC5b: the store's local check agrees (A-003).
		if got, want := store.HasLeadingMarker(c.src), rest != c.src; got != want {
			t.Errorf("store.HasLeadingMarker(%q) = %v, ConvertLeadingMarker changes it: %v", c.src, got, want)
		}
		if store.HasLeadingMarker(rest) {
			t.Errorf("ConvertLeadingMarker(%q) left a leading marker: %q", c.src, rest)
		}
	}
}

func TestWrapSelection(t *testing.T) {
	src := "say hello now"
	sel := markup.Range{Start: 4, End: 9}
	once, sel1 := markup.WrapSelection(src, sel, "*")
	if once != "say *hello* now" || once[sel1.Start:sel1.End] != "hello" {
		t.Fatalf("first wrap = %q %v", once, sel1)
	}
	twice, sel2 := markup.WrapSelection(once, sel1, "*")
	if twice != "say **hello** now" || twice[sel2.Start:sel2.End] != "hello" {
		t.Fatalf("second wrap = %q %v", twice, sel2)
	}
	if got := fmtRuns(markup.Parse(twice).Concealed()); got != ":say |b:hello|: now" {
		t.Errorf("second wrap renders %q, want bold", got)
	}
	thrice, _ := markup.WrapSelection(twice, sel2, "*")
	if got := fmtRuns(markup.Parse(thrice).Concealed()); got != ":say |bi:hello|: now" {
		t.Errorf("third wrap renders %q, want bold italic", got)
	}

	strike, _ := markup.WrapSelection("ab", markup.Range{Start: 0, End: 2}, "~")
	strike, _ = markup.WrapSelection(strike, markup.Range{Start: 1, End: 3}, "~")
	if strike != "~~ab~~" {
		t.Errorf("~ twice = %q, want ~~ab~~", strike)
	}

	// Out of range and mid-rune selections are clamped and widened.
	got, r := markup.WrapSelection("é", markup.Range{Start: 1, End: 99}, "`")
	if got != "`é`" || r != (markup.Range{Start: 1, End: 3}) {
		t.Errorf("clamped wrap = %q %v", got, r)
	}
}

func TestSetColor(t *testing.T) {
	cases := []struct {
		src  string
		c    markup.Color
		want string
	}{
		{"a", markup.Red, "<red>a</red>"},
		{"<red>**a** b</red>", markup.Blue, "<blue>**a** b</blue>"},
		{"<red>a</red>", markup.NoColor, "a"},
		{"x <red>a</red> y", markup.Green, "<green>x a y</green>"},
		{"<red>a</red> <blue>b</blue>", markup.Gray, "<gray>a b</gray>"},
		{"<pink>a</pink>", markup.Red, "<red><pink>a</pink></red>"},
		{"<red>a <blue>b</blue></red>", markup.Blue, `<blue>a \<blue>b\</blue></blue>`},
		{"<red>a <blue>b</blue></red>", markup.NoColor, `a \<blue>b\</blue>`},
		{"a</red>b", markup.Red, `<red>a\</red>b</red>`},
		{`<red>a\</red>`, markup.Red, `<red>\<red>a\</red></red>`},
		{"`<red>a</red>`", markup.Red, "<red>`<red>a</red>`</red>"},
		{`a\`, markup.Red, `<red>a\\</red>`},
		{`a\\`, markup.Red, `<red>a\\</red>`},
		{"", markup.Red, ""},
		{"plain", markup.NoColor, "plain"},
	}
	for _, c := range cases {
		got := markup.SetColor(c.src, c.c)
		if got != c.want {
			t.Errorf("SetColor(%q, %v) = %q, want %q", c.src, c.c, got, c.want)
		}
	}
	// Inner styles survive a color change.
	got := fmtRuns(markup.Parse(markup.SetColor("<red>**a** *b*</red>", markup.Cyan)).Concealed())
	if got != "b@cyan:a|@cyan: |i@cyan:b" {
		t.Errorf("recolored runs = %q", got)
	}
}

func TestExportColors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"<red>a</red> b", `<span style="color: red">a</span> b`},
		{"<gray>a</gray><purple>b</purple>", `<span style="color: gray">a</span><span style="color: purple">b</span>`},
		{"<red>a", "<red>a"},
		{"`<red>a</red>`", "`<red>a</red>`"},
		{"<red>a <blue>b</blue></red>", `<span style="color: red">a <blue>b</blue></span>`},
		{"no color", "no color"},
	}
	for _, c := range cases {
		if got := markup.ExportColors(c.src); got != c.want {
			t.Errorf("ExportColors(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestColorByName(t *testing.T) {
	for _, c := range markup.Colors() {
		got, ok := markup.ColorByName(c.String())
		if !ok || got != c {
			t.Errorf("ColorByName(%q) = %v %v", c.String(), got, ok)
		}
	}
	if _, ok := markup.ColorByName("pink"); ok {
		t.Error("pink is not a color")
	}
}

// checkDoc verifies the invariants every parse must keep.
func checkDoc(t *testing.T, src string) {
	t.Helper()
	d := markup.Parse(src)

	// Revealed covers the source byte for byte (AC2).
	at := 0
	var b strings.Builder
	for _, r := range d.Revealed() {
		if r.Src.Start != at || r.Text != src[r.Src.Start:r.Src.End] || r.Text == "" {
			t.Fatalf("Parse(%q): bad revealed run %+v at %d", src, r, at)
		}
		at = r.Src.End
		b.WriteString(r.Text)
	}
	if b.String() != src {
		t.Fatalf("Parse(%q): revealed text %q", src, b.String())
	}

	// Markers are sorted, disjoint and in bounds.
	markers := d.Markers()
	end := 0
	isMarker := make([]bool, len(src))
	for _, m := range markers {
		if m.Start < end || m.End <= m.Start || m.End > len(src) {
			t.Fatalf("Parse(%q): bad markers %v", src, markers)
		}
		for i := m.Start; i < m.End; i++ {
			isMarker[i] = true
		}
		end = m.End
	}

	// Concealed runs are the non-marker bytes, in order.
	at = 0
	for _, r := range d.Concealed() {
		if r.Src.Start < at || r.Text != src[r.Src.Start:r.Src.End] || r.Style&markup.Marker != 0 {
			t.Fatalf("Parse(%q): bad concealed run %+v", src, r)
		}
		for i := at; i < r.Src.Start; i++ {
			if !isMarker[i] {
				t.Fatalf("Parse(%q): byte %d is hidden but not a marker", src, i)
			}
		}
		for i := r.Src.Start; i < r.Src.End; i++ {
			if isMarker[i] {
				t.Fatalf("Parse(%q): marker byte %d shown", src, i)
			}
		}
		if (r.Style&markup.Link != 0) != (r.Link != nil) {
			t.Fatalf("Parse(%q): run %+v link mismatch", src, r)
		}
		at = r.Src.End
	}
	for i := at; i < len(src); i++ {
		if !isMarker[i] {
			t.Fatalf("Parse(%q): trailing byte %d hidden but not a marker", src, i)
		}
	}

	plain, offsets := markup.Plain(src)
	if len(offsets) != len(plain)+1 {
		t.Fatalf("Plain(%q): %d offsets for %d bytes", src, len(offsets), len(plain))
	}
	for i := 0; i < len(plain); i++ {
		if src[offsets[i]] != plain[i] {
			t.Fatalf("Plain(%q): offset %d maps to %q, want %q", src, i, src[offsets[i]], plain[i])
		}
	}
}

var seedCorpus = []string{
	"", "**bold *and italic***", "2 * 3 * 4", "`**x**`", "<red>a <blue>b</blue></red>",
	"[a](https://x)", `\*\\`, "[[a](https://x)](https://y)", "á 😀 **ü**",
	"[a](https://x`y)` z", "<red>a\\</red>", "***x***", "~~a==b~~==",
}

func FuzzRevealedIdentity(f *testing.F) {
	for _, s := range seedCorpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkDoc(t, src)
	})
}

func FuzzParse(f *testing.F) {
	for _, s := range seedCorpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		d := markup.Parse(src)
		_ = d.Concealed()
		_ = d.Links()

		// Coloring wraps the whole text and keeps what it shows.
		for _, c := range []markup.Color{markup.Red, markup.NoColor} {
			out := markup.SetColor(src, c)
			bare := markup.SetColor(src, markup.NoColor)
			if p1, _ := markup.Plain(out); c != markup.NoColor && bare != "" {
				p2, _ := markup.Plain(bare)
				if p1 != p2 {
					t.Fatalf("SetColor(%q): plain %q, uncolored %q", src, p1, p2)
				}
				for _, r := range markup.Parse(out).Concealed() {
					if r.Color != c {
						t.Fatalf("SetColor(%q) = %q: run %+v not colored", src, out, r)
					}
				}
			}
		}

		rest, _ := markup.ConvertLeadingMarker(src)
		if store.HasLeadingMarker(rest) || store.HasLeadingMarker(src) != (rest != src) {
			t.Fatalf("ConvertLeadingMarker(%q) = %q disagrees with store.HasLeadingMarker", src, rest)
		}
	})
}

// TestParse_Linear checks AC4's budget: 10 KB of hostile input parses in
// under 10 ms (more under the race detector).
func TestParse_Linear(t *testing.T) {
	budget := 10 * time.Millisecond
	if raceEnabled {
		budget *= 20
	}
	for name, in := range linearInputs(10_000) {
		if d := bestParse(in); d > budget {
			t.Errorf("%s: parse took %v, budget %v", name, d, budget)
		}
	}
	// Growth, which a fixed budget only catches near its edge: eight times
	// the input takes about 8× as long when linear and 64× when quadratic.
	big := linearInputs(80_000)
	for name, in := range linearInputs(10_000) {
		small := bestParse(in)
		if r := float64(bestParse(big[name])) / float64(max(small, 50*time.Microsecond)); r > 32 {
			t.Errorf("%s: 8× the input took %.1f× as long (%v at 10k)", name, r, small)
		}
	}
}

// linearInputs are adversarial inputs of n bytes for the delimiter stack,
// brackets, links and tags.
func linearInputs(n int) map[string]string {
	rep := func(s string) string { return strings.Repeat(s, n/len(s)+1)[:n] }
	r := rand.New(rand.NewPCG(1, 2))
	random := make([]byte, n)
	const alphabet = "*~=`<>[]()\\ab /:redhtps"
	for i := range random {
		random[i] = alphabet[r.IntN(len(alphabet))]
	}
	return map[string]string{
		"stars":         rep("*a "),
		"star runs":     rep("**a"),
		"open stars":    rep("*a"),
		"close stars":   rep("a* "),
		"tildes":        rep("~~a "),
		"tildes+stars":  rep("~~a ") + rep("b* "),
		"tags+stars":    rep("<red>a ") + rep("b* "),
		"stars+tags":    rep("*a ") + rep("</blue>"),
		"backticks":     rep("`a "),
		"brackets":      rep("["),
		"close":         rep("]"),
		"open dest":     rep("[a]("),
		"open urls":     rep("[a](http://"),
		"nested urls":   rep("[a](https://x"),
		"tags":          rep("<red>"),
		"close tags":    rep("</red>"),
		"mixed tags":    rep("<red><blue></red>"),
		"links":         rep("[a](https://x)"),
		"nested stars":  strings.Repeat("*", n/2) + strings.Repeat("a*", n/4),
		"random":        string(random),
		"escapes":       rep(`\*`),
		"code in label": rep("[`a]` "),
	}
}

// bestParse is the fastest of five parses, to keep noise out.
func bestParse(in string) time.Duration {
	best := time.Hour
	for range 5 {
		start := time.Now()
		d := markup.Parse(in)
		_ = d.Concealed()
		best = min(best, time.Since(start))
	}
	return best
}

func BenchmarkParse(b *testing.B) {
	src := "**bold** and *italic* with `code`, a [link](https://example.org) and <red>color</red> ~~done~~"
	for b.Loop() {
		_ = markup.Parse(src).Concealed()
	}
}
