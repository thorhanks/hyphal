package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

func uiOutline(t *testing.T, db *store.DB) *core.Outline {
	t.Helper()
	o, err := core.OpenOutline(context.Background(), db, core.OutlineOptions{Source: "ui", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func mustDo(t *testing.T, o *core.Outline, p core.Plan) model.ChangeSetID {
	t.Helper()
	if p.NoOp {
		t.Fatalf("no-op: %q", p.Hint)
	}
	id, err := o.Do(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertNode(t *testing.T, o *core.Outline, parent model.NodeID, text string) (model.NodeID, model.ChangeSetID) {
	t.Helper()
	p := core.PlanInsert(o.Ctx(), parent, core.Anchor{}, text, core.InsertFlags{})
	id := mustDo(t, o, p)
	return p.ChangeSet.Ops[0].NodeID, id
}

// fields lists every field of every node, by node and op group, without history
// or versions (P0-13 AC1). A group holds the fields one kind of op writes.
func fields(o *core.Outline) map[model.NodeID]map[string]string {
	tree := o.Tree()
	out := map[model.NodeID]map[string]string{}
	for _, id := range tree.AllNodes() {
		n, _ := tree.Node(id)
		f := map[string]string{
			"edit_text":    n.Text,
			"move":         string(n.ParentID) + " " + n.Position,
			"set_task":     fmt.Sprint(n.IsTask),
			"set_heading":  fmt.Sprint(n.IsHeading),
			"set_complete": "",
			"deleted":      string(n.DeletedBy),
			"set_star":     "",
			"edit_page":    "",
		}
		if n.CompletedAt != nil {
			f["set_complete"] = n.CompletedAt.String()
		}
		if n.DeletedAt != nil {
			f["deleted"] += " " + n.DeletedAt.String()
		}
		if s, ok := tree.StarredAt(id); ok {
			f["set_star"] = s.String()
		}
		if p, ok := tree.Page(id); ok {
			f["edit_page"] = "page: " + p.Markdown
		}
		out[id] = f
	}
	return out
}

func TestUndoAll_RestoresOriginal(t *testing.T) {
	ctx := context.Background()
	rapid.Check(t, func(rt *rapid.T) {
		seed := rapid.Uint64().Draw(rt, "seed")
		count := rapid.IntRange(1, 50).Draw(rt, "actions")
		db := testgen.TempDB(t)

		// Some earlier content, which isn't on this session's stack.
		setup, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "cli", Sync: true})
		if err != nil {
			rt.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 3))
		testgen.Actions(ctx, t, r, setup, 30)

		o := uiOutline(t, db)
		if err := o.LoadPages(ctx); err != nil {
			rt.Fatal(err)
		}
		want := fields(o)
		for range testgen.Actions(ctx, t, r, o, count) {
			p, err := o.Undo(ctx)
			if err != nil {
				rt.Fatal(err)
			}
			if p.NoOp || len(p.Skipped) != 0 {
				rt.Fatalf("undo: hint %q, skipped %+v", p.Hint, p.Skipped)
			}
		}
		if p, err := o.Undo(ctx); err != nil || !p.NoOp {
			rt.Fatalf("stack not empty: %+v, %v", p, err)
		}
		got := fields(o)
		for id, g := range got {
			w, existed := want[id]
			if !existed {
				// An undone insert stays in the Trash (D-013).
				if g["deleted"] == "" {
					rt.Fatalf("new node %s is live: %v", id, g)
				}
				continue
			}
			if !maps.Equal(g, w) {
				rt.Fatalf("node %s:\n got %v\nwant %v", id, g, w)
			}
		}
		if problems := o.Tree().Check(); len(problems) != 0 {
			rt.Fatal(problems)
		}
	})
}

func TestRevert_Isolation(t *testing.T) {
	ctx := context.Background()
	rapid.Check(t, func(rt *rapid.T) {
		seed := rapid.Uint64().Draw(rt, "seed")
		count := rapid.IntRange(1, 40).Draw(rt, "actions")
		db := testgen.TempDB(t)
		o := uiOutline(t, db)
		if err := o.LoadPages(ctx); err != nil {
			rt.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 4))
		done := testgen.Actions(ctx, t, r, o, count)
		if len(done) == 0 {
			return
		}
		target := done[rapid.IntRange(0, len(done)-1).Draw(rt, "target")]
		cs, err := db.ChangeSetByID(ctx, target)
		if err != nil {
			rt.Fatal(err)
		}
		mine := map[string]bool{}
		for _, op := range cs.Ops {
			mine[string(op.NodeID)+"/"+group(op.Kind)] = true
		}

		before := fields(o)
		if _, err := o.Revert(ctx, target); err != nil {
			rt.Fatal(err)
		}
		for id, after := range fields(o) {
			for name, value := range after {
				if !mine[string(id)+"/"+name] && value != before[id][name] {
					rt.Fatalf("%s %s changed: %q to %q", id, name, before[id][name], value)
				}
			}
		}
		if problems := o.Tree().Check(); len(problems) != 0 {
			rt.Fatal(problems)
		}
		fresh := uiOutline(t, db)
		if err := fresh.LoadPages(ctx); err != nil {
			rt.Fatal(err)
		}
		if !maps.EqualFunc(fields(fresh), fields(o), maps.Equal) {
			rt.Fatal("the tree differs from a reload")
		}
	})
}

func group(kind model.OpKind) string {
	switch kind {
	case model.OpInsert, model.OpDelete, model.OpRestore:
		return "deleted"
	}
	return string(kind)
}

func TestRevert_SkipsChangedFields(t *testing.T) {
	ctx := context.Background()
	o := uiOutline(t, testgen.TempDB(t))
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	// One change set touching two fields of a node.
	base := core.PlanSetTask(o.Ctx(), []model.NodeID{a}, true)
	base.ChangeSet.Ops = append(base.ChangeSet.Ops, core.PlanEditText(o.Ctx(), a, "edited").ChangeSet.Ops...)
	id := mustDo(t, o, base)
	mustDo(t, o, core.PlanEditText(o.Ctx(), a, "newer"))

	p, err := o.Revert(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Skipped{{NodeID: a, Kind: model.OpEditText, Field: "text"}}
	if !slices.Equal(p.Skipped, want) || p.Message == "" {
		t.Errorf("skipped %+v, message %q", p.Skipped, p.Message)
	}
	n, _ := o.Tree().Node(a)
	if n.Text != "newer" {
		t.Errorf("text overwritten: %q", n.Text)
	}
	if n.IsTask {
		t.Error("the unchanged field wasn't reverted")
	}

	// With nothing left to revert, nothing is written.
	mustDo(t, o, core.PlanEditText(o.Ctx(), a, "newest"))
	edit := mustDo(t, o, core.PlanEditText(o.Ctx(), a, "last"))
	mustDo(t, o, core.PlanEditText(o.Ctx(), a, "after that"))
	p, err = o.Revert(ctx, edit)
	if err != nil {
		t.Fatal(err)
	}
	if !p.NoOp || p.Hint == "" || len(p.Skipped) != 1 {
		t.Errorf("plan: %+v", p)
	}
}

// Structure the store would refuse is skipped too, not left to fail the commit.
func TestRevert_SkipsWhatTheTreeRefuses(t *testing.T) {
	ctx := context.Background()
	root := func(o *core.Outline) model.NodeID { return o.Tree().Root() }

	t.Run("an insert that has gained a child", func(t *testing.T) {
		o := uiOutline(t, testgen.TempDB(t))
		a, id := insertNode(t, o, root(o), "a")
		insertNode(t, o, a, "child")
		p, err := o.Revert(ctx, id)
		if err != nil || !p.NoOp || len(p.Skipped) != 1 || p.Skipped[0].Field != "deleted" {
			t.Fatalf("%+v, %v", p, err)
		}
	})

	t.Run("a move back into a trashed parent", func(t *testing.T) {
		o := uiOutline(t, testgen.TempDB(t))
		a, _ := insertNode(t, o, root(o), "a")
		b, _ := insertNode(t, o, a, "b")
		move := mustDo(t, o, core.PlanMoveTo(o.Ctx(), []model.NodeID{b}, root(o), core.Anchor{}))
		mustDo(t, o, core.PlanDelete(o.Ctx(), []model.NodeID{a}))
		p, err := o.Revert(ctx, move)
		if err != nil || !p.NoOp || len(p.Skipped) != 1 || p.Skipped[0].Field != "parent_id" {
			t.Fatalf("%+v, %v", p, err)
		}
	})

	t.Run("a move that would make a cycle", func(t *testing.T) {
		o := uiOutline(t, testgen.TempDB(t))
		a, _ := insertNode(t, o, root(o), "a")
		b, _ := insertNode(t, o, a, "b")
		first := mustDo(t, o, core.PlanMoveTo(o.Ctx(), []model.NodeID{b}, root(o), core.Anchor{}))
		mustDo(t, o, core.PlanMoveTo(o.Ctx(), []model.NodeID{a}, b, core.Anchor{}))
		p, err := o.Revert(ctx, first)
		if err != nil || !p.NoOp || len(p.Skipped) == 0 {
			t.Fatalf("%+v, %v", p, err)
		}
	})

	t.Run("a restore under a parent trashed since", func(t *testing.T) {
		o := uiOutline(t, testgen.TempDB(t))
		a, _ := insertNode(t, o, root(o), "a")
		b, _ := insertNode(t, o, a, "b")
		del := mustDo(t, o, core.PlanDelete(o.Ctx(), []model.NodeID{b}))
		mustDo(t, o, core.PlanDelete(o.Ctx(), []model.NodeID{a}))
		p, err := o.Revert(ctx, del)
		if err != nil || !p.NoOp || len(p.Skipped) != 1 || p.Skipped[0].Field != "parent_id" {
			t.Fatalf("%+v, %v", p, err)
		}
	})
}

// Undoing a restore that had to change parents puts the node back in its old
// deletion, under its old trashed parent (A-007).
func TestUndo_RestoreGoesBackIntoItsDeletion(t *testing.T) {
	ctx := context.Background()
	o := uiOutline(t, testgen.TempDB(t))
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	b, _ := insertNode(t, o, a, "b")
	del := mustDo(t, o, core.PlanDelete(o.Ctx(), []model.NodeID{b}))
	mustDo(t, o, core.PlanDelete(o.Ctx(), []model.NodeID{a}))
	mustDo(t, o, core.PlanRestore(o.Ctx(), del))
	if o.Tree().Parent(b) != o.Tree().Root() {
		t.Fatal("restore didn't go under Home")
	}
	if p, err := o.Undo(ctx); err != nil || len(p.Skipped) != 0 {
		t.Fatalf("%+v, %v", p, err)
	}
	n, _ := o.Tree().Node(b)
	if n.ParentID != a || n.DeletedBy != del {
		t.Errorf("node: %+v, want deletion %s", n, del)
	}
}

func TestUndoRedo_Linkage(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "old")
	edit := mustDo(t, o, core.PlanEditText(o.Ctx(), a, "new"))

	undo, err := o.Undo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := db.ChangeSetByID(ctx, undo.ChangeSet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.UndoOf != edit || recorded.Source != "ui" {
		t.Errorf("undo: %+v, want undo_of %s", recorded, edit)
	}
	if got := textOf(t, o, a); got != "old" {
		t.Errorf("after undo: %q", got)
	}

	redo, err := o.Redo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err = db.ChangeSetByID(ctx, redo.ChangeSet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.UndoOf != undo.ChangeSet.ID {
		t.Errorf("redo: undo_of %s, want %s", recorded.UndoOf, undo.ChangeSet.ID)
	}
	if got := textOf(t, o, a); got != "new" {
		t.Errorf("after redo: %q", got)
	}
	// The redo is itself a step.
	if _, err := o.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, o, a); got != "old" {
		t.Errorf("after second undo: %q", got)
	}
}

func TestUndo_NewChangeClearsRedo(t *testing.T) {
	ctx := context.Background()
	o := uiOutline(t, testgen.TempDB(t))
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	if _, err := o.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := insertNode(t, o, o.Tree().Root(), "b")
	p, err := o.Redo(ctx)
	if err != nil || !p.NoOp || p.Hint == "" {
		t.Fatalf("redo: %+v, %v", p, err)
	}
	if n, _ := o.Tree().Node(a); n.DeletedAt == nil {
		t.Error("a came back")
	}
	if n, _ := o.Tree().Node(b); n.DeletedAt != nil {
		t.Error("b is gone")
	}
}

func TestUndo_SealsTypingFirst(t *testing.T) {
	ctx := context.Background()
	o := uiOutline(t, testgen.TempDB(t))
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	if err := o.TypeInto(ctx, a, "typed", after(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Undo(ctx); err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, o, a); got != "a" {
		t.Errorf("text %q", got)
	}
	if o.PendingTyping() != nil {
		t.Error("typing still open")
	}
}

func TestUndo_CLIHasNoStack(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	insertNode(t, o, o.Tree().Root(), "a")
	p, err := o.Undo(ctx)
	if err != nil || !p.NoOp {
		t.Errorf("%+v, %v", p, err)
	}
}

// A page's text is compared with the store's, even before the background load.
func TestRevert_PageBeforeBackgroundLoad(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	mustDo(t, o, core.PlanSetPage(o.Ctx(), a, "one"))
	id := mustDo(t, o, core.PlanSetPage(o.Ctx(), a, "two"))

	fresh := uiOutline(t, db)
	p, err := fresh.Revert(ctx, id)
	if err != nil || p.NoOp || len(p.Skipped) != 0 {
		t.Fatalf("%+v, %v", p, err)
	}
	if page, ok, err := db.Page(ctx, a); err != nil || !ok || page.Markdown != "one" {
		t.Errorf("page %+v, %v, %v", page, ok, err)
	}
}

func TestRevert_NotFound(t *testing.T) {
	o := uiOutline(t, testgen.TempDB(t))
	if _, err := o.Revert(context.Background(), "000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestRevert_PurgedNode(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	b, _ := insertNode(t, o, o.Tree().Root(), "b")
	id := mustDo(t, o, core.PlanSetTask(o.Ctx(), []model.NodeID{a, b}, true))
	cs, err := db.ChangeSetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// The purge itself arrives with P1-06.
	o.Tree().ForgetNode(a)

	p := core.PlanRevert(o.Ctx(), cs)
	want := []core.Skipped{{NodeID: a, Kind: model.OpSetTask, Field: "gone"}}
	if !slices.Equal(p.Skipped, want) {
		t.Errorf("skipped %+v", p.Skipped)
	}
	if p.ChangeSet == nil || len(p.ChangeSet.Ops) != 1 || p.ChangeSet.Ops[0].NodeID != b {
		t.Fatalf("plan: %+v", p)
	}
	mustDo(t, o, p)
	if n, _ := o.Tree().Node(b); n.IsTask {
		t.Error("b wasn't reverted")
	}
}

func cliOn(t *testing.T, db *store.DB) *core.Outline {
	t.Helper()
	o, err := core.OpenOutline(context.Background(), db, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// A change from another connection is skipped even though this tree hasn't seen
// it (§5.6, §8.2).
func TestUndo_SkipsChangeFromOtherConnection(t *testing.T) {
	ctx := context.Background()
	t.Run("text", func(t *testing.T) {
		db := testgen.TempDB(t)
		o := uiOutline(t, db)
		a, _ := insertNode(t, o, o.Tree().Root(), "old")
		mustDo(t, o, core.PlanEditText(o.Ctx(), a, "mine"))
		cli := cliOn(t, db)
		mustDo(t, cli, core.PlanEditText(cli.Ctx(), a, "theirs"))

		p, err := o.Undo(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !p.NoOp || len(p.Skipped) != 1 || p.Skipped[0].Field != "text" {
			t.Fatalf("plan: %+v", p)
		}
		if got := textOf(t, o, a); got != "theirs" {
			t.Errorf("tree text %q", got)
		}
		fresh := cliOn(t, db)
		if got := textOf(t, fresh, a); got != "theirs" {
			t.Errorf("stored text %q", got)
		}
	})
	t.Run("star, which bumps no version", func(t *testing.T) {
		db := testgen.TempDB(t)
		o := uiOutline(t, db)
		a, _ := insertNode(t, o, o.Tree().Root(), "a")
		id := mustDo(t, o, core.PlanSetStar(o.Ctx(), []model.NodeID{a}, true))
		cli := cliOn(t, db)
		mustDo(t, cli, core.PlanSetStar(cli.Ctx(), []model.NodeID{a}, false))
		p, err := o.Revert(ctx, id)
		if err != nil || !p.NoOp || len(p.Skipped) != 1 {
			t.Fatalf("%+v, %v", p, err)
		}
	})
	t.Run("a write between this outline's own writes", func(t *testing.T) {
		db := testgen.TempDB(t)
		o := uiOutline(t, db)
		a, _ := insertNode(t, o, o.Tree().Root(), "a")
		b, _ := insertNode(t, o, o.Tree().Root(), "b")
		cli := cliOn(t, db)
		mustDo(t, cli, core.PlanEditText(cli.Ctx(), a, "theirs"))
		mustDo(t, o, core.PlanEditText(o.Ctx(), b, "mine")) // leaves the tree behind on a
		edit, err := o.Undo(ctx)
		if err != nil || edit.NoOp {
			t.Fatalf("%+v, %v", edit, err)
		}
		if got := textOf(t, o, a); got != "theirs" {
			t.Errorf("a is %q; the undo should have reloaded", got)
		}
	})
}

func TestUndo_CursorBefore(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "a")
	p := core.PlanEditText(o.Ctx(), a, "b")
	p.CursorBefore = "here"
	mustDo(t, o, p)

	undo, err := o.Undo(ctx)
	if err != nil || undo.CursorBefore != "here" {
		t.Errorf("undo cursor %v, %v", undo.CursorBefore, err)
	}
	redo, err := o.Redo(ctx)
	if err != nil || redo.CursorBefore != nil {
		t.Errorf("redo cursor %v, %v", redo.CursorBefore, err)
	}
	cli := cliOn(t, db)
	mustDo(t, cli, core.PlanEditText(cli.Ctx(), a, "theirs"))
	skipped, err := o.Undo(ctx)
	if err != nil || !skipped.NoOp || skipped.CursorBefore != nil {
		t.Errorf("skipped undo: %+v, %v", skipped, err)
	}
}

func TestRevert_ReturnsRecordedChangeSet(t *testing.T) {
	ctx := context.Background()
	o := uiOutline(t, testgen.TempDB(t))
	_, id := insertNode(t, o, o.Tree().Root(), "a")
	p, err := o.Revert(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	after, err := p.ChangeSet.Ops[0].DecodeAfter()
	if err != nil {
		t.Fatal(err)
	}
	if f := after.(*model.DeleteFields); f.DeletedBy == nil || *f.DeletedBy != p.ChangeSet.ID {
		t.Errorf("after: %+v", f)
	}
}

// A write landing after planning makes the commit fail its history check, and
// the undo plans again from the newer state (§8.2).
func TestUndo_ConflictPlansAgain(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "old")
	mustDo(t, o, core.PlanEditText(o.Ctx(), a, "mine"))
	cli := cliOn(t, db)
	once := false
	o.SetBeforeInvert(func() {
		if !once {
			once = true
			mustDo(t, cli, core.PlanEditText(cli.Ctx(), a, "theirs"))
		}
	})
	p, err := o.Undo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.NoOp || len(p.Skipped) != 1 || p.Skipped[0].Field != "text" {
		t.Fatalf("plan: %+v", p)
	}
	if got := textOf(t, cliOn(t, db), a); got != "theirs" {
		t.Errorf("stored text %q", got)
	}
}

func TestUndo_ConflictsRunOut(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	a, _ := insertNode(t, o, o.Tree().Root(), "old")
	b, _ := insertNode(t, o, o.Tree().Root(), "other")
	mustDo(t, o, core.PlanEditText(o.Ctx(), a, "mine"))
	cli := cliOn(t, db)
	n := 0
	o.SetBeforeInvert(func() {
		n++
		mustDo(t, cli, core.PlanEditText(cli.Ctx(), b, fmt.Sprint("theirs ", n)))
	})
	if _, err := o.Undo(ctx); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("got %v", err)
	}
	// The step is still there.
	o.SetBeforeInvert(nil)
	p, err := o.Undo(ctx)
	if err != nil || p.NoOp {
		t.Fatalf("%+v, %v", p, err)
	}
	if got := textOf(t, o, a); got != "old" {
		t.Errorf("text %q", got)
	}
}

// The imported history has as many change sets as this outline saw, so only the
// epoch tells them apart (A-009).
func TestRevert_ReloadsAfterImport(t *testing.T) {
	ctx := context.Background()
	src := testgen.TempDB(t)
	other := uiOutline(t, src)
	imported, id := insertNode(t, other, other.Tree().Root(), "imported")
	var file bytes.Buffer
	if err := src.ExportJSON(ctx, &file, true, "test"); err != nil {
		t.Fatal(err)
	}

	db := testgen.TempDB(t)
	o := uiOutline(t, db)
	insertNode(t, o, o.Tree().Root(), "mine")
	if err := db.ImportJSON(ctx, &file, store.ImportReplace); err != nil {
		t.Fatal(err)
	}
	p, err := o.Revert(ctx, id)
	if err != nil || p.NoOp {
		t.Fatalf("%+v, %v", p, err)
	}
	if n, ok := o.Tree().Node(imported); !ok || n.DeletedAt == nil {
		t.Errorf("the outline didn't reload: %+v", n)
	}
	if o.Tree().Root() != other.Tree().Root() {
		t.Error("the outline kept the old root")
	}
}
