package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// OutlineOptions configures an Outline.
type OutlineOptions struct {
	Source string
	Clock  func() time.Time    // default time.Now
	NewID  func() model.NodeID // default ids.NewNode
	Sync   bool                // commit in Do; the server's committer arrives with P1-23
}

// Outline couples a Tree with its store (ARCHITECTURE §4.3).
type Outline struct {
	db     *store.DB
	tree   *Tree
	opts   OutlineOptions
	typing *Typing // nil when no typing is open (§7.3)
	undo   UndoStack
	seq    int64 // the history position the tree reflects (store.HistorySeq)

	beforeInvert func() // test only: runs between planning an inverse and writing it
}

// OpenOutline loads the outline from db.
func OpenOutline(ctx context.Context, db *store.DB, o OutlineOptions) (*Outline, error) {
	if !o.Sync {
		return nil, errors.New("open outline: only synchronous commits exist so far")
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.NewID == nil {
		o.NewID = ids.NewNode
	}
	d, err := db.LoadAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("open outline: %w", err)
	}
	return &Outline{db: db, tree: NewTree(d), opts: o, seq: d.Seq}, nil
}

// Tree returns the in-memory tree.
func (o *Outline) Tree() *Tree {
	return o.tree
}

// Ctx returns the context for planning against this outline.
func (o *Outline) Ctx() Ctx {
	return Ctx{Tree: o.tree, Clock: o.opts.Clock, Source: o.opts.Source, NewID: o.opts.NewID}
}

// LoadPages loads page text into the tree (§6.2).
func (o *Outline) LoadPages(ctx context.Context) error {
	pages, err := o.db.LoadPages(ctx)
	if err != nil {
		return err
	}
	o.tree.SetPages(pages)
	return nil
}

// Do commits a plan and applies it to the tree, and returns the change set's
// ID, or "" for a no-op. Open typing seals first (§7.3). The commit comes first,
// so a refused plan leaves the tree as it was.
func (o *Outline) Do(ctx context.Context, p Plan) (model.ChangeSetID, error) {
	if p.NoOp || p.ChangeSet == nil {
		return "", nil
	}
	if _, _, err := o.Seal(ctx); err != nil {
		return "", err
	}
	cs, err := o.commit(ctx, p)
	return cs.ID, err
}

// commit writes a plan and applies it to the tree. A change from the UI becomes
// an undo step and clears redo (§5.6).
func (o *Outline) commit(ctx context.Context, p Plan, expect ...store.Expect) (model.ChangeSet, error) {
	cs, err := o.write(ctx, p, expect...)
	if err != nil {
		return model.ChangeSet{}, err
	}
	if o.opts.Source == "ui" {
		o.undo.push(cs, p.CursorBefore)
	}
	return cs, nil
}

// write commits a plan and applies it to the tree, and returns the change set as
// recorded. Once written, any open typing is over: only seal gets here with one,
// since every other caller seals first.
func (o *Outline) write(ctx context.Context, p Plan, expect ...store.Expect) (model.ChangeSet, error) {
	res, err := o.db.Commit(ctx, *p.ChangeSet, expect, nil)
	if err != nil {
		return model.ChangeSet{}, err
	}
	o.typing = nil
	if err := o.tree.Apply(res.ChangeSet); err != nil {
		return model.ChangeSet{}, fmt.Errorf("commit landed but the tree can't follow: %w", err)
	}
	// A gap means another connection wrote first, so seq stays behind and refresh
	// sees it.
	if res.Seq == o.seq+1 {
		o.seq = res.Seq
	}
	return res.ChangeSet, nil
}

// refresh reloads the tree if anyone else has written since it was loaded (§7.3).
// Open typing must be sealed first.
func (o *Outline) refresh(ctx context.Context) error {
	seq, err := o.db.HistorySeq(ctx)
	if err != nil || seq == o.seq {
		return err
	}
	d, err := o.db.LoadAll(ctx)
	if err != nil {
		return err
	}
	pages := o.tree.PagesLoaded()
	o.tree, o.seq = NewTree(d), d.Seq
	if pages {
		return o.LoadPages(ctx)
	}
	return nil
}
