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
	return &Outline{db: db, tree: NewTree(d), opts: o}, nil
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
	return o.commit(ctx, p)
}

// commit writes a plan and applies it to the tree. Once written, any open typing
// is over: only seal gets here with one, since Do seals first.
func (o *Outline) commit(ctx context.Context, p Plan) (model.ChangeSetID, error) {
	res, err := o.db.Commit(ctx, *p.ChangeSet, nil, nil)
	if err != nil {
		return "", err
	}
	o.typing = nil
	if err := o.tree.Apply(res.ChangeSet); err != nil {
		return "", fmt.Errorf("commit landed but the tree can't follow: %w", err)
	}
	return res.ChangeSet.ID, nil
}
