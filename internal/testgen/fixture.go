package testgen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// RootID is the root of every fixture.
const RootID = model.NodeID("0000000000zz")

// FixtureTime is the fixed time of fixtures and of their clock.
var FixtureTime = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

// State is what the notation says besides the tree (CONTRACTS §11).
type State struct {
	Zoom      model.NodeID // "" for Home
	Cursor    model.NodeID // "" for none
	CursorOff int          // bytes in the stored text
	SelNode   model.NodeID // "" for none
	SelStart  int
	SelEnd    int
	Block     []model.NodeID        // {sel}
	FixedIDs  map[model.NodeID]bool // written back as {id=…}
}

// Fixture is a parsed outline.
type Fixture struct {
	Tree  *core.Tree
	State State
	next  int
}

// genID returns the n-th generated ID; octal digits are all in the ID alphabet.
func genID(n int) model.NodeID {
	return model.NodeID(fmt.Sprintf("%012s", strconv.FormatInt(int64(n), 8)))
}

// NewID returns IDs in sequence, so tests are repeatable.
func (f *Fixture) NewID() model.NodeID {
	f.next++
	return genID(f.next)
}

// Ctx returns a planning context with the fixture's clock and IDs.
func (f *Fixture) Ctx() core.Ctx {
	return core.Ctx{Tree: f.Tree, Clock: func() time.Time { return FixtureTime }, Source: "ui", NewID: f.NewID}
}

// ID finds the node with this stored text, which must be unique.
func (f *Fixture) ID(t testing.TB, text string) model.NodeID {
	t.Helper()
	var found []model.NodeID
	for _, id := range f.Tree.AllNodes() {
		if n, _ := f.Tree.Node(id); n.Text == text && id != RootID {
			found = append(found, id)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d nodes have the text %q", len(found), text)
	}
	return found[0]
}

// ApplyAndFormat applies a plan to the tree and renders the result.
func (f *Fixture) ApplyAndFormat(t testing.TB, p core.Plan) string {
	t.Helper()
	if p.ChangeSet != nil {
		if err := f.Tree.Apply(*p.ChangeSet); err != nil {
			t.Fatal(err)
		}
	}
	return FormatOutline(f.Tree, f.State)
}

// MustParseOutline parses or fails the test.
func MustParseOutline(t testing.TB, s string) *Fixture {
	t.Helper()
	f, err := ParseOutline(s)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// line is one parsed line of a fixture.
type line struct {
	node      model.Node
	depth     int
	trashed   bool
	collapsed bool
	star      bool
	page      *string
}

// ParseOutline reads the fixture notation (CONTRACTS §11).
func ParseOutline(s string) (*Fixture, error) {
	f := &Fixture{State: State{FixedIDs: map[model.NodeID]bool{}}}
	var lines []*line
	for i, text := range strings.Split(s, "\n") {
		l, err := f.parseLine(text, strings.ContainsRune(s, '‸'))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		prev := -1
		if len(lines) > 0 {
			prev = lines[len(lines)-1].depth
		}
		if l.depth > prev+1 {
			return nil, fmt.Errorf("line %d: indented too far", i+1)
		}
		lines = append(lines, l)
	}
	d, err := buildDump(lines)
	if err != nil {
		return nil, err
	}
	f.Tree = core.NewTree(d)
	f.Tree.SetPages(d.Pages)
	return f, nil
}

func (f *Fixture) parseLine(text string, explicitCaret bool) (*line, error) {
	rest := strings.TrimLeft(text, " ")
	indent := len(text) - len(rest)
	if indent%2 != 0 {
		return nil, fmt.Errorf("odd indent in %q", text)
	}
	if rest != "-" && !strings.HasPrefix(rest, "- ") {
		return nil, fmt.Errorf("no bullet in %q", text)
	}
	now := model.NewTime(FixtureTime)
	l := &line{depth: indent / 2}
	l.node = model.Node{ID: f.NewID(), CreatedAt: now, UpdatedAt: now, Version: 1}
	body, attrs := splitAttrs(strings.TrimPrefix(strings.TrimPrefix(rest, "-"), " "))
	for _, a := range attrs { // the ID first, since other attributes record it
		if value, ok := strings.CutPrefix(a, "id="); ok {
			l.node.ID = model.NodeID(value)
			f.State.FixedIDs[l.node.ID] = true
		}
	}
	for _, a := range attrs {
		name, value, _ := strings.Cut(a, "=")
		switch name {
		case "zoom":
			f.State.Zoom = l.node.ID
		case "collapsed":
			l.collapsed = true
		case "sel":
			f.State.Block = append(f.State.Block, l.node.ID)
		case "star":
			l.star = true
		case "trashed":
			l.trashed = true
		case "page":
			l.page = &value
		}
	}
	switch {
	case strings.HasPrefix(body, "[ ] "):
		l.node.IsTask = true
		body = body[4:]
	case strings.HasPrefix(body, "[x] "):
		l.node.IsTask = true
		l.node.CompletedAt = &now
		body = body[4:]
	}
	if strings.HasPrefix(body, "## ") {
		l.node.IsHeading = true
		body = body[3:]
	}
	l.node.Text = f.parseMarks(body, l.node.ID, explicitCaret)
	return l, nil
}

var knownAttrs = []string{"zoom", "collapsed", "sel", "star", "trashed", "page", "id"}

// splitAttrs takes a trailing {…} if every word in it is an attribute; other
// braces are text.
func splitAttrs(body string) (string, []string) {
	i := strings.LastIndex(body, "{")
	if !strings.HasSuffix(body, "}") || i < 0 || (i > 0 && body[i-1] != ' ') {
		return body, nil
	}
	attrs := strings.Fields(body[i+1 : len(body)-1])
	for _, a := range attrs {
		name, _, _ := strings.Cut(a, "=")
		if !slices.Contains(knownAttrs, name) {
			return body, nil
		}
	}
	if len(attrs) == 0 {
		return body, nil
	}
	return strings.TrimSuffix(body[:i], " "), attrs
}

// parseMarks removes ‸ ⟦ ⟧, recording them as byte offsets in the stored text.
// The cursor sits at ⟧ unless ‸ appears anywhere in the fixture.
func (f *Fixture) parseMarks(s string, id model.NodeID, explicitCaret bool) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '‸':
			f.State.Cursor, f.State.CursorOff = id, b.Len()
		case '⟦':
			f.State.SelNode, f.State.SelStart = id, b.Len()
		case '⟧':
			f.State.SelEnd = b.Len()
			if !explicitCaret {
				f.State.Cursor, f.State.CursorOff = id, b.Len()
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func buildDump(lines []*line) (*store.Dump, error) {
	now := model.NewTime(FixtureTime)
	root := model.Node{ID: RootID, Position: "a0", CreatedAt: now, UpdatedAt: now, Version: 1}
	d := &store.Dump{
		Nodes:     []model.Node{root},
		Pages:     map[model.NodeID]model.Page{},
		Stars:     map[model.NodeID]model.Time{},
		Collapsed: map[model.NodeID]bool{},
		Meta:      map[string]string{"root_id": string(RootID)},
	}
	counts := map[model.NodeID]int{}
	stack := []*line{}
	for _, l := range lines {
		stack = stack[:l.depth]
		l.node.ParentID = RootID
		if l.depth > 0 {
			l.node.ParentID = stack[l.depth-1].node.ID
		}
		counts[l.node.ParentID]++
		stack = append(stack, l)
	}
	keys := map[model.NodeID][]string{}
	for parent, n := range counts {
		k, err := fracindex.NBetween("", "", n)
		if err != nil {
			return nil, err
		}
		keys[parent] = k
	}
	stack = stack[:0]
	for i, l := range lines {
		stack = stack[:l.depth]
		l.node.Position = keys[l.node.ParentID][0]
		keys[l.node.ParentID] = keys[l.node.ParentID][1:]
		if l.trashed {
			// A trashed line under a trashed line is the same deletion.
			by := model.ChangeSetID(genID(1_000_000 + i))
			if l.depth > 0 && stack[l.depth-1].trashed {
				by = stack[l.depth-1].node.DeletedBy
			}
			l.node.DeletedAt, l.node.DeletedBy = &now, by
		}
		stack = append(stack, l)
		d.Nodes = append(d.Nodes, l.node)
		if l.page != nil {
			d.Pages[l.node.ID] = model.Page{NodeID: l.node.ID, Markdown: *l.page, UpdatedAt: now, Version: 1}
		}
		if l.star {
			d.Stars[l.node.ID] = now
		}
		if l.collapsed {
			d.Collapsed[l.node.ID] = true
		}
	}
	return d, nil
}

// FormatOutline renders a tree in the fixture notation, trashed lines included
// (CONTRACTS §11).
func FormatOutline(tr *core.Tree, s State) string {
	kids := map[model.NodeID][]*model.Node{}
	for _, id := range tr.AllNodes() {
		if n, _ := tr.Node(id); id != tr.Root() {
			kids[n.ParentID] = append(kids[n.ParentID], n)
		}
	}
	var out []string
	var walk func(parent model.NodeID, depth int)
	walk = func(parent model.NodeID, depth int) {
		list := kids[parent]
		slices.SortStableFunc(list, func(a, b *model.Node) int { return strings.Compare(a.Position, b.Position) })
		for _, n := range list {
			out = append(out, strings.Repeat("  ", depth)+formatLine(tr, s, n))
			walk(n.ID, depth+1)
		}
	}
	walk(tr.Root(), 0)
	return strings.Join(out, "\n")
}

func formatLine(tr *core.Tree, s State, n *model.Node) string {
	prefix := ""
	switch {
	case n.CompletedAt != nil:
		prefix = "[x] "
	case n.IsTask:
		prefix = "[ ] "
	}
	if n.IsHeading {
		prefix += "## "
	}
	text := prefix + formatMarks(s, n)

	var attrs []string
	add := func(on bool, a string) {
		if on {
			attrs = append(attrs, a)
		}
	}
	add(s.Zoom == n.ID, "zoom")
	add(tr.Collapsed(n.ID), "collapsed")
	if p, ok := tr.Page(n.ID); ok {
		if p.Markdown == "" {
			attrs = append(attrs, "page")
		} else {
			attrs = append(attrs, "page="+p.Markdown)
		}
	}
	_, starred := tr.StarredAt(n.ID)
	add(starred, "star")
	add(slices.Contains(s.Block, n.ID), "sel")
	add(n.DeletedAt != nil, "trashed")
	add(s.FixedIDs[n.ID], "id="+string(n.ID))
	switch {
	case len(attrs) == 0:
		return "- " + text
	case text == "":
		return "- {" + strings.Join(attrs, " ") + "}"
	}
	return "- " + text + " {" + strings.Join(attrs, " ") + "}"
}

// formatMarks puts ‸ ⟦ ⟧ back into the text. ‸ is left out when the cursor sits
// at ⟧, where the notation implies it.
func formatMarks(s State, n *model.Node) string {
	hasSel := s.SelNode == n.ID
	hasCursor := s.Cursor == n.ID && (!hasSel || s.CursorOff != s.SelEnd)
	if !hasSel && !hasCursor {
		return n.Text
	}
	var b strings.Builder
	for i := 0; i <= len(n.Text); i++ {
		if hasSel && i == s.SelEnd {
			b.WriteRune('⟧')
		}
		if hasCursor && i == s.CursorOff {
			b.WriteRune('‸')
		}
		if hasSel && i == s.SelStart {
			b.WriteRune('⟦')
		}
		if i < len(n.Text) {
			b.WriteByte(n.Text[i])
		}
	}
	return b.String()
}

// Equal fails the test if got differs from want.
func Equal[T comparable](t testing.TB, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("got:\n%v\nwant:\n%v", got, want)
	}
}
