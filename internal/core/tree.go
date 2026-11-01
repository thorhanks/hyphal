package core

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// Tree is the in-memory outline (§7.3, §9.3). It isn't safe for concurrent
// use; one goroutine owns it.
type Tree struct {
	root        model.NodeID
	inbox       model.NodeID
	nodes       map[model.NodeID]*model.Node    // live and trashed
	children    map[model.NodeID][]model.NodeID // live only, ordered by (position, id)
	pages       map[model.NodeID]model.Page
	pagesLoaded bool
	stars       map[model.NodeID]model.Time
	collapsed   map[model.NodeID]bool
}

// NewTree builds a tree from a dump. It accepts a damaged dump, so Check can
// report what is wrong with it.
func NewTree(d *store.Dump) *Tree {
	t := &Tree{
		root:      model.NodeID(d.Meta["root_id"]),
		inbox:     model.NodeID(d.Meta["inbox_id"]),
		nodes:     make(map[model.NodeID]*model.Node, len(d.Nodes)),
		children:  map[model.NodeID][]model.NodeID{},
		pages:     maps.Clone(d.Pages),
		stars:     maps.Clone(d.Stars),
		collapsed: maps.Clone(d.Collapsed),
	}
	for i := range d.Nodes {
		n := d.Nodes[i]
		t.nodes[n.ID] = &n
		if n.ParentID != "" && n.DeletedAt == nil {
			t.children[n.ParentID] = append(t.children[n.ParentID], n.ID)
		}
	}
	for _, kids := range t.children {
		slices.SortFunc(kids, t.compareSiblings)
	}
	return t
}

// compareSiblings orders by (position, id) (CONTRACTS §3).
func (t *Tree) compareSiblings(a, b model.NodeID) int {
	return cmp.Or(cmp.Compare(t.nodes[a].Position, t.nodes[b].Position), cmp.Compare(a, b))
}

// Root returns the root's ID.
func (t *Tree) Root() model.NodeID {
	return t.root
}

// Node returns a node, live or trashed. The caller must not change it.
func (t *Tree) Node(id model.NodeID) (*model.Node, bool) {
	n, ok := t.nodes[id]
	return n, ok
}

// AllNodes returns every node ID, live and trashed, sorted.
func (t *Tree) AllNodes() []model.NodeID {
	return slices.Sorted(maps.Keys(t.nodes))
}

// displayName names a node in a message; the root's stored text is empty (D-024).
func (t *Tree) displayName(id model.NodeID) string {
	if id == t.root {
		return "Home"
	}
	return `"` + t.nodes[id].Text + `"`
}

// Children returns a node's live children in order (CONTRACTS §3). The caller
// must not change the slice.
func (t *Tree) Children(id model.NodeID) []model.NodeID {
	return t.children[id]
}

// Parent returns a node's parent, or "" for the root or an unknown node.
func (t *Tree) Parent(id model.NodeID) model.NodeID {
	if n, ok := t.nodes[id]; ok {
		return n.ParentID
	}
	return ""
}

// Ancestors returns a node's ancestors, root first, without the node.
func (t *Tree) Ancestors(id model.NodeID) []model.NodeID {
	var out []model.NodeID
	seen := map[model.NodeID]bool{id: true}
	for p := t.Parent(id); p != "" && !seen[p]; p = t.Parent(p) {
		seen[p] = true
		out = append(out, p)
	}
	slices.Reverse(out)
	return out
}

// Page returns a node's page. Its Markdown is empty until SetPages has run,
// unless this session wrote it.
func (t *Tree) Page(id model.NodeID) (model.Page, bool) {
	p, ok := t.pages[id]
	return p, ok
}

// PagesLoaded reports whether SetPages has run (§6.2).
func (t *Tree) PagesLoaded() bool {
	return t.pagesLoaded
}

// SetPages takes in the background page load (§6.2). A page changed or deleted
// since that read keeps its newer state.
func (t *Tree) SetPages(pages map[model.NodeID]model.Page) {
	for id, p := range pages {
		if cur, ok := t.pages[id]; ok && cur.Version <= p.Version {
			t.pages[id] = p
		}
	}
	t.pagesLoaded = true
}

// StarredAt returns when a node was starred, and whether it is.
func (t *Tree) StarredAt(id model.NodeID) (model.Time, bool) {
	s, ok := t.stars[id]
	return s, ok
}

// Collapsed reports a node's stored collapse state (§7.2).
func (t *Tree) Collapsed(id model.NodeID) bool {
	return t.collapsed[id]
}

// Apply applies a change set to the tree the way store.Commit applies it to the
// rows (CONTRACTS §4, §5), so the tree equals a reload. cs must have its ID and
// time. It trusts cs, since the store is what validates; on an error the tree
// may hold part of cs, and the caller must reload.
func (t *Tree) Apply(cs model.ChangeSet) error {
	if cs.ID == "" || cs.At.IsZero() {
		return errors.New("apply: change set needs an ID and a time")
	}
	at := model.NewTime(cs.At.Time)
	for i, op := range cs.Ops {
		if err := t.applyOp(op, at, cs.ID); err != nil {
			return fmt.Errorf("apply op %d (%s on %s): %w", i, op.Kind, op.NodeID, err)
		}
	}
	return nil
}

func (t *Tree) applyOp(op model.Op, at model.Time, csID model.ChangeSetID) error {
	after, err := op.DecodeAfter()
	if err != nil {
		return err
	}
	if a, ok := after.(*model.InsertFields); ok {
		t.insert(op.NodeID, a, at)
		return nil
	}
	n, ok := t.nodes[op.NodeID]
	if !ok {
		return errors.New("node doesn't exist")
	}
	switch a := after.(type) {
	case *model.DeleteFields:
		if op.Kind == model.OpDelete {
			t.detach(n)
			n.DeletedAt, n.DeletedBy = &at, csID
		} else {
			n.DeletedAt, n.DeletedBy = nil, ""
			t.attach(n)
		}
		n.Version++
	case *model.MoveFields:
		live := n.DeletedAt == nil
		if live {
			t.detach(n)
		}
		n.ParentID, n.Position = a.ParentID, a.Position
		if live {
			t.attach(n)
		}
		n.Version++
	case *model.TextFields:
		n.Text = a.Text
		n.Version, n.UpdatedAt = n.Version+1, at
	case *model.TaskFields:
		n.IsTask = a.IsTask
		n.Version, n.UpdatedAt = n.Version+1, at
	case *model.HeadingFields:
		n.IsHeading = a.IsHeading
		n.Version, n.UpdatedAt = n.Version+1, at
	case *model.CompleteFields:
		n.CompletedAt = a.CompletedAt
		n.Version, n.UpdatedAt = n.Version+1, at
	case *model.StarFields:
		if a.StarredAt == nil {
			delete(t.stars, n.ID)
		} else {
			t.stars[n.ID] = *a.StarredAt
		}
	default: // *model.PageFields, or nil for a deleted page
		page, _ := after.(*model.PageFields)
		t.setPage(n, page, at)
	}
	return nil
}

func (t *Tree) insert(id model.NodeID, a *model.InsertFields, at model.Time) {
	n := &model.Node{
		ID: id, ParentID: a.ParentID, Position: a.Position, Text: a.Text,
		IsTask: a.IsTask, IsHeading: a.IsHeading, CompletedAt: a.CompletedAt,
		CreatedAt: a.CreatedAt, UpdatedAt: at, Version: 1,
	}
	t.nodes[id] = n
	t.attach(n)
}

func (t *Tree) setPage(n *model.Node, page *model.PageFields, at model.Time) {
	if page == nil {
		delete(t.pages, n.ID)
	} else {
		cur := t.pages[n.ID] // zero version for a new page
		t.pages[n.ID] = model.Page{NodeID: n.ID, Markdown: page.Markdown, UpdatedAt: at, Version: cur.Version + 1}
	}
	n.Version, n.UpdatedAt = n.Version+1, at
}

// attach puts a live node among its parent's children, in order.
func (t *Tree) attach(n *model.Node) {
	kids := t.children[n.ParentID]
	i, _ := slices.BinarySearchFunc(kids, n.ID, t.compareSiblings)
	t.children[n.ParentID] = slices.Insert(kids, i, n.ID)
}

// detach takes a node out of its parent's children.
func (t *Tree) detach(n *model.Node) {
	kids := t.children[n.ParentID]
	if i := slices.Index(kids, n.ID); i >= 0 {
		t.children[n.ParentID] = slices.Delete(kids, i, i+1)
	}
}

// ForgetNode drops a row from the tree without any history: what a purge does
// (§8.3). The Purge plan arrives with P1-06.
func (t *Tree) ForgetNode(id model.NodeID) {
	if n, ok := t.nodes[id]; ok {
		t.detach(n)
		delete(t.nodes, id)
	}
}

// Problem is one broken tree invariant.
type Problem struct {
	Invariant int          // the item number in CONTRACTS §8
	Node      model.NodeID // "" if it isn't about one node
	Message   string
}

func (p Problem) String() string {
	return fmt.Sprintf("invariant %d: %s: %s", p.Invariant, p.Node, p.Message)
}

// Check reports every broken invariant, CONTRACTS §8 items 1 to 9, in a stable
// order. Item 10 belongs to the store.
func (t *Tree) Check() []Problem {
	var out []Problem
	report := func(invariant int, id model.NodeID, format string, args ...any) {
		out = append(out, Problem{invariant, id, fmt.Sprintf(format, args...)})
	}
	sorted := t.AllNodes()

	// 1. Exactly one root, named by meta.root_id.
	var roots []model.NodeID
	for _, id := range sorted {
		if t.nodes[id].ParentID == "" {
			roots = append(roots, id)
		}
	}
	if len(roots) != 1 {
		report(1, "", "%d rows have no parent", len(roots))
	}
	if !slices.Contains(roots, t.root) {
		report(1, t.root, "meta.root_id isn't a row without a parent")
	}

	for _, id := range sorted {
		n := t.nodes[id]
		if n.ParentID != "" {
			parent, ok := t.nodes[n.ParentID]
			switch {
			case !ok:
				report(2, id, "parent %s doesn't exist", n.ParentID)
			case n.DeletedAt == nil && parent.DeletedAt != nil:
				report(4, id, "live under trashed parent %s", n.ParentID)
			}
		}
		// TODO(P0-16): use markup.ConvertLeadingMarker.
		if strings.ContainsAny(n.Text, "\n\r") || store.HasLeadingMarker(n.Text) {
			report(6, id, "bad text %q", n.Text)
		}
		if !ids.Valid(string(id)) {
			report(7, id, "invalid ID")
		}
		if !fracindex.Valid(n.Position) {
			report(7, id, "invalid position %q", n.Position)
		}
		if (n.DeletedAt != nil) != (n.DeletedBy != "") {
			report(8, id, "deleted_at and deleted_by disagree")
		}
	}
	for _, id := range t.cycleMembers(sorted) {
		report(3, id, "is in a cycle")
	}

	// 5. Rows that reference nodes.
	refs := func(table string, keys []model.NodeID, rootAllowed bool) {
		for _, id := range keys {
			if _, ok := t.nodes[id]; !ok {
				report(5, id, "%s row has no node", table)
			} else if id == t.root && !rootAllowed {
				report(5, id, "the root has a %s row", table)
			}
		}
	}
	refs("pages", slices.Sorted(maps.Keys(t.pages)), false)
	refs("stars", slices.Sorted(maps.Keys(t.stars)), false)
	refs("view_state", slices.Sorted(maps.Keys(t.collapsed)), true)

	if t.inbox != "" {
		if _, ok := t.nodes[t.inbox]; !ok {
			report(9, t.inbox, "meta.inbox_id has no node")
		}
	}
	return out
}

// cycleMembers returns the nodes on a parent cycle. Each walk up stops at a node
// already judged, so the whole check is linear.
func (t *Tree) cycleMembers(sorted []model.NodeID) []model.NodeID {
	judged := make(map[model.NodeID]bool, len(sorted))
	var out []model.NodeID
	for _, start := range sorted {
		onPath := map[model.NodeID]bool{}
		id := start
		for id != "" && !judged[id] && !onPath[id] && t.nodes[id] != nil {
			onPath[id] = true
			id = t.nodes[id].ParentID
		}
		if onPath[id] { // the walk came back to itself: id is on the cycle
			for at := id; ; {
				out = append(out, at)
				at = t.nodes[at].ParentID
				if at == id {
					break
				}
			}
		}
		for p := range onPath {
			judged[p] = true
		}
	}
	slices.Sort(out)
	return out
}
