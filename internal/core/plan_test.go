package core_test

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/testgen"
)

func kinds(p core.Plan) []string {
	if p.ChangeSet == nil {
		return nil
	}
	var out []string
	for _, op := range p.ChangeSet.Ops {
		out = append(out, string(op.Kind))
	}
	return out
}

func TestPlanInsert(t *testing.T) {
	cases := []struct {
		name, before string
		parent       string // text, "" for Home
		after, under string
		want         string
	}{
		{"end of Home", "- A\n- B", "", "", "", "- A\n- B\n- new"},
		{"empty Home child", "- A", "A", "", "", "- A\n  - new"},
		{"after", "- A\n- B", "", "A", "", "- A\n- new\n- B"},
		{"before", "- A\n- B", "", "", "B", "- A\n- new\n- B"},
		{"before first", "- A\n- B", "", "", "A", "- new\n- A\n- B"},
		{"after last", "- A\n- B", "", "B", "", "- A\n- B\n- new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := testgen.MustParseOutline(t, c.before)
			parent := testgen.RootID
			if c.parent != "" {
				parent = f.ID(t, c.parent)
			}
			var a core.Anchor
			if c.after != "" {
				a.After = f.ID(t, c.after)
			}
			if c.under != "" {
				a.Before = f.ID(t, c.under)
			}
			p := core.PlanInsert(f.Ctx(), parent, a, "new", core.InsertFlags{})
			testgen.Equal(t, c.want, f.ApplyAndFormat(t, p))
		})
	}

	f := testgen.MustParseOutline(t, "- A\n- B\n  - C\n- Gone {trashed}")
	c := f.Ctx()
	bad := map[string]core.Plan{
		"line break":     core.PlanInsert(c, testgen.RootID, core.Anchor{}, "a\nb", core.InsertFlags{}),
		"marker":         core.PlanInsert(c, testgen.RootID, core.Anchor{}, "## a", core.InsertFlags{}),
		"missing parent": core.PlanInsert(c, "000000000999", core.Anchor{}, "x", core.InsertFlags{}),
		"trashed parent": core.PlanInsert(c, f.ID(t, "Gone"), core.Anchor{}, "x", core.InsertFlags{}),
		"foreign anchor": core.PlanInsert(c, testgen.RootID, core.Anchor{After: f.ID(t, "C")}, "x", core.InsertFlags{}),
	}
	for name, p := range bad {
		if !p.NoOp || p.Hint == "" {
			t.Errorf("%s: %+v", name, p)
		}
	}
	flagged := core.PlanInsert(c, testgen.RootID, core.Anchor{}, "T", core.InsertFlags{IsTask: true, IsHeading: true, Completed: true})
	testgen.Equal(t, "- A\n- B\n  - C\n- Gone {trashed}\n- [x] ## T", f.ApplyAndFormat(t, flagged))
}

func TestPlanEditText(t *testing.T) {
	f := testgen.MustParseOutline(t, "- A\n- Gone {trashed}")
	c := f.Ctx()
	id := f.ID(t, "A")
	if p := core.PlanEditText(c, id, "A"); !p.NoOp {
		t.Errorf("same text: %+v", p)
	}
	for _, p := range []core.Plan{core.PlanEditText(c, testgen.RootID, "x"), core.PlanEditText(c, f.ID(t, "Gone"), "x"), core.PlanEditText(c, id, "[ ] x")} {
		if !p.NoOp {
			t.Errorf("accepted: %+v", p)
		}
	}
	testgen.Equal(t, "- B\n- Gone {trashed}", f.ApplyAndFormat(t, core.PlanEditText(c, id, "B")))
}

func TestPlanMoveTo(t *testing.T) {
	f := testgen.MustParseOutline(t, "- A\n  - A1\n- B\n- C\n- D")
	c := f.Ctx()
	a, b, cc, d := f.ID(t, "A"), f.ID(t, "B"), f.ID(t, "C"), f.ID(t, "D")
	got := f.ApplyAndFormat(t, core.PlanMoveTo(c, []model.NodeID{d, b}, a, core.Anchor{Before: f.ID(t, "A1")}))
	testgen.Equal(t, "- A\n  - D\n  - B\n  - A1\n- C", got)

	got = f.ApplyAndFormat(t, core.PlanMoveTo(c, []model.NodeID{a, d}, testgen.RootID, core.Anchor{After: cc}))
	testgen.Equal(t, "- C\n- A\n  - D\n  - B\n  - A1", got)

	for name, p := range map[string]core.Plan{
		"into itself":     core.PlanMoveTo(c, []model.NodeID{a}, a, core.Anchor{}),
		"into descendant": core.PlanMoveTo(c, []model.NodeID{a}, b, core.Anchor{}),
		"the root":        core.PlanMoveTo(c, []model.NodeID{testgen.RootID}, a, core.Anchor{}),
		"anchor moves":    core.PlanMoveTo(c, []model.NodeID{d, b}, a, core.Anchor{After: d}),
		"nothing":         core.PlanMoveTo(c, nil, a, core.Anchor{}),
	} {
		if !p.NoOp {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

func TestPlanDelete_Subtree(t *testing.T) {
	f := testgen.MustParseOutline(t, "- Top\n  - A\n    - A1\n  - B\n  - Old {trashed}\n- Other")
	p := core.PlanDelete(f.Ctx(), []model.NodeID{f.ID(t, "Top")})
	if want := []string{"delete", "delete", "delete", "delete"}; !slices.Equal(kinds(p), want) {
		t.Fatalf("kinds %v", kinds(p))
	}
	var order []model.NodeID
	for _, op := range p.ChangeSet.Ops {
		order = append(order, op.NodeID)
	}
	want := []model.NodeID{f.ID(t, "A1"), f.ID(t, "A"), f.ID(t, "B"), f.ID(t, "Top")}
	if !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
	old, _ := f.Tree.Node(f.ID(t, "Old"))
	oldBy := old.DeletedBy
	f.ApplyAndFormat(t, p)
	for _, id := range want {
		n, _ := f.Tree.Node(id)
		if n.DeletedBy != p.ChangeSet.ID || n.DeletedAt == nil {
			t.Errorf("%s: %+v", n.Text, n)
		}
	}
	if old.DeletedBy != oldBy {
		t.Error("the earlier deletion changed")
	}
	if ps := f.Tree.Check(); len(ps) != 0 {
		t.Errorf("problems: %v", ps)
	}

	g := testgen.MustParseOutline(t, "- P\n  - C")
	nested := core.PlanDelete(g.Ctx(), []model.NodeID{g.ID(t, "C"), g.ID(t, "P"), g.ID(t, "C")})
	if len(nested.ChangeSet.Ops) != 2 {
		t.Errorf("nested selection: %v", kinds(nested))
	}
	if p := core.PlanDelete(g.Ctx(), []model.NodeID{testgen.RootID}); !p.NoOp {
		t.Errorf("deleted the root: %+v", p)
	}
}

func TestPlanRestore_OneDeletion(t *testing.T) {
	f := testgen.MustParseOutline(t, "- P\n  - Earlier\n  - Later")
	c := f.Ctx()
	earlier := core.PlanDelete(c, []model.NodeID{f.ID(t, "Earlier")})
	f.ApplyAndFormat(t, earlier)
	top := core.PlanDelete(c, []model.NodeID{f.ID(t, "P")})
	testgen.Equal(t, "- P {trashed}\n  - Earlier {trashed}\n  - Later {trashed}", f.ApplyAndFormat(t, top))

	got := f.ApplyAndFormat(t, core.PlanRestore(c, top.ChangeSet.ID))
	testgen.Equal(t, "- P\n  - Earlier {trashed}\n  - Later", got)

	got = f.ApplyAndFormat(t, core.PlanRestore(c, earlier.ChangeSet.ID))
	testgen.Equal(t, "- P\n  - Earlier\n  - Later", got)

	if p := core.PlanRestore(c, top.ChangeSet.ID); !p.NoOp {
		t.Errorf("restored twice: %+v", p)
	}
}

func TestPlanRestore_Placement(t *testing.T) {
	t.Run("parent live keeps its place", func(t *testing.T) {
		f := testgen.MustParseOutline(t, "- A\n- B\n- C")
		c := f.Ctx()
		del := core.PlanDelete(c, []model.NodeID{f.ID(t, "B")})
		f.ApplyAndFormat(t, del)
		p := core.PlanRestore(c, del.ChangeSet.ID)
		testgen.Equal(t, "restore", kinds(p)[0])
		testgen.Equal(t, 1, len(kinds(p)))
		testgen.Equal(t, "- A\n- B\n- C", f.ApplyAndFormat(t, p))
		testgen.Equal(t, "", p.Message)
	})
	t.Run("same key taken goes just after that sibling", func(t *testing.T) {
		f := testgen.MustParseOutline(t, "- A\n- B\n- C")
		c := f.Ctx()
		b := f.ID(t, "B")
		bn, _ := f.Tree.Node(b)
		position := bn.Position
		del := core.PlanDelete(c, []model.NodeID{b})
		f.ApplyAndFormat(t, del)
		// Another bullet takes the key while B is gone.
		ins := core.PlanInsert(c, testgen.RootID, core.Anchor{After: f.ID(t, "A")}, "New", core.InsertFlags{})
		f.ApplyAndFormat(t, ins)
		mv := model.Op{NodeID: f.ID(t, "New"), Kind: model.OpMove}
		if err := mv.EncodeAfter(model.MoveFields{ParentID: testgen.RootID, Position: position}); err != nil {
			t.Fatal(err)
		}
		if err := f.Tree.Apply(model.ChangeSet{ID: "m", At: ins.ChangeSet.At, Ops: []model.Op{mv}}); err != nil {
			t.Fatal(err)
		}
		got := f.ApplyAndFormat(t, core.PlanRestore(c, del.ChangeSet.ID))
		testgen.Equal(t, "- A\n- New\n- B\n- C", got)
	})
	t.Run("parent trashed goes under nearest live ancestor", func(t *testing.T) {
		f := testgen.MustParseOutline(t, "- Live\n  - X\n  - Dead\n    - Inner\n      - Kid")
		c := f.Ctx()
		del := core.PlanDelete(c, []model.NodeID{f.ID(t, "Inner")})
		f.ApplyAndFormat(t, del)
		f.ApplyAndFormat(t, core.PlanDelete(c, []model.NodeID{f.ID(t, "Dead")}))
		p := core.PlanRestore(c, del.ChangeSet.ID)
		got := f.ApplyAndFormat(t, p)
		testgen.Equal(t, "- Live\n  - X\n  - Dead {trashed}\n  - Inner\n    - Kid", got)
		testgen.Equal(t, `Restored under "Live"`, p.Message)
	})
	t.Run("chain broken by a purged row goes under Home", func(t *testing.T) {
		// Inner's parent row doesn't exist any more.
		f := testgen.MustParseOutline(t, "- First\n- Purged {id=00000000pppp}\n  - Inner {trashed}")
		d := brokenChain(t, f)
		p := core.PlanRestore(d.Ctx(), d.deletion)
		d.fixture.ApplyAndFormat(t, p)
		testgen.Equal(t, "Restored under Home", p.Message)
		n, _ := d.fixture.Tree.Node(d.inner)
		testgen.Equal(t, testgen.RootID, n.ParentID)
		if n.DeletedAt != nil {
			t.Error("still trashed")
		}
	})
	t.Run("several tops don't collide", func(t *testing.T) {
		f := testgen.MustParseOutline(t, "- Live\n  - Dead\n    - K1\n    - K2")
		c := f.Ctx()
		del := core.PlanDelete(c, []model.NodeID{f.ID(t, "K1"), f.ID(t, "K2")})
		f.ApplyAndFormat(t, del)
		f.ApplyAndFormat(t, core.PlanDelete(c, []model.NodeID{f.ID(t, "Dead")}))
		got := f.ApplyAndFormat(t, core.PlanRestore(c, del.ChangeSet.ID))
		k1, _ := f.Tree.Node(f.ID(t, "K1"))
		k2, _ := f.Tree.Node(f.ID(t, "K2"))
		if k1.ParentID != f.ID(t, "Live") || k1.Position == k2.Position {
			t.Errorf("%s\n%+v %+v", got, k1, k2)
		}
	})
}

func TestPlanSetPage_Empty(t *testing.T) {
	f := testgen.MustParseOutline(t, "- Bare\n- Has {page=text}")
	c := f.Ctx()
	bare, has := f.ID(t, "Bare"), f.ID(t, "Has")
	if p := core.PlanSetPage(c, bare, ""); !p.NoOp {
		t.Errorf("empty on no page: %+v", p)
	}
	p := core.PlanSetPage(c, has, "")
	if p.ChangeSet == nil || string(p.ChangeSet.Ops[0].After) != "null" || p.ChangeSet.Ops[0].Kind != model.OpEditPage {
		t.Fatalf("empty on a page: %+v", p)
	}
	if q := core.PlanSetPage(c, has, "text"); !q.NoOp {
		t.Errorf("same text: %+v", q)
	}
	testgen.Equal(t, "- Bare\n- Has", f.ApplyAndFormat(t, p))
	testgen.Equal(t, "- Bare {page=new}\n- Has", f.ApplyAndFormat(t, core.PlanSetPage(c, bare, "new")))
}

func TestPlanFlags(t *testing.T) {
	f := testgen.MustParseOutline(t, "- A\n- [x] B")
	c := f.Ctx()
	a, b := f.ID(t, "A"), f.ID(t, "B")
	both := []model.NodeID{a, b}
	testgen.Equal(t, "- [ ] A\n- [x] B", f.ApplyAndFormat(t, core.PlanSetTask(c, both, true)))
	if p := core.PlanSetTask(c, both, true); !p.NoOp {
		t.Errorf("no change: %+v", p)
	}
	testgen.Equal(t, "- [ ] ## A\n- [x] B", f.ApplyAndFormat(t, core.PlanSetHeading(c, []model.NodeID{a}, true)))
	complete := core.PlanSetComplete(c, both, true)
	testgen.Equal(t, 1, len(complete.ChangeSet.Ops)) // B is already complete
	testgen.Equal(t, "- [x] ## A\n- [x] B", f.ApplyAndFormat(t, complete))
	testgen.Equal(t, "- [ ] ## A\n- [ ] B", f.ApplyAndFormat(t, core.PlanSetComplete(c, both, false)))
	testgen.Equal(t, "- [ ] ## A {star}\n- [ ] B", f.ApplyAndFormat(t, core.PlanSetStar(c, []model.NodeID{a}, true)))
	if p := core.PlanSetStar(c, []model.NodeID{a}, true); !p.NoOp {
		t.Errorf("star again: %+v", p)
	}
	testgen.Equal(t, "- [ ] ## A\n- [ ] B", f.ApplyAndFormat(t, core.PlanSetStar(c, []model.NodeID{a}, false)))
	if p := core.PlanSetTask(c, []model.NodeID{testgen.RootID}, true); !p.NoOp {
		t.Errorf("root: %+v", p)
	}
}

func TestPositions_TiedSiblings(t *testing.T) {
	db := testgen.TempDB(t)
	ctx := context.Background()
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	// Two inserts planned from the same tree tie, as with concurrent connections.
	first := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{}, "one", core.InsertFlags{})
	second := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{}, "two", core.InsertFlags{})
	for _, p := range []core.Plan{first, second} {
		if _, err := o.Do(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	kids := o.Tree().Children(o.Tree().Root())
	p := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{After: kids[0]}, "three", core.InsertFlags{})
	if p.NoOp {
		t.Fatalf("no-op: %s", p.Hint)
	}
	if _, err := o.Do(ctx, p); err != nil {
		t.Fatal(err)
	}
	kids = o.Tree().Children(o.Tree().Root())
	last, _ := o.Tree().Node(kids[2])
	testgen.Equal(t, "three", last.Text)
}

func TestOutline_DoCommits(t *testing.T) {
	ctx := context.Background()
	db := testgen.TempDB(t)
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := o.Do(ctx, core.Plan{NoOp: true}); err != nil || id != "" {
		t.Errorf("no-op: %q %v", id, err)
	}
	p := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{}, "x", core.InsertFlags{})
	id, err := o.Do(ctx, p)
	if err != nil || id != p.ChangeSet.ID {
		t.Fatalf("%q %v", id, err)
	}
	if len(o.Tree().Children(o.Tree().Root())) != 1 {
		t.Error("the tree didn't follow")
	}
	// A refused plan leaves the tree alone.
	bad := p
	if _, err := o.Do(ctx, bad); err == nil {
		t.Error("the same change set committed twice")
	}
	if len(o.Tree().Children(o.Tree().Root())) != 1 {
		t.Error("the tree changed on a refused plan")
	}
	if _, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui"}); err == nil {
		t.Error("opened without Sync")
	}
}

// TestActions_MatchStore runs every planner through the real store: each plan
// must be accepted, and the tree must equal a reload.
func TestActions_MatchStore(t *testing.T) {
	ctx := context.Background()
	for seed := range uint64(5) {
		db := testgen.TempDB(t)
		o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true})
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 2))
		if done := testgen.Actions(ctx, t, r, o, 150); len(done) < 75 {
			t.Errorf("seed %d: only %d of 150 actions did anything", seed, len(done))
		}
		if p := o.Tree().Check(); len(p) != 0 {
			t.Fatalf("seed %d: %v", seed, p)
		}
		fresh, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.LoadPages(ctx); err != nil {
			t.Fatal(err)
		}
		testgen.Equal(t, testgen.FormatOutline(fresh.Tree(), testgen.State{}), testgen.FormatOutline(o.Tree(), testgen.State{}))
	}
}

// brokenFixture is a fixture whose trashed node's parent row is gone.
type brokenFixture struct {
	fixture  *testgen.Fixture
	inner    model.NodeID
	deletion model.ChangeSetID
}

func (b brokenFixture) Ctx() core.Ctx { return b.fixture.Ctx() }

func brokenChain(t *testing.T, f *testgen.Fixture) brokenFixture {
	t.Helper()
	inner := f.ID(t, "Inner")
	n, _ := f.Tree.Node(inner)
	f.Tree.ForgetNode(f.ID(t, "Purged"))
	return brokenFixture{f, inner, n.DeletedBy}
}
