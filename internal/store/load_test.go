package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

func TestLoadAll(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	live, trashed := ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, live, root, "a1", "live"), insertOp(t, trashed, root, "a2", "trashed")))
	mustCommit(t, db, set(
		kindOp(t, live, model.OpEditPage, model.PageFields{Markdown: "secret"}),
		kindOp(t, live, model.OpSetStar, model.StarFields{StarredAt: at(t1)}),
		kindOp(t, live, model.OpSetComplete, model.CompleteFields{CompletedAt: at(t1)}),
		deleteOp(t, trashed),
	))
	if _, err := db.sql.Exec(`INSERT INTO view_state VALUES (?, 1)`, string(live)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO ui_state VALUES ('zoom', '"x"')`); err != nil {
		t.Fatal(err)
	}

	d, err := db.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[model.NodeID]model.Node{}
	for _, n := range d.Nodes {
		byID[n.ID] = n
	}
	if len(byID) != 3 || byID[root].ParentID != "" {
		t.Errorf("nodes: %+v", d.Nodes)
	}
	n := byID[live]
	if n.Text != "live" || n.ParentID != root || n.Position != "a1" || n.CompletedAt == nil || n.CompletedAt.String() != at(t1).String() || n.Version != 3 {
		t.Errorf("live node: %+v", n)
	}
	if tr := byID[trashed]; tr.DeletedAt == nil || tr.DeletedBy == "" {
		t.Errorf("trashed node: %+v", tr)
	}
	if p, ok := d.Pages[live]; !ok || p.Markdown != "" || p.Version != 1 {
		t.Errorf("page metadata: %+v", p)
	}
	if s, ok := d.Stars[live]; !ok || s.String() != at(t1).String() {
		t.Errorf("star: %v", s)
	}
	if !d.Collapsed[live] || string(d.UIState["zoom"]) != `"x"` || d.Meta["root_id"] != string(root) {
		t.Errorf("view: %v %v %v", d.Collapsed, d.UIState, d.Meta)
	}

	pages, err := db.LoadPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[live].Markdown != "secret" {
		t.Errorf("pages: %+v", pages)
	}
	if p, ok, err := db.Page(ctx, live); err != nil || !ok || p.Markdown != "secret" || p.NodeID != live {
		t.Errorf("page: %+v %v %v", p, ok, err)
	}
	if _, ok, err := db.Page(ctx, trashed); err != nil || ok {
		t.Errorf("missing page: %v %v", ok, err)
	}
}

func TestLoadAll_WorksReadOnlyAndDuringWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outline.db")
	writer := openNew(t, path)
	tx, err := writer.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	ro, err := Open(path, OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	d, err := ro.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != 1 {
		t.Errorf("%d nodes", len(d.Nodes))
	}
	if _, err := ro.LoadPages(ctx); err != nil {
		t.Error(err)
	}
}

func BenchmarkLoadAll10k(b *testing.B) {
	ctx := context.Background()
	db, err := Open(filepath.Join(b.TempDir(), "outline.db"), OpenOptions{Create: true})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	root, _, err := db.Meta(ctx, "root_id")
	if err != nil {
		b.Fatal(err)
	}
	tx, err := db.sql.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := range 10_000 {
		if _, err := tx.Exec(`INSERT INTO nodes (id, parent_id, position, text, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			string(ids.NewNode()), root, fmt.Sprintf("a%d", i), fmt.Sprintf("bullet number %d with **some** text", i),
			"2026-11-01T12:00:00.000Z", "2026-11-01T12:00:00.000Z"); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := db.LoadAll(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
