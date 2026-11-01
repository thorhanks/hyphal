package testgen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
)

// Text for RealOutline: what a person's outline holds, in several scripts.
var (
	realAreas = []string{
		"Projects", "Reading list", "Ideas", "Recipes", "Travel", "Work", "Home repairs",
		"日本語のメモ", "Заметки", "ملاحظات", "Música 🎵", "Health",
	}
	realTopics = []string{
		"**Roadmap** for the spring", "Call the plumber about the *kitchen* sink", "Compare ~~three~~ two quotes",
		"Read [the design notes](https://example.com/notes)", "`make check` before pushing", "==Decide== by Friday",
		"<red>Overdue</red> invoice & receipts", "京都の旅行計画", "Купить билеты на поезд", "مراجعة الميزانية",
		"Crème brûlée, attempt 3", "👩‍🔬 lab results", "Fix tool tip on the settings page", "Ask Sam about the #budget",
		"Tags: #home #errand", "A long bullet that goes on for a while, the way notes do when you are thinking out loud",
	}
)

// RealOutline fills an outline with at least n real-looking nodes through the
// planners: nested areas, tasks, headings, completion, pages, stars and some
// deleted subtrees. It makes the P0-14 fixture.
func RealOutline(ctx context.Context, t testing.TB, r *rand.Rand, o *core.Outline, n int) {
	t.Helper()
	do := func(p core.Plan) {
		t.Helper()
		if _, err := o.Do(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(parent model.NodeID, text string, f core.InsertFlags) model.NodeID {
		t.Helper()
		p := core.PlanInsert(o.Ctx(), parent, core.Anchor{}, text, f)
		do(p)
		return p.ChangeSet.Ops[0].NodeID
	}
	var nodes []model.NodeID
	for _, area := range realAreas {
		nodes = append(nodes, insert(o.Tree().Root(), area, core.InsertFlags{IsHeading: true}))
	}
	for i := len(nodes); i < n; i++ {
		// Mostly under recent nodes, as outlines grow.
		parent := nodes[len(nodes)-1-r.IntN(min(len(nodes), 12))]
		if r.IntN(5) == 0 {
			parent = nodes[r.IntN(len(realAreas))]
		}
		if len(o.Tree().Ancestors(parent)) >= maxDepth {
			parent = nodes[r.IntN(len(realAreas))]
		}
		text := realTopics[r.IntN(len(realTopics))]
		if r.IntN(3) > 0 {
			text = fmt.Sprintf("%s (%d)", text, i)
		}
		f := core.InsertFlags{IsTask: r.IntN(5) == 0, IsHeading: r.IntN(25) == 0}
		f.Completed = f.IsTask && r.IntN(2) == 0
		nodes = append(nodes, insert(parent, text, f))
	}
	for _, id := range nodes {
		switch r.IntN(30) {
		case 0, 1:
			do(core.PlanSetPage(o.Ctx(), id, fmt.Sprintf("# Notes\n\n- [ ] Follow up on %s\n\n> 引用 — a quote\n\nMore text with <tags> & \"quotes\".\n", id)))
		case 2:
			do(core.PlanSetStar(o.Ctx(), []model.NodeID{id}, true))
		case 3:
			do(core.PlanSetComplete(o.Ctx(), []model.NodeID{id}, true)) // not a task
		}
	}
	for range n / 100 {
		id := nodes[len(realAreas)+r.IntN(len(nodes)-len(realAreas))]
		if node, _ := o.Tree().Node(id); node.DeletedAt == nil {
			do(core.PlanDelete(o.Ctx(), []model.NodeID{id}))
		}
	}
}
