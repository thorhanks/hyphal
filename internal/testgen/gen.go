package testgen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// TempDB opens a new database in a temp directory, closed when the test ends.
func TempDB(t testing.TB) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "outline.db"), store.OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// OutlineOpts sizes the Outline generator.
type OutlineOpts struct {
	Nodes int // default 50
}

// sampleTexts has markup and the scripts search and rendering must cope with.
var sampleTexts = []string{
	"plain text", "**bold** and *italic*", "~~gone~~ `code`", "==marked== <red>red</red>",
	"[a link](https://example.com)", "日本語のテキスト", "emoji 🍄 and 👩‍🔬", "café crème",
	"مرحبا بالعالم", "",
}

const maxDepth = 12

// Outline returns a random valid dump (TESTING §9). It takes a *rand.Rand; a
// rapid test seeds one from a drawn value (A-004).
func Outline(r *rand.Rand, o OutlineOpts) *store.Dump {
	if o.Nodes == 0 {
		o.Nodes = 50
	}
	now := model.NewTime(FixtureTime)
	root := ids.NewNode()
	d := &store.Dump{
		Nodes:     []model.Node{{ID: root, Position: "a0", CreatedAt: now, UpdatedAt: now, Version: 1}},
		Pages:     map[model.NodeID]model.Page{},
		Stars:     map[model.NodeID]model.Time{},
		Collapsed: map[model.NodeID]bool{},
		Meta:      map[string]string{"root_id": string(root)},
	}
	depth := map[model.NodeID]int{root: 0}
	last := map[model.NodeID]string{}
	open := []model.NodeID{root} // skewed: recent nodes get more children
	for range o.Nodes {
		parent := open[max(0, len(open)-1-r.IntN(min(len(open), 8)))]
		if r.IntN(4) == 0 {
			parent = open[r.IntN(len(open))]
		}
		position, err := fracindex.Between(last[parent], "")
		if err != nil {
			panic(err)
		}
		last[parent] = position
		n := model.Node{
			ID: ids.NewNode(), ParentID: parent, Position: position, Version: 1,
			Text: sampleTexts[r.IntN(len(sampleTexts))], CreatedAt: now, UpdatedAt: now,
			IsTask: r.IntN(6) == 0, IsHeading: r.IntN(10) == 0,
		}
		if r.IntN(6) == 0 {
			n.CompletedAt = &now
		}
		if r.IntN(10) == 0 {
			d.Pages[n.ID] = model.Page{NodeID: n.ID, Markdown: "# Page\n\n" + n.Text + "\n", UpdatedAt: now, Version: 1}
		}
		if r.IntN(12) == 0 {
			d.Stars[n.ID] = now
		}
		if r.IntN(8) == 0 {
			d.Collapsed[n.ID] = true
		}
		d.Nodes = append(d.Nodes, n)
		depth[n.ID] = depth[parent] + 1
		if depth[n.ID] < maxDepth {
			open = append(open, n.ID)
		}
	}
	// Trash a few whole subtrees, each its own deletion.
	for range r.IntN(3) + 1 {
		top := d.Nodes[1+r.IntN(len(d.Nodes)-1)]
		if top.DeletedAt == nil {
			trashSubtree(d, top.ID, ids.NewChangeSet(), now)
		}
	}
	return d
}

func trashSubtree(d *store.Dump, top model.NodeID, by model.ChangeSetID, now model.Time) {
	for i := range d.Nodes {
		if n := &d.Nodes[i]; n.ID == top && n.DeletedAt == nil {
			n.DeletedAt, n.DeletedBy = &now, by
		}
	}
	for i := range d.Nodes {
		if d.Nodes[i].ParentID == top {
			trashSubtree(d, d.Nodes[i].ID, by, now)
		}
	}
}

// Action returns one random plan valid for the tree, and its name (TESTING §9).
func Action(r *rand.Rand, c core.Ctx) (string, core.Plan) {
	var live []model.NodeID
	var deletions []model.ChangeSetID
	for _, id := range c.Tree.AllNodes() {
		n, _ := c.Tree.Node(id)
		switch {
		case id == c.Tree.Root():
		case n.DeletedAt != nil:
			if !slices.Contains(deletions, n.DeletedBy) {
				deletions = append(deletions, n.DeletedBy)
			}
		default:
			live = append(live, id)
		}
	}
	insert := func() (string, core.Plan) {
		parent := c.Tree.Root()
		if len(live) > 0 && r.IntN(2) == 0 {
			parent = live[r.IntN(len(live))]
		}
		var a core.Anchor
		if kids := c.Tree.Children(parent); len(kids) > 0 {
			switch r.IntN(3) {
			case 0:
				a.After = kids[r.IntN(len(kids))]
			case 1:
				a.Before = kids[r.IntN(len(kids))]
			}
		}
		text := fmt.Sprintf("%s %d", sampleTexts[r.IntN(len(sampleTexts))], r.IntN(1000))
		return "insert", core.PlanInsert(c, parent, a, text, core.InsertFlags{IsTask: r.IntN(5) == 0})
	}
	if len(live) == 0 {
		if len(deletions) > 0 && r.IntN(2) == 0 {
			return "restore", core.PlanRestore(c, deletions[r.IntN(len(deletions))])
		}
		return insert()
	}
	node := live[r.IntN(len(live))]
	on := r.IntN(2) == 0
	switch r.IntN(10) {
	case 0:
		return "edit", core.PlanEditText(c, node, fmt.Sprintf("edited %d", r.IntN(1000)))
	case 1:
		parent := c.Tree.Root()
		if candidate := live[r.IntN(len(live))]; candidate != node && !slices.Contains(c.Tree.Ancestors(candidate), node) {
			parent = candidate
		}
		return "move", core.PlanMoveTo(c, []model.NodeID{node}, parent, core.Anchor{})
	case 2:
		return "delete", core.PlanDelete(c, []model.NodeID{node})
	case 3:
		if len(deletions) > 0 {
			return "restore", core.PlanRestore(c, deletions[r.IntN(len(deletions))])
		}
	case 4:
		return "task", core.PlanSetTask(c, []model.NodeID{node}, on)
	case 5:
		return "heading", core.PlanSetHeading(c, []model.NodeID{node}, on)
	case 6:
		return "complete", core.PlanSetComplete(c, []model.NodeID{node}, on)
	case 7:
		return "star", core.PlanSetStar(c, []model.NodeID{node}, on)
	case 8:
		markdown := ""
		if on {
			markdown = fmt.Sprintf("# Page %d\n", r.IntN(1000))
		}
		return "page", core.PlanSetPage(c, node, markdown)
	}
	return insert()
}

// Actions runs n random plans on an outline (TESTING §9). It returns the change
// sets committed, in order; no-ops commit nothing.
func Actions(ctx context.Context, t testing.TB, r *rand.Rand, o *core.Outline, n int) []model.ChangeSetID {
	t.Helper()
	var done []model.ChangeSetID
	for range n {
		name, p := Action(r, o.Ctx())
		id, err := o.Do(ctx, p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if id != "" {
			done = append(done, id)
		}
	}
	return done
}
