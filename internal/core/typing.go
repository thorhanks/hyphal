package core

import (
	"context"
	"fmt"
	"time"

	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// How long typing stays open (§7.3).
const (
	sealIdle = 2 * time.Second
	sealOpen = 5 * time.Second
)

// Typing is one open typing operation: edits to one node's text held in memory
// until they seal (§7.3).
type Typing struct {
	Node         model.NodeID
	Before       string // the text when typing opened
	Text         string // the text now, which the tree shows
	Opened, Last time.Time
}

func (t *Typing) due(now time.Time) bool {
	return now.Sub(t.Last) >= sealIdle || now.Sub(t.Opened) >= sealOpen
}

// TypeInto sets a node's text in the tree at once; the edit_text op comes when
// the typing seals (§7.3). Open typing that belongs to another node or is due
// seals first. Text a bullet can't hold, or a node that isn't live, returns
// ErrInvalid and changes nothing. If that seal fails, its typing stays open and
// this text isn't taken.
func (o *Outline) TypeInto(ctx context.Context, node model.NodeID, text string, now time.Time) error {
	n, ok := o.Ctx().live(node)
	if !ok {
		return fmt.Errorf("%w: node %q isn't in the outline", store.ErrInvalid, node)
	}
	if hint := checkText(text); hint != "" {
		return fmt.Errorf("%w: %s", store.ErrInvalid, hint)
	}
	if t := o.typing; t != nil && (t.Node != node || t.due(now)) {
		if _, _, err := o.seal(ctx, now); err != nil {
			return err
		}
	}
	if o.typing == nil {
		if n.Text == text {
			return nil
		}
		o.typing = &Typing{Node: node, Before: n.Text, Opened: now}
	}
	o.typing.Text, o.typing.Last = text, now
	n.Text = text
	return nil
}

// SealIfDue seals open typing after 2 s idle or 5 s open, and reports whether
// it closed any (§7.3).
func (o *Outline) SealIfDue(ctx context.Context, now time.Time) (bool, error) {
	if o.typing == nil || !o.typing.due(now) {
		return false, nil
	}
	_, sealed, err := o.seal(ctx, now)
	return sealed, err
}

// Seal seals open typing now, and reports whether it closed any. The ID is ""
// when there was none, or when the text ended where it began (§7.3).
func (o *Outline) Seal(ctx context.Context) (model.ChangeSetID, bool, error) {
	return o.seal(ctx, o.opts.Clock())
}

func (o *Outline) seal(ctx context.Context, now time.Time) (model.ChangeSetID, bool, error) {
	t := o.typing
	if t == nil {
		return "", false, nil
	}
	if t.Text == t.Before {
		o.typing = nil
		return "", true, nil
	}
	c := o.Ctx()
	c.Clock = func() time.Time { return now }
	cs, err := o.commit(ctx, c.changeSet(newOp(t.Node, model.OpEditText, model.TextFields{Text: t.Text})))
	if err != nil {
		return "", false, err
	}
	return cs.ID, true, nil
}

// PendingTyping returns a copy of the open typing, or nil (§4.5).
func (o *Outline) PendingTyping() *Typing {
	if o.typing == nil {
		return nil
	}
	t := *o.typing
	return &t
}
