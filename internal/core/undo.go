package core

import (
	"context"
	"slices"

	"github.com/thorhanks/hyphal/internal/model"
)

// UndoEntry is one step on the undo or redo stack (§5.6).
type UndoEntry struct {
	ChangeSetID  model.ChangeSetID
	Ops          []model.Op // as recorded
	CursorBefore any
}

// UndoStack holds a session's undo and redo steps, in memory only (§5.6).
type UndoStack struct {
	undo, redo []UndoEntry
}

// push records a new change from the UI, which clears redo (§5.6).
func (u *UndoStack) push(cs model.ChangeSet, cursor any) {
	u.undo = append(u.undo, UndoEntry{cs.ID, cs.Ops, cursor})
	u.redo = nil
}

// Undo seals open typing, so that it is the step undone, then applies the inverse
// of the last step (§5.6). Fields changed since are skipped and reported in the
// returned Plan, whose ChangeSet is as recorded. An empty stack is a no-op. CLI
// outlines never have steps.
func (o *Outline) Undo(ctx context.Context) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	if len(o.undo.undo) == 0 {
		return noOp("Nothing to undo"), nil
	}
	return o.stepBack(ctx, &o.undo.undo, &o.undo.redo)
}

// Redo seals open typing, then applies the inverse of the last undo, with undo_of
// naming that undo (D-009). Typing that sealed here was a new change, so it has
// already cleared redo (§5.6).
func (o *Outline) Redo(ctx context.Context) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	if len(o.undo.redo) == 0 {
		return noOp("Nothing to redo"), nil
	}
	return o.stepBack(ctx, &o.undo.redo, &o.undo.undo)
}

// stepBack inverts the top of from and pushes the result on to. A step whose
// fields have all changed since leaves from and goes nowhere. A failed commit
// leaves both stacks alone.
func (o *Outline) stepBack(ctx context.Context, from, to *[]UndoEntry) (Plan, error) {
	e := (*from)[len(*from)-1]
	if err := o.needPages(ctx, e.Ops); err != nil {
		return Plan{}, err
	}
	p := PlanRevert(o.Ctx(), model.ChangeSet{ID: e.ChangeSetID, Ops: e.Ops})
	p.CursorBefore = e.CursorBefore
	if p.NoOp {
		*from = (*from)[:len(*from)-1]
		return p, nil
	}
	cs, err := o.write(ctx, p)
	if err != nil {
		return Plan{}, err
	}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, UndoEntry{cs.ID, cs.Ops, e.CursorBefore})
	p.ChangeSet = &cs
	return p, nil
}

// Revert seals open typing, then applies the inverse of any recorded change set
// for the fields not changed since (§8.2). Reverting from the UI is a new change,
// so it is itself a step. store.ErrNotFound means there is no such change set.
func (o *Outline) Revert(ctx context.Context, id model.ChangeSetID) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	cs, err := o.db.ChangeSetByID(ctx, id)
	if err != nil {
		return Plan{}, err
	}
	if err := o.needPages(ctx, cs.Ops); err != nil {
		return Plan{}, err
	}
	p := PlanRevert(o.Ctx(), cs)
	if p.NoOp {
		return p, nil
	}
	_, err = o.commit(ctx, p)
	return p, err
}

// needPages loads page text if ops compare it, since the tree's is empty until
// the background load has run (§6.2).
func (o *Outline) needPages(ctx context.Context, ops []model.Op) error {
	if o.tree.PagesLoaded() || !slices.ContainsFunc(ops, func(op model.Op) bool { return op.Kind == model.OpEditPage }) {
		return nil
	}
	return o.LoadPages(ctx)
}
