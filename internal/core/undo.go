package core

import (
	"context"
	"errors"
	"slices"

	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// invertAttempts bounds re-planning when other writers keep getting in first.
const invertAttempts = 5

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
// returned Plan, whose ChangeSet is as recorded and whose CursorBefore is where to
// put the cursor. An empty stack is a no-op. CLI
// outlines never have steps.
func (o *Outline) Undo(ctx context.Context) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	if len(o.undo.undo) == 0 {
		return noOp("Nothing to undo"), nil
	}
	p, cursor, err := o.stepBack(ctx, &o.undo.undo, &o.undo.redo)
	if !p.NoOp {
		p.CursorBefore = cursor // nothing changed on a no-op, so the cursor stays
	}
	return p, err
}

// Redo seals open typing, then applies the inverse of the last undo, with undo_of
// naming that undo (D-009). Typing that sealed here was a new change, so it has
// already cleared redo (§5.6). The returned Plan has no CursorBefore: that belongs
// to the state before the change, and a step doesn't record the one after it.
func (o *Outline) Redo(ctx context.Context) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	if len(o.undo.redo) == 0 {
		return noOp("Nothing to redo"), nil
	}
	p, _, err := o.stepBack(ctx, &o.undo.redo, &o.undo.undo)
	return p, err
}

// stepBack inverts the top of from and pushes the result on to, and returns the
// step's cursor. A step whose fields have all changed since leaves from and goes
// nowhere. A failed commit leaves both stacks alone.
func (o *Outline) stepBack(ctx context.Context, from, to *[]UndoEntry) (Plan, any, error) {
	e := (*from)[len(*from)-1]
	p, err := o.invert(ctx, model.ChangeSet{ID: e.ChangeSetID, Ops: e.Ops}, o.write)
	if err != nil {
		return Plan{NoOp: true}, nil, err
	}
	*from = (*from)[:len(*from)-1]
	if !p.NoOp {
		*to = append(*to, UndoEntry{p.ChangeSet.ID, p.ChangeSet.Ops, e.CursorBefore})
	}
	return p, e.CursorBefore, nil
}

// Revert seals open typing, then applies the inverse of any recorded change set
// for the fields not changed since (§8.2). The returned Plan's ChangeSet is as
// recorded. Reverting from the UI is a new change, so it is itself a step.
// store.ErrNotFound means there is no such change set.
func (o *Outline) Revert(ctx context.Context, id model.ChangeSetID) (Plan, error) {
	if _, _, err := o.Seal(ctx); err != nil {
		return Plan{}, err
	}
	cs, err := o.db.ChangeSetByID(ctx, id)
	if err != nil {
		return Plan{}, err
	}
	return o.invert(ctx, cs, o.commit)
}

// invert plans an inverse against the current state and writes it with the given
// function. "Changed since" must hold at the commit (§8.2), so the plan comes
// from a tree as new as the store, and the commit is refused if anyone wrote
// after that; it then plans again. Typing is already sealed.
func (o *Outline) invert(ctx context.Context, cs model.ChangeSet, write func(context.Context, Plan, ...store.Expect) (model.ChangeSet, error)) (Plan, error) {
	for attempt := 1; ; attempt++ {
		if err := o.refresh(ctx); err != nil {
			return Plan{}, err
		}
		if err := o.needPages(ctx, cs.Ops); err != nil {
			return Plan{}, err
		}
		p := PlanRevert(o.Ctx(), cs)
		if p.NoOp {
			return p, nil
		}
		if o.beforeInvert != nil {
			o.beforeInvert()
		}
		recorded, err := write(ctx, p, store.Expect{History: true, Version: o.seq})
		if errors.Is(err, store.ErrConflict) && attempt < invertAttempts {
			continue
		}
		if err != nil {
			return Plan{}, err
		}
		p.ChangeSet = &recorded
		return p, nil
	}
}

// needPages loads page text if ops compare it, since the tree's is empty until
// the background load has run (§6.2).
func (o *Outline) needPages(ctx context.Context, ops []model.Op) error {
	if o.tree.PagesLoaded() || !slices.ContainsFunc(ops, func(op model.Op) bool { return op.Kind == model.OpEditPage }) {
		return nil
	}
	return o.LoadPages(ctx)
}
