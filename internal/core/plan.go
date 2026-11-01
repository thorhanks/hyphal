package core

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// Plan is what a planner decides: a change set to commit, or a no-op with a hint
// (ARCHITECTURE §4.3). Planners never touch the store or change the tree.
type Plan struct {
	ChangeSet *model.ChangeSet
	NoOp      bool
	Hint      string // message text for a no-op (§4.5)
	Message   string // confirmation message (§4.5)

	CursorBefore any       // where the cursor was, for undo to restore (§5.6)
	Skipped      []Skipped // fields a revert or undo left alone (§8.2)
}

// Ctx is what a planner works from (ARCHITECTURE §4.3).
type Ctx struct {
	Tree   *Tree
	Clock  func() time.Time
	Source string
	NewID  func() model.NodeID
}

func noOp(hint string) Plan {
	return Plan{NoOp: true, Hint: hint}
}

// changeSet starts a change set with its ID and time, which Tree.Apply needs.
func (c Ctx) changeSet(ops ...model.Op) Plan {
	return Plan{ChangeSet: &model.ChangeSet{ID: ids.NewChangeSet(), At: c.now(), Source: c.Source, Ops: ops}}
}

func (c Ctx) now() model.Time {
	return model.NewTime(c.Clock())
}

// newOp builds an op from a payload that fits its kind by construction.
func newOp(id model.NodeID, kind model.OpKind, after any) model.Op {
	op := model.Op{NodeID: id, Kind: kind}
	if err := op.EncodeAfter(after); err != nil {
		panic(fmt.Sprintf("core: %s payload: %v", kind, err))
	}
	return op
}

// live returns a node that exists, isn't the root and isn't in the Trash.
func (c Ctx) live(id model.NodeID) (*model.Node, bool) {
	n, ok := c.Tree.Node(id)
	if !ok || id == c.Tree.Root() || n.DeletedAt != nil {
		return nil, false
	}
	return n, true
}

// liveParent reports whether id can hold children: live, or the root.
func (c Ctx) liveParent(id model.NodeID) bool {
	n, ok := c.Tree.Node(id)
	return ok && n.DeletedAt == nil
}

// tops keeps the nodes that have no ancestor in the list, in order, without
// repeats. It returns false if any node isn't live and movable.
func (c Ctx) tops(nodes []model.NodeID) ([]model.NodeID, bool) {
	var out []model.NodeID
	for _, id := range nodes {
		if _, ok := c.live(id); !ok {
			return nil, false
		}
		if slices.Contains(out, id) {
			continue
		}
		if !slices.ContainsFunc(c.Tree.Ancestors(id), func(a model.NodeID) bool { return slices.Contains(nodes, a) }) {
			out = append(out, id)
		}
	}
	return out, len(out) > 0
}

// Anchor says where among a parent's children something goes. The zero Anchor
// means the end; set at most one field.
type Anchor struct {
	After, Before model.NodeID
}

// positionsAt returns n keys for the anchor's place among parent's children,
// ignoring the skip nodes. Siblings can tie on position (CONTRACTS §3); a new
// key then lands after or before all of the tied ones (A-004).
func (c Ctx) positionsAt(parent model.NodeID, a Anchor, n int, skip []model.NodeID) ([]string, bool) {
	var sibs []string
	for _, id := range c.Tree.Children(parent) {
		if !slices.Contains(skip, id) {
			node, _ := c.Tree.Node(id)
			sibs = append(sibs, node.Position)
		}
	}
	low, high := "", ""
	switch {
	case a.After != "":
		pos, ok := c.anchorPosition(parent, a.After, skip)
		if !ok {
			return nil, false
		}
		low = pos
		if i := slices.IndexFunc(sibs, func(s string) bool { return s > pos }); i >= 0 {
			high = sibs[i]
		}
	case a.Before != "":
		pos, ok := c.anchorPosition(parent, a.Before, skip)
		if !ok {
			return nil, false
		}
		high = pos
		for _, s := range sibs {
			if s < pos {
				low = s
			}
		}
	case len(sibs) > 0:
		low = sibs[len(sibs)-1]
	}
	keys, err := fracindex.NBetween(low, high, n)
	return keys, err == nil
}

func (c Ctx) anchorPosition(parent, anchor model.NodeID, skip []model.NodeID) (string, bool) {
	n, ok := c.Tree.Node(anchor)
	if !ok || n.ParentID != parent || n.DeletedAt != nil || slices.Contains(skip, anchor) {
		return "", false
	}
	return n.Position, true
}

// checkText returns a hint if text can't be bullet text (§5.4, CONTRACTS §8).
// Callers convert leading markers first.
func checkText(text string) string {
	if strings.ContainsAny(text, "\n\r") {
		return "A bullet is one line"
	}
	if store.HasLeadingMarker(text) {
		return "A bullet can't start with a task or heading marker"
	}
	return ""
}

// InsertFlags are the attributes of a new bullet.
type InsertFlags struct {
	IsTask, IsHeading, Completed bool
}

// PlanInsert adds one bullet under parent at the anchor (§5.2).
func PlanInsert(c Ctx, parent model.NodeID, a Anchor, text string, f InsertFlags) Plan {
	if hint := checkText(text); hint != "" {
		return noOp(hint)
	}
	if !c.liveParent(parent) {
		return noOp("That parent isn't in the outline")
	}
	keys, ok := c.positionsAt(parent, a, 1, nil)
	if !ok {
		return noOp("That place isn't under this parent")
	}
	now := c.now()
	after := model.InsertFields{ParentID: parent, Position: keys[0], Text: text, IsTask: f.IsTask, IsHeading: f.IsHeading, CreatedAt: now}
	if f.Completed {
		after.CompletedAt = &now
	}
	return c.changeSet(newOp(c.NewID(), model.OpInsert, after))
}

// PlanEditText replaces a bullet's text (§7.3).
func PlanEditText(c Ctx, node model.NodeID, text string) Plan {
	if hint := checkText(text); hint != "" {
		return noOp(hint)
	}
	n, ok := c.live(node)
	if !ok {
		return noOp("That bullet isn't in the outline")
	}
	if n.Text == text {
		return noOp("")
	}
	return c.changeSet(newOp(node, model.OpEditText, model.TextFields{Text: text}))
}

// PlanMoveTo moves subtrees under parent at the anchor, keeping their order
// (§5.4). A node inside another listed subtree moves with it.
func PlanMoveTo(c Ctx, nodes []model.NodeID, parent model.NodeID, a Anchor) Plan {
	tops, ok := c.tops(nodes)
	if !ok {
		return noOp("That bullet isn't in the outline")
	}
	if !c.liveParent(parent) {
		return noOp("That parent isn't in the outline")
	}
	for _, id := range tops {
		if id == parent || slices.Contains(c.Tree.Ancestors(parent), id) {
			return noOp("A bullet can't move inside itself")
		}
	}
	keys, ok := c.positionsAt(parent, a, len(tops), tops)
	if !ok {
		return noOp("That place isn't under this parent")
	}
	ops := make([]model.Op, len(tops))
	for i, id := range tops {
		ops[i] = newOp(id, model.OpMove, model.MoveFields{ParentID: parent, Position: keys[i]})
	}
	return c.changeSet(ops...)
}

// PlanDelete moves subtrees to the Trash with one delete op per node, children
// first (§8.3, D-008). Descendants already in the Trash stay in their own
// deletion.
func PlanDelete(c Ctx, nodes []model.NodeID) Plan {
	tops, ok := c.tops(nodes)
	if !ok {
		return noOp("That bullet isn't in the outline")
	}
	var ops []model.Op
	var walk func(model.NodeID)
	walk = func(id model.NodeID) {
		for _, child := range c.Tree.Children(id) {
			walk(child)
		}
		ops = append(ops, model.Op{NodeID: id, Kind: model.OpDelete})
	}
	for _, id := range tops {
		walk(id)
	}
	p := c.changeSet(ops...)
	for i := range ops {
		ops[i] = newOp(ops[i].NodeID, model.OpDelete, model.DeleteFields{Deleted: true, DeletedAt: &p.ChangeSet.At, DeletedBy: &p.ChangeSet.ID})
	}
	return p
}

// PlanRestore brings back exactly one deletion: the nodes whose deleted_by
// matches (§8.3). Each top node goes back per D-017, parents before children.
func PlanRestore(c Ctx, deletion model.ChangeSetID) Plan {
	var members []model.NodeID
	inSet := map[model.NodeID]bool{}
	for _, id := range c.Tree.AllNodes() {
		if n, _ := c.Tree.Node(id); n.DeletedAt != nil && n.DeletedBy == deletion {
			members = append(members, id)
			inSet[id] = true
		}
	}
	if len(members) == 0 {
		return noOp("That deletion isn't in the Trash")
	}
	depth := func(id model.NodeID) int { return len(c.Tree.Ancestors(id)) }
	slices.SortStableFunc(members, func(a, b model.NodeID) int { return cmp.Compare(depth(a), depth(b)) })

	places := map[model.NodeID][]string{} // sibling keys per parent, as this plan fills them
	var ops []model.Op
	var movedTo []model.NodeID
	for _, id := range members {
		n, _ := c.Tree.Node(id)
		if !inSet[n.ParentID] {
			parent, position, ok := c.restorePlace(n, places)
			if !ok {
				return noOp("There is no free place to restore that to")
			}
			if parent != n.ParentID || position != n.Position {
				ops = append(ops, newOp(id, model.OpMove, model.MoveFields{ParentID: parent, Position: position}))
			}
			if parent != n.ParentID && !slices.Contains(movedTo, parent) {
				movedTo = append(movedTo, parent)
			}
		}
		ops = append(ops, newOp(id, model.OpRestore, model.DeleteFields{}))
	}
	p := c.changeSet(ops...)
	if len(movedTo) > 0 {
		p.Message = "Restored under " + c.Tree.displayName(movedTo[0])
	}
	return p
}

// restorePlace finds a top node's parent and position per D-017, and records
// the key so later nodes of the same restore don't take it.
func (c Ctx) restorePlace(n *model.Node, places map[model.NodeID][]string) (model.NodeID, string, bool) {
	parent := c.nearestLive(n.ParentID)
	keys, seen := places[parent]
	if !seen {
		for _, id := range c.Tree.Children(parent) {
			sib, _ := c.Tree.Node(id)
			keys = append(keys, sib.Position)
		}
	}
	var position string
	var err error
	i, taken := slices.BinarySearch(keys, n.Position)
	switch {
	case parent != n.ParentID:
		last := ""
		if len(keys) > 0 {
			last = keys[len(keys)-1]
		}
		position, err = fracindex.Between(last, "")
	case !taken:
		position = n.Position
	default:
		for i < len(keys) && keys[i] == n.Position {
			i++
		}
		next := ""
		if i < len(keys) {
			next = keys[i]
		}
		position, err = fracindex.Between(n.Position, next)
	}
	if err != nil {
		return "", "", false
	}
	i, _ = slices.BinarySearch(keys, position)
	places[parent] = slices.Insert(keys, i, position)
	return parent, position, true
}

// nearestLive walks up from id through existing rows to the first live node. A
// chain broken by a purged row ends at the root (D-017).
func (c Ctx) nearestLive(id model.NodeID) model.NodeID {
	for {
		n, ok := c.Tree.Node(id)
		if !ok {
			return c.Tree.Root()
		}
		if n.DeletedAt == nil {
			return id
		}
		id = n.ParentID
	}
}

// eachLive builds one op per node that needs it; a list with a bad node or
// nothing to change is a no-op.
func (c Ctx) eachLive(nodes []model.NodeID, op func(*model.Node) (model.Op, bool)) Plan {
	var ops []model.Op
	for _, id := range nodes {
		n, ok := c.live(id)
		if !ok {
			return noOp("That bullet isn't in the outline")
		}
		if o, change := op(n); change {
			ops = append(ops, o)
		}
	}
	if len(ops) == 0 {
		return noOp("")
	}
	return c.changeSet(ops...)
}

// PlanSetTask makes bullets tasks or plain bullets (§5.4).
func PlanSetTask(c Ctx, nodes []model.NodeID, on bool) Plan {
	return c.eachLive(nodes, func(n *model.Node) (model.Op, bool) {
		return newOp(n.ID, model.OpSetTask, model.TaskFields{IsTask: on}), n.IsTask != on
	})
}

// PlanSetHeading turns the heading style on or off (§5.4).
func PlanSetHeading(c Ctx, nodes []model.NodeID, on bool) Plan {
	return c.eachLive(nodes, func(n *model.Node) (model.Op, bool) {
		return newOp(n.ID, model.OpSetHeading, model.HeadingFields{IsHeading: on}), n.IsHeading != on
	})
}

// PlanSetComplete completes or reopens bullets (§5.4, §7.2).
func PlanSetComplete(c Ctx, nodes []model.NodeID, on bool) Plan {
	now := c.now()
	return c.eachLive(nodes, func(n *model.Node) (model.Op, bool) {
		var when *model.Time
		if on {
			when = &now
		}
		return newOp(n.ID, model.OpSetComplete, model.CompleteFields{CompletedAt: when}), (n.CompletedAt != nil) != on
	})
}

// PlanSetStar stars or unstars bullets (§7.2).
func PlanSetStar(c Ctx, nodes []model.NodeID, on bool) Plan {
	now := c.now()
	return c.eachLive(nodes, func(n *model.Node) (model.Op, bool) {
		_, starred := c.Tree.StarredAt(n.ID)
		var when *model.Time
		if on {
			when = &now
		}
		return newOp(n.ID, model.OpSetStar, model.StarFields{StarredAt: when}), starred != on
	})
}

// PlanSetPage sets a bullet's page. Empty text deletes the page, and on a bullet
// without one does nothing (§5.7).
func PlanSetPage(c Ctx, node model.NodeID, markdown string) Plan {
	if _, ok := c.live(node); !ok {
		return noOp("That bullet isn't in the outline")
	}
	page, exists := c.Tree.Page(node)
	if markdown == "" {
		if !exists {
			return noOp("")
		}
		return c.changeSet(newOp(node, model.OpEditPage, nil))
	}
	if exists && page.Markdown == markdown && (c.Tree.PagesLoaded() || page.Markdown != "") {
		return noOp("")
	}
	return c.changeSet(newOp(node, model.OpEditPage, model.PageFields{Markdown: markdown}))
}
