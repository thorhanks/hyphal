package core

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

func encode(t rapid.TB, id model.NodeID, kind model.OpKind, after any) model.Op {
	o := model.Op{NodeID: id, Kind: kind}
	if err := o.EncodeAfter(after); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return o
}

// snapshot renders all of a tree's state, for comparing with a reload.
func snapshot(t rapid.TB, tr *Tree) string {
	nodes := slices.SortedFunc(maps.Values(tr.nodes), func(a, b *model.Node) int { return strings.Compare(string(a.ID), string(b.ID)) })
	kids := map[model.NodeID][]model.NodeID{}
	for id, k := range tr.children {
		if len(k) > 0 {
			kids[id] = k
		}
	}
	b, err := json.MarshalIndent(map[string]any{
		"nodes": nodes, "children": kids, "pages": tr.pages, "stars": tr.stars,
	}, "", " ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// randomOps returns a change set's ops that are valid for the tree.
func randomOps(t *rapid.T, tr *Tree, clock time.Time, next func() string) []model.Op {
	var live, trashed []model.NodeID
	for _, id := range slices.Sorted(maps.Keys(tr.nodes)) {
		switch n := tr.nodes[id]; {
		case id == tr.root:
		case n.DeletedAt != nil:
			trashed = append(trashed, id)
		default:
			live = append(live, id)
		}
	}
	pick := func(label string, from []model.NodeID) model.NodeID {
		return rapid.SampledFrom(from).Draw(t, label)
	}
	now := model.NewTime(clock)
	kind := rapid.SampledFrom([]string{"insert", "insert", "edit", "move", "delete", "restore", "task", "heading", "complete", "page", "star"}).Draw(t, "kind")
	if kind != "insert" && len(live) == 0 && kind != "restore" {
		kind = "insert"
	}
	if kind == "restore" && len(trashed) == 0 {
		kind = "insert"
	}
	switch kind {
	case "edit":
		return []model.Op{encode(t, pick("node", live), model.OpEditText, model.TextFields{Text: next()})}
	case "task":
		return []model.Op{encode(t, pick("node", live), model.OpSetTask, model.TaskFields{IsTask: rapid.Bool().Draw(t, "on")})}
	case "heading":
		return []model.Op{encode(t, pick("node", live), model.OpSetHeading, model.HeadingFields{IsHeading: rapid.Bool().Draw(t, "on")})}
	case "complete":
		var when *model.Time
		if rapid.Bool().Draw(t, "on") {
			when = &now
		}
		return []model.Op{encode(t, pick("node", live), model.OpSetComplete, model.CompleteFields{CompletedAt: when})}
	case "star":
		var when *model.Time
		if rapid.Bool().Draw(t, "on") {
			when = &now
		}
		return []model.Op{encode(t, pick("node", live), model.OpSetStar, model.StarFields{StarredAt: when})}
	case "page":
		id := pick("node", live)
		if _, ok := tr.pages[id]; ok && rapid.Bool().Draw(t, "clear") {
			return []model.Op{encode(t, id, model.OpEditPage, nil)}
		}
		return []model.Op{encode(t, id, model.OpEditPage, model.PageFields{Markdown: next()})}
	case "move":
		id := pick("node", live)
		targets := []model.NodeID{tr.root}
		for _, c := range live {
			if c != id && !slices.Contains(tr.Ancestors(c), id) {
				targets = append(targets, c)
			}
		}
		return []model.Op{encode(t, id, model.OpMove, model.MoveFields{ParentID: pick("target", targets), Position: randomPosition(t)})}
	case "delete":
		var ops []model.Op
		var walk func(model.NodeID)
		walk = func(id model.NodeID) {
			for _, c := range tr.Children(id) {
				walk(c)
			}
			ops = append(ops, encode(t, id, model.OpDelete, model.DeleteFields{Deleted: true}))
		}
		walk(pick("node", live))
		return ops
	case "restore":
		var ready []model.NodeID
		for _, id := range trashed {
			if p := tr.nodes[tr.nodes[id].ParentID]; p.DeletedAt == nil {
				ready = append(ready, id)
			}
		}
		if len(ready) > 0 {
			return []model.Op{encode(t, pick("node", ready), model.OpRestore, model.DeleteFields{})}
		}
	}
	parent := tr.root
	if len(live) > 0 && rapid.Bool().Draw(t, "nested") {
		parent = pick("parent", live)
	}
	return []model.Op{encode(t, ids.NewNode(), model.OpInsert, model.InsertFields{
		ParentID: parent, Position: randomPosition(t), Text: next(), CreatedAt: now,
	})}
}

// randomPosition draws from few keys, so siblings tie and order falls to the ID.
func randomPosition(t *rapid.T) string {
	return fmt.Sprintf("a%d", rapid.IntRange(0, 5).Draw(t, "position"))
}

func TestTreeApply_MatchesStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var run atomic.Int64
	rapid.Check(t, func(t *rapid.T) {
		path := filepath.Join(dir, fmt.Sprintf("%d.db", run.Add(1)))
		db, err := store.Open(path, store.OpenOptions{Create: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		load := func() *Tree {
			d, err := db.LoadAll(ctx)
			if err != nil {
				t.Fatal(err)
			}
			tr := NewTree(d)
			pages, err := db.LoadPages(ctx)
			if err != nil {
				t.Fatal(err)
			}
			tr.SetPages(pages)
			return tr
		}
		tree := load()
		clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		count := 0
		next := func() string {
			count++
			return fmt.Sprintf("text %d", count)
		}
		for range rapid.IntRange(1, 25).Draw(t, "steps") {
			clock = clock.Add(time.Minute)
			cs := model.ChangeSet{ID: ids.NewChangeSet(), At: model.NewTime(clock), Source: "ui", Ops: randomOps(t, tree, clock, next)}
			if err := tree.Apply(cs); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Commit(ctx, cs, nil, nil); err != nil {
				t.Fatal(err)
			}
			reloaded := load()
			if got, want := snapshot(t, tree), snapshot(t, reloaded); got != want {
				t.Fatalf("tree differs from reload\ntree: %s\nreload: %s", got, want)
			}
			if problems := tree.Check(); len(problems) > 0 {
				t.Fatalf("problems: %v", problems)
			}
		}
	})
}

// stamp is a fixed time for crafted rows.
func stamp() *model.Time {
	v := model.NewTime(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	return &v
}

const (
	idRoot = model.NodeID("0000000000rt")
	idA    = model.NodeID("00000000000a")
	idB    = model.NodeID("00000000000b")
	idC    = model.NodeID("00000000000c")
)

func dumpOf(nodes ...model.Node) *store.Dump {
	return &store.Dump{
		Nodes: nodes,
		Pages: map[model.NodeID]model.Page{},
		Stars: map[model.NodeID]model.Time{},
		Meta:  map[string]string{"root_id": string(idRoot)},

		Collapsed: map[model.NodeID]bool{},
	}
}

func rootNode() model.Node { return model.Node{ID: idRoot, Position: "a0"} }

func child(id, parent model.NodeID, position string) model.Node {
	return model.Node{ID: id, ParentID: parent, Position: position}
}

func TestTree_ChildrenOrder(t *testing.T) {
	gone := child(idC, idRoot, "a0")
	gone.DeletedAt, gone.DeletedBy = stamp(), "x"
	tr := NewTree(dumpOf(rootNode(), child(idB, idRoot, "a0"), child(idA, idRoot, "a0"), child("00000000000d", idRoot, "Zz"), gone))
	want := []model.NodeID{"00000000000d", idA, idB}
	if got := tr.Children(idRoot); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if n, ok := tr.Node(idC); !ok || n.DeletedAt == nil {
		t.Error("the trashed node should still be reachable")
	}
	if tr.Root() != idRoot || tr.Parent(idA) != idRoot || tr.Parent(idRoot) != "" {
		t.Error("root or parent")
	}
}

func TestTree_AncestorsRootFirst(t *testing.T) {
	tr := NewTree(dumpOf(rootNode(), child(idA, idRoot, "a0"), child(idB, idA, "a0")))
	if got := tr.Ancestors(idB); !slices.Equal(got, []model.NodeID{idRoot, idA}) {
		t.Errorf("got %v", got)
	}
	if got := tr.Ancestors(idRoot); len(got) != 0 {
		t.Errorf("root: %v", got)
	}
	// A damaged tree can't make it spin.
	loop := NewTree(dumpOf(rootNode(), child(idA, idB, "a0"), child(idB, idA, "a0")))
	if got := loop.Ancestors(idA); len(got) != 1 {
		t.Errorf("cycle: %v", got)
	}
}

func TestTree_SetPagesKeepsNewer(t *testing.T) {
	d := dumpOf(rootNode(), child(idA, idRoot, "a0"), child(idB, idRoot, "a1"), child(idC, idRoot, "a2"))
	for _, id := range []model.NodeID{idA, idB} {
		d.Pages[id] = model.Page{NodeID: id, Version: 1}
	}
	tr := NewTree(d)
	if tr.PagesLoaded() {
		t.Error("loaded too early")
	}
	cs := model.ChangeSet{ID: "c", At: *stamp(), Ops: []model.Op{
		encode(t, idA, model.OpEditPage, model.PageFields{Markdown: "newer"}),
		encode(t, idB, model.OpEditPage, nil),
	}}
	if err := tr.Apply(cs); err != nil {
		t.Fatal(err)
	}
	tr.SetPages(map[model.NodeID]model.Page{
		idA: {NodeID: idA, Markdown: "older", Version: 1},
		idB: {NodeID: idB, Markdown: "deleted since", Version: 1},
	})
	if p, _ := tr.Page(idA); p.Markdown != "newer" || p.Version != 2 {
		t.Errorf("edited page: %+v", p)
	}
	if _, ok := tr.Page(idB); ok {
		t.Error("the deleted page came back")
	}
	if !tr.PagesLoaded() {
		t.Error("not marked loaded")
	}
}

func TestTree_ApplyErrors(t *testing.T) {
	tr := NewTree(dumpOf(rootNode()))
	if err := tr.Apply(model.ChangeSet{}); err == nil {
		t.Error("no ID or time accepted")
	}
	cs := model.ChangeSet{ID: "c", At: *stamp(), Ops: []model.Op{encode(t, idA, model.OpEditText, model.TextFields{Text: "x"})}}
	if err := tr.Apply(cs); err == nil {
		t.Error("edit of a missing node accepted")
	}
}

func TestTreeCheck(t *testing.T) {
	trashedAt := stamp()
	gone := func(n model.Node) model.Node {
		n.DeletedAt, n.DeletedBy = trashedAt, "x"
		return n
	}
	text := func(n model.Node, s string) model.Node {
		n.Text = s
		return n
	}
	tests := []struct {
		name      string
		dump      func() *store.Dump
		invariant int
		node      model.NodeID
	}{
		{"1 two roots", func() *store.Dump { return dumpOf(rootNode(), model.Node{ID: idA, Position: "a0"}) }, 1, ""},
		{"1 no root row", func() *store.Dump { return dumpOf(child(idA, idB, "a0"), child(idB, idA, "a0")) }, 1, ""},
		{"1 root_id isn't the root", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Meta["root_id"] = string(idA)
			return d
		}, 1, idA},
		{"2 missing parent", func() *store.Dump { return dumpOf(rootNode(), child(idA, idB, "a0")) }, 2, idA},
		{"3 cycle", func() *store.Dump { return dumpOf(rootNode(), child(idA, idB, "a0"), child(idB, idA, "a0")) }, 3, idA},
		{"3 self parent", func() *store.Dump { return dumpOf(rootNode(), child(idA, idA, "a0")) }, 3, idA},
		{"4 live under trashed", func() *store.Dump {
			return dumpOf(rootNode(), gone(child(idA, idRoot, "a0")), child(idB, idA, "a0"))
		}, 4, idB},
		{"5 page without node", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Pages[idA] = model.Page{NodeID: idA}
			return d
		}, 5, idA},
		{"5 star without node", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Stars[idA] = *trashedAt
			return d
		}, 5, idA},
		{"5 view state without node", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Collapsed[idA] = true
			return d
		}, 5, idA},
		{"5 root has a page", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Pages[idRoot] = model.Page{NodeID: idRoot}
			return d
		}, 5, idRoot},
		{"5 root has a star", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Stars[idRoot] = *trashedAt
			return d
		}, 5, idRoot},
		{"6 line break", func() *store.Dump { return dumpOf(rootNode(), text(child(idA, idRoot, "a0"), "a\nb")) }, 6, idA},
		{"6 leading marker", func() *store.Dump { return dumpOf(rootNode(), text(child(idA, idRoot, "a0"), "[ ] x")) }, 6, idA},
		{"7 bad ID", func() *store.Dump { return dumpOf(rootNode(), child("BAD", idRoot, "a0")) }, 7, "BAD"},
		{"7 bad position", func() *store.Dump { return dumpOf(rootNode(), child(idA, idRoot, "!")) }, 7, idA},
		{"8 deleted_at alone", func() *store.Dump {
			n := child(idA, idRoot, "a0")
			n.DeletedAt = trashedAt
			return dumpOf(rootNode(), n)
		}, 8, idA},
		{"8 deleted_by alone", func() *store.Dump {
			n := child(idA, idRoot, "a0")
			n.DeletedBy = "x"
			return dumpOf(rootNode(), n)
		}, 8, idA},
		{"9 inbox missing", func() *store.Dump {
			d := dumpOf(rootNode())
			d.Meta["inbox_id"] = string(idA)
			return d
		}, 9, idA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := NewTree(tt.dump()).Check()
			if !slices.ContainsFunc(problems, func(p Problem) bool {
				return p.Invariant == tt.invariant && p.Node == tt.node
			}) {
				t.Errorf("no invariant %d on %q in %v", tt.invariant, tt.node, problems)
			}
		})
	}

	t.Run("healthy", func(t *testing.T) {
		d := dumpOf(rootNode(), child(idA, idRoot, "a0"), gone(child(idB, idA, "a0")))
		d.Meta["inbox_id"] = string(idB) // a trashed Inbox is allowed
		d.Collapsed[idRoot] = true
		if problems := NewTree(d).Check(); len(problems) != 0 {
			t.Errorf("problems: %v", problems)
		}
	})
}
