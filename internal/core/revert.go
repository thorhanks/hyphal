package core

import (
	"fmt"

	"github.com/thorhanks/hyphal/internal/model"
)

// fieldGone is the Skipped field for a node that has been purged (§8.3).
const fieldGone = "gone"

// Skipped is a field a revert or undo left alone, because it changed since or
// its node can't take the old value (§8.2, CONTRACTS §5, §7.4). Field is
// "gone" when the node has been purged.
type Skipped struct {
	NodeID model.NodeID
	Kind   model.OpKind
	Field  string
}

// PlanRevert plans the inverse of a recorded change set, for the fields not
// changed since; the rest are in Plan.Skipped, never overwritten (§5.6, §8.2).
// It sets undo_of. The tree's pages must be loaded if cs has edit_page ops.
func PlanRevert(c Ctx, cs model.ChangeSet) Plan {
	sim := revertSim{t: c.Tree, deleted: map[model.NodeID]bool{}, parent: map[model.NodeID]model.NodeID{}}
	type fieldKey struct {
		node  model.NodeID
		group string
	}
	done := map[fieldKey]bool{} // what later ops of cs, undone first, did with this field
	var ops []model.Op
	var skipped []Skipped
	for i := len(cs.Ops) - 1; i >= 0; i-- {
		op := cs.Ops[i]
		key := fieldKey{op.NodeID, opGroup(op.Kind)}
		applied, seen := done[key]
		if seen && !applied {
			continue // already reported; this op's after was never the current value
		}
		fields, err := sim.blocked(op, !seen)
		if err != nil {
			return noOp("That change's history can't be read")
		}
		if len(fields) > 0 {
			done[key] = false
			for _, f := range fields {
				skipped = append(skipped, Skipped{op.NodeID, op.Kind, f})
			}
			continue
		}
		done[key] = true
		sim.apply(op)
		ops = append(ops, model.Inverse(op))
	}
	if len(ops) == 0 {
		p := noOp("")
		if len(skipped) > 0 {
			p.Hint = "Nothing to change: all of it changed since"
		}
		p.Skipped = skipped
		return p
	}
	p := c.changeSet(ops...)
	p.ChangeSet.UndoOf = cs.ID
	p.Skipped = skipped
	if len(skipped) > 0 {
		p.Message = fmt.Sprintf("Skipped %d fields that changed since", len(skipped))
	}
	return p
}

// opGroup names the field or fields an op's inverse writes.
func opGroup(kind model.OpKind) string {
	switch kind {
	case model.OpInsert, model.OpDelete, model.OpRestore:
		return "deleted" // inverting an insert only trashes the node
	}
	return string(kind)
}

// revertSim is the tree as the inverses planned so far would leave it, for the
// structure the store would otherwise refuse (CONTRACTS §4).
type revertSim struct {
	t       *Tree
	deleted map[model.NodeID]bool
	parent  map[model.NodeID]model.NodeID
}

func (s revertSim) live(id model.NodeID) bool {
	if d, ok := s.deleted[id]; ok {
		return !d
	}
	n, ok := s.t.nodes[id]
	return ok && n.DeletedAt == nil
}

func (s revertSim) parentOf(id model.NodeID) model.NodeID {
	if p, ok := s.parent[id]; ok {
		return p
	}
	return s.t.Parent(id)
}

func (s revertSim) hasLiveChildren(id model.NodeID) bool {
	isLiveChild := func(kid model.NodeID) bool { return s.parentOf(kid) == id && s.live(kid) }
	for _, kid := range s.t.Children(id) {
		if isLiveChild(kid) {
			return true
		}
	}
	for kid := range s.deleted {
		if isLiveChild(kid) {
			return true
		}
	}
	for kid := range s.parent {
		if isLiveChild(kid) {
			return true
		}
	}
	return false
}

// under reports whether id is at or below top.
func (s revertSim) under(id, top model.NodeID) bool {
	seen := map[model.NodeID]bool{}
	for ; id != "" && !seen[id]; id = s.parentOf(id) {
		if id == top {
			return true
		}
		seen[id] = true
	}
	return false
}

// blocked returns the fields that stop op being inverted. compare is false when a
// later op of the same change set wrote these fields and is already undone, so
// the current value says nothing about this op.
func (s revertSim) blocked(op model.Op, compare bool) ([]string, error) {
	n, ok := s.t.nodes[op.NodeID]
	if !ok {
		return []string{fieldGone}, nil
	}
	if compare && op.Kind != model.OpInsert {
		cur := model.Current{Node: n}
		if page, ok := s.t.Page(op.NodeID); ok {
			cur.Page = &page
		}
		if star, ok := s.t.StarredAt(op.NodeID); ok {
			cur.StarredAt = &star
		}
		changed, err := model.ChangedFields(op, cur)
		if err != nil || len(changed) > 0 {
			return changed, err
		}
	}
	switch op.Kind {
	case model.OpInsert, model.OpRestore: // the inverse is a delete
		if !s.live(op.NodeID) || s.hasLiveChildren(op.NodeID) {
			return []string{"deleted"}, nil
		}
	case model.OpDelete: // the inverse is a restore
		if s.live(op.NodeID) {
			return []string{"deleted"}, nil
		}
		if !s.live(s.parentOf(op.NodeID)) {
			return []string{"parent_id"}, nil
		}
	case model.OpMove:
		before, err := moveBefore(op)
		if err != nil {
			return nil, err
		}
		_, exists := s.t.nodes[before.ParentID]
		if !exists || (s.live(op.NodeID) && !s.live(before.ParentID)) || s.under(before.ParentID, op.NodeID) {
			return []string{"parent_id"}, nil
		}
	}
	return nil, nil
}

func moveBefore(op model.Op) (*model.MoveFields, error) {
	p, err := op.DecodeBefore()
	if err != nil {
		return nil, err
	}
	before, ok := p.(*model.MoveFields)
	if !ok {
		return nil, fmt.Errorf("move on %s has no before", op.NodeID)
	}
	return before, nil
}

// apply records what op's inverse does to the structure. blocked has already
// read op's payload.
func (s revertSim) apply(op model.Op) {
	switch op.Kind {
	case model.OpInsert, model.OpRestore:
		s.deleted[op.NodeID] = true
	case model.OpDelete:
		s.deleted[op.NodeID] = false
	case model.OpMove:
		if before, err := moveBefore(op); err == nil {
			s.parent[op.NodeID] = before.ParentID
		}
	}
}
