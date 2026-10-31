package main

import (
	"fmt"
	"math/rand"
	"strings"
	"unicode/utf16"
)

type node struct {
	id        string
	parent    *node
	children  []*node
	text      string
	lower     string
	collapsed bool
	done      bool
	task      bool
	heading   bool
	page      bool
	deleted   bool
}

// cursor is the session's selection state. Off and End are UTF-16 offsets, the
// unit the browser reports.
type cursor struct {
	id     string
	off    int
	block  []string
	anchor string
}

type step struct {
	snaps         map[string]*node
	before, after cursor
}

// Tree is the fake core: an in-memory tree and the §5 rules. Snapshots of touched
// nodes give undo for free; swapping a step's snapshots in applies it, and
// swapping again reverses that.
type Tree struct {
	nodes  map[string]*node
	root   *node
	zoom   *node
	cur    cursor
	filter string
	stay   map[string]bool
	msg    string
	undo   []*step
	redo   []*step
	tx     *step
	typing *step
	nextID int
}

type rowRef struct {
	n     *node
	depth int
}

func newTree() *Tree {
	r := &node{id: "root", text: "Home"}
	return &Tree{nodes: map[string]*node{"root": r}, root: r, zoom: r, stay: map[string]bool{}}
}

func u16len(s string) int { return len(utf16.Encode([]rune(s))) }

func u16split(s string, off int) (string, string) {
	u := utf16.Encode([]rune(s))
	off = max(0, min(off, len(u)))
	return string(utf16.Decode(u[:off])), string(utf16.Decode(u[off:]))
}

func (t *Tree) add(parent *node, id, text string) *node {
	n := &node{id: id, parent: parent, text: text, lower: strings.ToLower(text)}
	t.nodes[id] = n
	parent.children = append(parent.children, n)
	return n
}

// parseSeed reads test fixtures: two spaces per depth, then id|flags|text. Flags
// are c (collapsed), p (page), x (done).
func parseSeed(src string) *Tree {
	t := newTree()
	stack := []*node{t.root}
	for line := range strings.Lines(src) {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		trim := strings.TrimLeft(line, " ")
		depth := (len(line) - len(trim)) / 2
		f := strings.SplitN(trim, "|", 3)
		stack = stack[:depth+1]
		n := t.add(stack[depth], f[0], f[2])
		n.collapsed = strings.Contains(f[1], "c")
		n.page = strings.Contains(f[1], "p")
		n.done = strings.Contains(f[1], "x")
		stack = append(stack, n)
	}
	if len(t.root.children) > 0 {
		t.cur.id = t.root.children[0].id
	}
	return t
}

// generate builds a repeatable outline of total nodes with about expanded of them
// visible, and every sixth bullet long enough to wrap.
func generate(total, expanded int) *Tree {
	t := newTree()
	r := rand.New(rand.NewSource(1))
	words := strings.Fields("mycelium outline render tooltip controller inventory lighthouse design notes project review zürich café")
	queue := []*node{t.root}
	for i := 0; i < total; i++ {
		p := queue[0]
		if len(p.children) >= 10 {
			queue = queue[1:]
			p = queue[0]
		}
		n := 4
		if i%6 == 0 {
			n = 40
		}
		var b strings.Builder
		for j := range n {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(words[r.Intn(len(words))])
		}
		queue = append(queue, t.add(p, fmt.Sprintf("n%d", i), b.String()))
	}
	seen := 0
	var walk func(*node)
	walk = func(n *node) {
		for _, c := range n.children {
			seen++
			if len(c.children) > 0 {
				if seen >= expanded {
					c.collapsed = true
				} else {
					walk(c)
				}
			}
		}
	}
	walk(t.root)
	t.cur.id = t.root.children[0].id
	return t
}

// Transactions.

func (t *Tree) begin() {
	t.seal()
	t.tx = &step{snaps: map[string]*node{}, before: t.cur}
}

func (t *Tree) touch(n *node) {
	if t.tx == nil {
		return
	}
	if _, ok := t.tx.snaps[n.id]; !ok {
		c := *n
		c.children = append([]*node(nil), n.children...)
		t.tx.snaps[n.id] = &c
	}
}

func (t *Tree) commit() {
	s := t.tx
	t.tx = nil
	if s == nil || len(s.snaps) == 0 {
		return
	}
	s.after = t.cur
	t.undo = append(t.undo, s)
	t.redo = nil
}

func (t *Tree) seal() {
	if t.typing != nil {
		t.typing.after = t.cur
		t.typing = nil
	}
}

func (t *Tree) swap(s *step) {
	for id, snap := range s.snaps {
		n := t.nodes[id]
		cur := *n
		*n = *snap
		s.snaps[id] = &cur
	}
}

// Undo implements §5.6.
func (t *Tree) Undo() {
	t.seal()
	if len(t.undo) == 0 {
		t.msg = "Nothing to undo"
		return
	}
	s := t.undo[len(t.undo)-1]
	t.undo = t.undo[:len(t.undo)-1]
	t.swap(s)
	t.redo = append(t.redo, s)
	t.cur = s.before
	t.showCursor()
}

// Redo reapplies the last undone step.
func (t *Tree) Redo() {
	t.seal()
	if len(t.redo) == 0 {
		t.msg = "Nothing to redo"
		return
	}
	s := t.redo[len(t.redo)-1]
	t.redo = t.redo[:len(t.redo)-1]
	t.swap(s)
	t.undo = append(t.undo, s)
	t.cur = s.after
	t.showCursor()
}

func (t *Tree) showCursor() {
	n := t.nodes[t.cur.id]
	for a := n; a != nil; a = a.parent {
		if a == t.zoom {
			return
		}
	}
	t.zoom = n.parent
	t.msg = "Zoomed to show the undone change"
}

// SetText applies typing, one step per node until sealed (§5.6, §7.3).
func (t *Tree) SetText(id, text string, off int) {
	n := t.nodes[id]
	if n == nil {
		return
	}
	if t.typing == nil || t.typing.snaps[id] == nil {
		t.seal()
		c := *n
		c.children = append([]*node(nil), n.children...)
		t.typing = &step{snaps: map[string]*node{id: &c}, before: t.cur}
		if t.cur.id != id {
			t.typing.before = cursor{id: id, off: off}
		}
		t.undo = append(t.undo, t.typing)
		t.redo = nil
	}
	n.text, n.lower = text, strings.ToLower(text)
	t.stay[id] = t.filter != ""
	t.cur = cursor{id: id, off: off}
}

// Views.

func (t *Tree) matches() map[*node]bool {
	vis := map[*node]bool{}
	q := strings.ToLower(t.filter)
	var walk func(*node) bool
	walk = func(n *node) bool {
		any := strings.Contains(n.lower, q) || t.stay[n.id]
		for _, c := range n.children {
			if walk(c) {
				any = true
			}
		}
		if any {
			vis[n] = true
		}
		return any
	}
	walk(t.zoom)
	return vis
}

// Flatten returns the visible rows; a zoom other than Home gets a heading row.
func (t *Tree) Flatten() []rowRef {
	rows := make([]rowRef, 0, 256)
	var vis map[*node]bool
	if t.filter != "" {
		vis = t.matches()
	}
	if t.zoom != t.root {
		rows = append(rows, rowRef{t.zoom, -1})
	}
	var walk func(*node, int)
	walk = func(n *node, d int) {
		for _, c := range n.children {
			if vis != nil && !vis[c] {
				continue
			}
			rows = append(rows, rowRef{c, d})
			if vis != nil || !c.collapsed {
				walk(c, d+1)
			}
		}
	}
	walk(t.zoom, 0)
	return rows
}

func indexOf(rows []rowRef, id string) int {
	for i, r := range rows {
		if r.n.id == id {
			return i
		}
	}
	return -1
}

func (t *Tree) neighbor(id string, dir int) *node {
	rows := t.Flatten()
	i := indexOf(rows, id) + dir
	if i < 0 || i >= len(rows) {
		return nil
	}
	return rows[i].n
}

// Tree edits.

func sibIndex(n *node) int {
	for i, c := range n.parent.children {
		if c == n {
			return i
		}
	}
	return -1
}

func (t *Tree) insert(p *node, i int, n *node) {
	t.touch(p)
	t.touch(n)
	k := append([]*node(nil), p.children[:i]...)
	k = append(k, n)
	p.children = append(k, p.children[i:]...)
	n.parent = p
}

func (t *Tree) detach(n *node) int {
	p := n.parent
	i := sibIndex(n)
	t.touch(p)
	t.touch(n)
	k := append([]*node(nil), p.children[:i]...)
	p.children = append(k, p.children[i+1:]...)
	return i
}

func (t *Tree) create(p *node, i int, text string) *node {
	t.nextID++
	n := &node{id: fmt.Sprintf("new%d", t.nextID), text: text, lower: strings.ToLower(text)}
	c := *n
	t.nodes[n.id] = n
	t.tx.snaps[n.id] = &c
	t.tx.snaps[n.id].deleted = true
	t.insert(p, i, n)
	if t.filter != "" {
		t.stay[n.id] = true
	}
	return n
}

func (t *Tree) setText(n *node, s string) {
	t.touch(n)
	n.text, n.lower = s, strings.ToLower(s)
}

func (t *Tree) outdent(n *node) bool {
	if n.parent == t.zoom || n == t.zoom {
		t.msg = "Already at the top level of this zoom"
		return false
	}
	p := n.parent
	t.detach(n)
	t.insert(p.parent, sibIndex(p)+1, n)
	return true
}

func (t *Tree) indent(n *node) bool {
	i := sibIndex(n)
	if i <= 0 || n == t.zoom {
		t.msg = "The first sibling can't be indented"
		return false
	}
	prev := n.parent.children[i-1]
	t.touch(prev)
	prev.collapsed = false
	t.detach(n)
	t.insert(prev, len(prev.children), n)
	return true
}

// Enter implements the §5.2 table.
func (t *Tree) Enter(off, end int) {
	n := t.nodes[t.cur.id]
	t.begin()
	defer t.commit()
	if off != end {
		a, _ := u16split(n.text, min(off, end))
		_, b := u16split(n.text, max(off, end))
		t.setText(n, a+b)
		off = min(off, end)
	}
	switch {
	case n == t.zoom:
		t.touch(n)
		n.collapsed = false
		t.cur = cursor{id: t.create(n, 0, "").id}
	case n.text == "":
		if n.parent != t.zoom {
			t.outdent(n)
			t.cur = cursor{id: n.id}
		} else {
			t.cur = cursor{id: t.create(n.parent, sibIndex(n)+1, "").id}
		}
	case off == 0:
		t.create(n.parent, sibIndex(n), "")
		t.cur = cursor{id: n.id}
	case off >= u16len(n.text):
		if len(n.children) > 0 && !n.collapsed {
			t.cur = cursor{id: t.create(n, 0, "").id}
		} else {
			t.cur = cursor{id: t.create(n.parent, sibIndex(n)+1, "").id}
		}
	default:
		a, b := u16split(n.text, off)
		t.setText(n, a)
		t.cur = cursor{id: t.create(n.parent, sibIndex(n)+1, b).id}
	}
}

func (t *Tree) trash(n *node) {
	t.detach(n)
	n.deleted = true
}

// mergeInto moves src's text, children and page onto dst, then trashes src.
func (t *Tree) mergeInto(dst, src *node) {
	t.touch(dst)
	dst.text += src.text
	dst.lower = strings.ToLower(dst.text)
	dst.page = dst.page || src.page
	for _, c := range append([]*node(nil), src.children...) {
		t.detach(c)
		t.insert(dst, len(dst.children), c)
	}
	t.trash(src)
}

// Backspace implements the §5.3 table at offset 0; the browser handles the rest.
func (t *Tree) Backspace() {
	n := t.nodes[t.cur.id]
	prev := t.neighbor(n.id, -1)
	t.begin()
	defer t.commit()
	switch {
	case prev == nil || n == t.zoom:
	case n.text == "" && len(n.children) == 0:
		t.trash(n)
		t.cur = cursor{id: prev.id, off: u16len(prev.text)}
	case n.text == "":
		t.msg = "Use Esc then Delete to remove this bullet and its children"
	case prev == t.zoom:
	case n.page && prev.page:
		t.msg = "Both bullets have a page, so they can't merge"
	case len(n.children) > 0 && len(prev.children) > 0:
		t.msg = "Both bullets have children, so they can't merge"
	default:
		off := u16len(prev.text)
		t.mergeInto(prev, n)
		t.cur = cursor{id: prev.id, off: off}
	}
}

// Delete implements the §5.3a table at the end of a bullet.
func (t *Tree) Delete() {
	n := t.nodes[t.cur.id]
	next := t.neighbor(n.id, 1)
	off := u16len(n.text)
	t.begin()
	defer t.commit()
	switch {
	case next == nil:
	case next.parent == n && n.children[0] == next:
		t.msg = "The next bullet is this bullet's own child"
	case next.text == "" && len(next.children) == 0:
		t.trash(next)
	case n.page && next.page:
		t.msg = "Both bullets have a page, so they can't merge"
	case len(next.children) > 0 && len(n.children) > 0:
		t.msg = "Both bullets have children, so they can't merge"
	default:
		t.mergeInto(n, next)
		t.cur = cursor{id: n.id, off: off}
	}
}

func (t *Tree) targets() []*node {
	var out []*node
	if len(t.cur.block) == 0 {
		return []*node{t.nodes[t.cur.id]}
	}
	for _, id := range t.cur.block {
		out = append(out, t.nodes[id])
	}
	return out
}

// Indent and Outdent implement §5.4 for the current bullet or selection.
func (t *Tree) Indent() {
	t.begin()
	defer t.commit()
	for _, n := range t.targets() {
		if !t.indent(n) {
			return
		}
	}
}

// Outdent keeps the selection's order by working from its last bullet.
func (t *Tree) Outdent() {
	ns := t.targets()
	t.begin()
	defer t.commit()
	for i := len(ns) - 1; i >= 0; i-- {
		if !t.outdent(ns[i]) {
			return
		}
	}
}

func (t *Tree) moveOne(n *node, dir int) {
	p := n.parent
	i := sibIndex(n)
	if n == t.zoom {
		return
	}
	if j := i + dir; j >= 0 && j < len(p.children) {
		t.detach(n)
		t.insert(p, j, n)
		return
	}
	if p == t.zoom {
		return
	}
	pi := sibIndex(p)
	if k := pi + dir; k >= 0 && k < len(p.parent.children) {
		adj := p.parent.children[k]
		t.touch(adj)
		adj.collapsed = false
		t.detach(n)
		if dir < 0 {
			t.insert(adj, len(adj.children), n)
		} else {
			t.insert(adj, 0, n)
		}
		return
	}
	t.detach(n)
	if dir < 0 {
		t.insert(p.parent, pi, n)
	} else {
		t.insert(p.parent, pi+1, n)
	}
}

// Move implements Move up/down (§5.4).
func (t *Tree) Move(dir int) {
	ns := t.targets()
	t.begin()
	defer t.commit()
	if dir > 0 {
		for i := len(ns) - 1; i >= 0; i-- {
			t.moveOne(ns[i], dir)
		}
		return
	}
	for _, n := range ns {
		t.moveOne(n, dir)
	}
}

// ToggleDone implements Complete / reopen: any open bullet means complete all.
func (t *Tree) ToggleDone() {
	ns := t.targets()
	done := true
	for _, n := range ns {
		done = done && n.done
	}
	t.begin()
	defer t.commit()
	for _, n := range ns {
		t.touch(n)
		n.done = !done
	}
}

// Toggle flips a bullet flag named by the block-selection keys.
func (t *Tree) Toggle(what string) {
	ns := t.targets()
	t.begin()
	defer t.commit()
	for _, n := range ns {
		t.touch(n)
		switch what {
		case "task":
			n.task = !n.task
		case "heading":
			n.heading = !n.heading
		}
	}
}

// SetCollapsed is view state, so it is not an undo step.
func (t *Tree) SetCollapsed(v bool) {
	for _, n := range t.targets() {
		if len(n.children) > 0 {
			n.collapsed = v
		}
	}
}

// Block selection (§5.1).

func (t *Tree) BlockEnter() {
	t.seal()
	t.cur.block = []string{t.cur.id}
	t.cur.anchor = t.cur.id
}

func (t *Tree) BlockExit() {
	t.cur.block, t.cur.anchor = nil, ""
	if n := t.nodes[t.cur.id]; n != nil {
		t.cur.off = u16len(n.text)
	}
}

func (t *Tree) BlockMove(dir int) {
	if n := t.neighbor(t.cur.id, dir); n != nil {
		t.cur = cursor{id: n.id, block: []string{n.id}, anchor: n.id}
	}
}

func (t *Tree) BlockExtend(dir int) {
	head, anchor := t.nodes[t.cur.id], t.nodes[t.cur.anchor]
	sibs := head.parent.children
	j := sibIndex(head) + dir
	if j < 0 || j >= len(sibs) || head == t.zoom {
		t.msg = "Selection extends across siblings only"
		return
	}
	a, b := sibIndex(anchor), j
	if a > b {
		a, b = b, a
	}
	var ids []string
	for _, n := range sibs[a : b+1] {
		ids = append(ids, n.id)
	}
	t.cur.id, t.cur.block = sibs[j].id, ids
}

// DeleteBlock trashes the selection and lands on the nearest visible bullet.
func (t *Tree) DeleteBlock() {
	ns := t.targets()
	last, first := ns[len(ns)-1], ns[0]
	land := t.neighborPast(last, 1)
	if land == nil {
		land = t.neighbor(first.id, -1)
	}
	t.begin()
	defer t.commit()
	total := 0
	for _, n := range ns {
		if n == t.zoom {
			return
		}
		total += countAll(n) - 1
		t.trash(n)
	}
	if land != nil {
		t.cur = cursor{id: land.id, block: []string{land.id}, anchor: land.id}
	} else {
		t.cur = cursor{id: t.zoom.id, block: []string{t.zoom.id}, anchor: t.zoom.id}
	}
	t.msg = fmt.Sprintf("Moved %q and %d descendants to Trash · Ctrl+Z undoes", first.text, total)
}

// neighborPast finds the visible row after n's whole subtree.
func (t *Tree) neighborPast(n *node, _ int) *node {
	rows := t.Flatten()
	i := indexOf(rows, n.id)
	for j := i + 1; j < len(rows); j++ {
		if rows[j].depth <= rows[i].depth {
			return rows[j].n
		}
	}
	return nil
}

func countAll(n *node) int {
	c := 1
	for _, k := range n.children {
		c += countAll(k)
	}
	return c
}

func (t *Tree) ZoomIn() {
	t.seal()
	n := t.nodes[t.cur.id]
	t.zoom = n
	n.collapsed = false
	id := n.id
	if len(n.children) > 0 {
		id = n.children[0].id
	}
	t.cur = cursor{id: id, block: []string{id}, anchor: id}
}

func (t *Tree) ZoomOut(all bool) {
	t.seal()
	if t.zoom == t.root {
		return
	}
	id := t.zoom.id
	if all {
		t.zoom = t.root
	} else {
		t.zoom = t.zoom.parent
	}
	t.cur = cursor{id: id, block: []string{id}, anchor: id}
}

// Markdown renders a subtree in a minimal form of the shared dialect (§5.5).
func (t *Tree) Markdown() string {
	var b strings.Builder
	var walk func(*node, int)
	walk = func(n *node, d int) {
		b.WriteString(strings.Repeat("  ", d) + "- " + n.text + "\n")
		for _, c := range n.children {
			walk(c, d+1)
		}
	}
	for _, n := range t.targets() {
		walk(n, 0)
	}
	return b.String()
}
