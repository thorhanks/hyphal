package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

var (
	t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func at(tm time.Time) *model.Time {
	v := model.NewTime(tm)
	return &v
}

func op(t *testing.T, id model.NodeID, kind model.OpKind, before, after any) model.Op {
	t.Helper()
	o := model.Op{NodeID: id, Kind: kind}
	if err := o.EncodeBefore(before); err != nil {
		t.Fatal(err)
	}
	if err := o.EncodeAfter(after); err != nil {
		t.Fatal(err)
	}
	return o
}

func insertOp(t *testing.T, id, parent model.NodeID, position, text string) model.Op {
	t.Helper()
	o := model.Op{NodeID: id, Kind: model.OpInsert}
	if err := o.EncodeAfter(model.InsertFields{ParentID: parent, Position: position, Text: text, CreatedAt: *at(t0)}); err != nil {
		t.Fatal(err)
	}
	return o
}

func kindOp(t *testing.T, id model.NodeID, kind model.OpKind, after any) model.Op {
	t.Helper()
	o := model.Op{NodeID: id, Kind: kind}
	if err := o.EncodeAfter(after); err != nil {
		t.Fatal(err)
	}
	return o
}

func deleteOp(t *testing.T, id model.NodeID) model.Op {
	return kindOp(t, id, model.OpDelete, model.DeleteFields{Deleted: true})
}

func restoreOp(t *testing.T, id model.NodeID) model.Op {
	return kindOp(t, id, model.OpRestore, model.DeleteFields{})
}

func commitDB(t *testing.T) (*DB, model.NodeID) {
	t.Helper()
	db := openNew(t, filepath.Join(t.TempDir(), "outline.db"))
	root, _, err := db.Meta(context.Background(), "root_id")
	if err != nil {
		t.Fatal(err)
	}
	return db, model.NodeID(root)
}

func set(ops ...model.Op) model.ChangeSet {
	return model.ChangeSet{At: *at(t0), Source: "ui", Ops: ops}
}

func mustCommit(t *testing.T, db *DB, cs model.ChangeSet) CommitResult {
	t.Helper()
	res, err := db.Commit(context.Background(), cs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCommit_WritesHistory(t *testing.T) {
	db, root := commitDB(t)
	a, b := ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, b, root, "a1", "other")))
	res := mustCommit(t, db, model.ChangeSet{
		ID: ids.NewChangeSet(), At: *at(t1), Source: "agent:bot", Description: "three things", UndoOf: "undone",
		Ops: []model.Op{
			insertOp(t, a, root, "a2", "first"),
			kindOp(t, a, model.OpEditText, model.TextFields{Text: "second"}),
			kindOp(t, a, model.OpMove, model.MoveFields{ParentID: b, Position: "a0"}),
		},
	})

	var parent, position, text string
	var version int
	if err := db.sql.QueryRow(`SELECT parent_id, position, text, version FROM nodes WHERE id = ?`, string(a)).Scan(&parent, &position, &text, &version); err != nil {
		t.Fatal(err)
	}
	if parent != string(b) || position != "a0" || text != "second" || version != 3 {
		t.Errorf("node: %s %s %s %d", parent, position, text, version)
	}

	var src, desc, undo, when string
	if err := db.sql.QueryRow(`SELECT at, source, description, undo_of FROM change_sets WHERE id = ?`, string(res.ChangeSet.ID)).Scan(&when, &src, &desc, &undo); err != nil {
		t.Fatal(err)
	}
	if when != "2026-11-01T13:00:00.000Z" || src != "agent:bot" || desc != "three things" || undo != "undone" {
		t.Errorf("change set: %s %s %s %s", when, src, desc, undo)
	}

	rows, err := db.sql.Query(`SELECT kind, before, after FROM ops WHERE change_set_id = ? ORDER BY id`, string(res.ChangeSet.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var kind string
		var before, after sql.NullString
		if err := rows.Scan(&kind, &before, &after); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s %s", kind, before.String))
	}
	want := []string{
		"insert ",
		`edit_text {"text":"first"}`,
		fmt.Sprintf(`move {"parent_id":%q,"position":"a2"}`, root),
	}
	if !slices.Equal(got, want) {
		t.Errorf("ops:\n%q\nwant\n%q", got, want)
	}
	if len(res.ChangeSet.Ops) != 3 || string(res.ChangeSet.Ops[0].Before) != "null" || string(res.ChangeSet.Ops[1].Before) != `{"text":"first"}` {
		t.Errorf("result ops: %+v", res.ChangeSet.Ops)
	}
}

func TestCommit_FillsIDAndTime(t *testing.T) {
	db, root := commitDB(t)
	res := mustCommit(t, db, model.ChangeSet{Source: "cli", Ops: []model.Op{insertOp(t, ids.NewNode(), root, "a1", "x")}})
	if !ids.Valid(string(res.ChangeSet.ID)) || res.ChangeSet.At.IsZero() {
		t.Errorf("got %+v", res.ChangeSet)
	}
}

func TestCommit_RecomputesBefore(t *testing.T) {
	db, root := commitDB(t)
	id := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, id, root, "a1", "true text")))
	res := mustCommit(t, db, set(op(t, id, model.OpEditText, model.TextFields{Text: "a lie"}, model.TextFields{Text: "new"})))
	if got := string(res.ChangeSet.Ops[0].Before); got != `{"text":"true text"}` {
		t.Errorf("before: %s", got)
	}
	var stored string
	if err := db.sql.QueryRow(`SELECT before FROM ops WHERE change_set_id = ?`, string(res.ChangeSet.ID)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `{"text":"true text"}` {
		t.Errorf("stored before: %s", stored)
	}
}

func TestCommit_TimestampsAndVersions(t *testing.T) {
	db, root := commitDB(t)
	other := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, other, root, "a0", "other")))
	tests := []struct {
		name        string
		op          func(id model.NodeID) model.Op
		updated     bool
		nodeVersion int
		pageVersion int
	}{
		{"edit_text", func(id model.NodeID) model.Op { return kindOp(t, id, model.OpEditText, model.TextFields{Text: "b"}) }, true, 2, 0},
		{"set_task", func(id model.NodeID) model.Op { return kindOp(t, id, model.OpSetTask, model.TaskFields{IsTask: true}) }, true, 2, 0},
		{"set_heading", func(id model.NodeID) model.Op {
			return kindOp(t, id, model.OpSetHeading, model.HeadingFields{IsHeading: true})
		}, true, 2, 0},
		{"set_complete", func(id model.NodeID) model.Op {
			return kindOp(t, id, model.OpSetComplete, model.CompleteFields{CompletedAt: at(t1)})
		}, true, 2, 0},
		{"edit_page", func(id model.NodeID) model.Op {
			return kindOp(t, id, model.OpEditPage, model.PageFields{Markdown: "m"})
		}, true, 2, 1},
		{"move", func(id model.NodeID) model.Op {
			return kindOp(t, id, model.OpMove, model.MoveFields{ParentID: other, Position: "a0"})
		}, false, 2, 0},
		{"set_star", func(id model.NodeID) model.Op {
			return kindOp(t, id, model.OpSetStar, model.StarFields{StarredAt: at(t1)})
		}, false, 1, 0},
		{"delete", func(id model.NodeID) model.Op { return deleteOp(t, id) }, false, 2, 0},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ids.NewNode()
			mustCommit(t, db, set(insertOp(t, id, root, fmt.Sprintf("a%d", i+1), "a")))
			mustCommit(t, db, model.ChangeSet{At: *at(t1), Source: "ui", Ops: []model.Op{tt.op(id)}})
			var updated string
			var version int
			if err := db.sql.QueryRow(`SELECT updated_at, version FROM nodes WHERE id = ?`, string(id)).Scan(&updated, &version); err != nil {
				t.Fatal(err)
			}
			want := at(t0).String()
			if tt.updated {
				want = at(t1).String()
			}
			if updated != want {
				t.Errorf("updated_at %s, want %s", updated, want)
			}
			if version != tt.nodeVersion {
				t.Errorf("version %d, want %d", version, tt.nodeVersion)
			}
			var pageVersion int
			_ = db.sql.QueryRow(`SELECT version FROM pages WHERE node_id = ?`, string(id)).Scan(&pageVersion)
			if pageVersion != tt.pageVersion {
				t.Errorf("page version %d, want %d", pageVersion, tt.pageVersion)
			}
		})
	}
}

func TestCommit_Pages(t *testing.T) {
	db, root := commitDB(t)
	id := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, id, root, "a1", "x")))
	before := func(res CommitResult) string { return string(res.ChangeSet.Ops[0].Before) }

	res := mustCommit(t, db, set(kindOp(t, id, model.OpEditPage, model.PageFields{Markdown: "one"})))
	if before(res) != "null" {
		t.Errorf("create: %s", before(res))
	}
	res = mustCommit(t, db, set(kindOp(t, id, model.OpEditPage, model.PageFields{Markdown: "two"})))
	if before(res) != `{"markdown":"one"}` {
		t.Errorf("edit: %s", before(res))
	}
	if v := queryInt(t, db.sql, `SELECT version FROM pages`); v != 2 {
		t.Errorf("page version %d", v)
	}
	res = mustCommit(t, db, set(kindOp(t, id, model.OpEditPage, nil)))
	if before(res) != `{"markdown":"two"}` || string(res.ChangeSet.Ops[0].After) != "null" {
		t.Errorf("delete: %s %s", before(res), res.ChangeSet.Ops[0].After)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM pages`); n != 0 {
		t.Errorf("%d pages", n)
	}
	var stored sql.NullString
	if err := db.sql.QueryRow(`SELECT after FROM ops WHERE change_set_id = ?`, string(res.ChangeSet.ID)).Scan(&stored); err != nil || stored.Valid {
		t.Errorf("stored after %v, %v", stored, err)
	}
}

func TestCommit_Stars(t *testing.T) {
	db, root := commitDB(t)
	id := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, id, root, "a1", "x")))
	res := mustCommit(t, db, set(kindOp(t, id, model.OpSetStar, model.StarFields{StarredAt: at(t0)})))
	if got := string(res.ChangeSet.Ops[0].Before); got != `{"starred_at":null}` {
		t.Errorf("before: %s", got)
	}
	res = mustCommit(t, db, set(kindOp(t, id, model.OpSetStar, model.StarFields{})))
	if got := string(res.ChangeSet.Ops[0].Before); got != fmt.Sprintf(`{"starred_at":%q}`, at(t0)) {
		t.Errorf("before: %s", got)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM stars`); n != 0 {
		t.Errorf("%d stars", n)
	}
}

func TestCommit_ExpectConflict(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	id := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, id, root, "a1", "x")))
	for _, text := range []string{"b", "c", "d"} {
		mustCommit(t, db, set(kindOp(t, id, model.OpEditText, model.TextFields{Text: text})))
	}
	edit := set(kindOp(t, id, model.OpEditText, model.TextFields{Text: "late"}))
	before := queryInt(t, db.sql, `SELECT count(*) FROM change_sets`)
	if _, err := db.Commit(ctx, edit, []Expect{{NodeID: id, Version: 3}}, nil); !errors.Is(err, ErrConflict) {
		t.Errorf("got %v", err)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM change_sets`); n != before {
		t.Error("the conflict wrote a change set")
	}
	if _, err := db.Commit(ctx, edit, []Expect{{NodeID: id, Version: 0, Page: true}, {NodeID: id, Version: 4}}, nil); err != nil {
		t.Errorf("right versions: %v", err)
	}
}

func TestCommit_ReportsStale(t *testing.T) {
	db, root := commitDB(t)
	a, b := ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, a, root, "a1", "x"), insertOp(t, b, root, "a2", "y")))
	mustCommit(t, db, set(kindOp(t, a, model.OpEditText, model.TextFields{Text: "newer"})))
	res, err := db.Commit(context.Background(), set(kindOp(t, a, model.OpEditText, model.TextFields{Text: "mine"})), nil,
		map[model.NodeID]int64{a: 1, b: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Stale, []model.NodeID{a}) {
		t.Errorf("stale: %v", res.Stale)
	}
	if v := queryInt(t, db.sql, fmt.Sprintf(`SELECT count(*) FROM nodes WHERE text = 'mine' AND id = '%s'`, a)); v != 1 {
		t.Error("did not commit")
	}
}

func TestCommit_Invariants(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	parent, child, trashed, free := ids.NewNode(), ids.NewNode(), ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(
		insertOp(t, parent, root, "a1", "parent"),
		insertOp(t, child, parent, "a0", "child"),
		insertOp(t, trashed, root, "a2", "trashed"),
	))
	mustCommit(t, db, set(deleteOp(t, trashed)))
	mv := func(id, to model.NodeID) model.Op {
		return kindOp(t, id, model.OpMove, model.MoveFields{ParentID: to, Position: "a5"})
	}
	txt := func(s string) model.Op { return kindOp(t, parent, model.OpEditText, model.TextFields{Text: s}) }

	tests := []struct {
		name string
		ops  []model.Op
	}{
		{"move under own descendant", []model.Op{mv(parent, child)}},
		{"move under itself", []model.Op{mv(parent, parent)}},
		{"move under trashed", []model.Op{mv(child, trashed)}},
		{"move under missing", []model.Op{mv(child, free)}},
		{"move with bad position", []model.Op{kindOp(t, child, model.OpMove, model.MoveFields{ParentID: root, Position: "!"})}},
		{"insert under trashed", []model.Op{insertOp(t, free, trashed, "a0", "x")}},
		{"insert under missing", []model.Op{insertOp(t, free, ids.NewNode(), "a0", "x")}},
		{"insert existing", []model.Op{insertOp(t, child, root, "a9", "x")}},
		{"insert bad position", []model.Op{insertOp(t, free, root, "", "x")}},
		{"insert bad ID", []model.Op{insertOp(t, "NOPE", root, "a9", "x")}},
		{"insert with newline", []model.Op{insertOp(t, free, root, "a9", "two\nlines")}},
		{"insert with marker", []model.Op{insertOp(t, free, root, "a9", "[ ] task")}},
		{"edit the root", []model.Op{kindOp(t, root, model.OpEditText, model.TextFields{Text: "x"})}},
		{"move the root", []model.Op{mv(root, parent)}},
		{"delete the root", []model.Op{deleteOp(t, root)}},
		{"complete the root", []model.Op{kindOp(t, root, model.OpSetComplete, model.CompleteFields{CompletedAt: at(t0)})}},
		{"star the root", []model.Op{kindOp(t, root, model.OpSetStar, model.StarFields{StarredAt: at(t0)})}},
		{"page on the root", []model.Op{kindOp(t, root, model.OpEditPage, model.PageFields{Markdown: "x"})}},
		{"edit missing", []model.Op{kindOp(t, free, model.OpEditText, model.TextFields{Text: "x"})}},
		{"text with CR", []model.Op{txt("a\rb")}},
		{"text with heading marker", []model.Op{txt("## x")}},
		{"text with done marker", []model.Op{txt("[X] x")}},
		{"delete with live child", []model.Op{deleteOp(t, parent)}},
		{"delete twice", []model.Op{deleteOp(t, trashed)}},
		{"delete that doesn't delete", []model.Op{kindOp(t, child, model.OpDelete, model.DeleteFields{})}},
		{"restore a live node", []model.Op{restoreOp(t, child)}},
		{"restore that deletes", []model.Op{kindOp(t, trashed, model.OpRestore, model.DeleteFields{Deleted: true})}},
		{"delete a page that isn't there", []model.Op{kindOp(t, child, model.OpEditPage, nil)}},
		{"malformed after", []model.Op{{NodeID: child, Kind: model.OpEditText, After: json.RawMessage(`{}`)}}},
		{"malformed before", []model.Op{{NodeID: child, Kind: model.OpEditText, Before: json.RawMessage(`{}`), After: json.RawMessage(`{"text":"x"}`)}}},
		{"unknown kind", []model.Op{{NodeID: child, Kind: "paint", After: json.RawMessage(`{}`)}}},
		{"good op then bad", []model.Op{kindOp(t, child, model.OpEditText, model.TextFields{Text: "changed"}), mv(parent, child)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changeSets := queryInt(t, db.sql, `SELECT count(*) FROM change_sets`)
			_, err := db.Commit(ctx, set(tt.ops...), nil, nil)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
			if n := queryInt(t, db.sql, `SELECT count(*) FROM change_sets`); n != changeSets {
				t.Error("wrote a change set")
			}
			if n := queryInt(t, db.sql, `SELECT count(*) FROM nodes WHERE text = 'changed'`); n != 0 {
				t.Error("kept a partial change")
			}
		})
	}

	for name, cs := range map[string]model.ChangeSet{
		"no source":     {Ops: nil},
		"bad source":    {Source: "tui"},
		"empty agent":   {Source: "agent:"},
		"bad change ID": {ID: "x", Source: "ui"},
	} {
		if _, err := db.Commit(ctx, cs, nil, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	dup := mustCommit(t, db, set())
	if _, err := db.Commit(ctx, model.ChangeSet{ID: dup.ChangeSet.ID, Source: "ui"}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("reused change set ID: %v", err)
	}
}

func TestCommit_Atomic(t *testing.T) {
	db, root := commitDB(t)
	boom := errors.New("boom")
	db.commitHook = func(i int) error {
		if i == 1 {
			return boom
		}
		return nil
	}
	_, err := db.Commit(context.Background(), set(insertOp(t, ids.NewNode(), root, "a1", "x"), insertOp(t, ids.NewNode(), root, "a2", "y")), nil, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	db.commitHook = nil
	for table, want := range map[string]int64{"nodes": 1, "change_sets": 0, "ops": 0} {
		if n := queryInt(t, db.sql, `SELECT count(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows", table, n)
		}
	}
}

func helperCommit(path string, id model.NodeID) error {
	db, err := Open(path, OpenOptions{Create: true})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for i := range 25 {
		o := model.Op{NodeID: id, Kind: model.OpEditText}
		if err := o.EncodeAfter(model.TextFields{Text: fmt.Sprintf("%d-%d", os.Getpid(), i)}); err != nil {
			return err
		}
		if _, err := db.Commit(context.Background(), model.ChangeSet{Source: "cli", Ops: []model.Op{o}}, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func TestCommit_ConcurrentProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline.db")
	db := openNew(t, path)
	root, _, err := db.Meta(context.Background(), "root_id")
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, id, model.NodeID(root), "a1", "x")))

	cmds := make([]*exec.Cmd, 2)
	outs := make([]strings.Builder, 2)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0])
		cmds[i].Env = append(os.Environ(), "STORE_TEST_HELPER=commit", "STORE_TEST_DB="+path, "STORE_TEST_NODE="+string(id))
		cmds[i].Stderr = &outs[i]
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Errorf("helper %d: %v: %s", i, err, outs[i].String())
		}
	}
	if n := queryInt(t, db.sql, `SELECT version FROM nodes WHERE id = '`+string(id)+`'`); n != 51 {
		t.Errorf("version %d", n)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM ops WHERE kind = 'edit_text'`); n != 50 {
		t.Errorf("%d ops", n)
	}
}

func TestCommit_Busy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outline.db")
	db := openNew(t, path)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	other, err := Open(path, OpenOptions{BusyTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if _, err := other.Commit(ctx, set(), nil, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("got %v", err)
	}
}

// TestCommit_InversePayloads covers AC9: inverses from model.Inverse commit
// with the right stamps, and merged_into survives undo and redo (D-049).
func TestCommit_InversePayloads(t *testing.T) {
	db, root := commitDB(t)
	id, survivor := ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, survivor, root, "a0", "survivor")))

	// Undoing an insert records a delete stamped with its own change set.
	ins := mustCommit(t, db, set(insertOp(t, id, root, "a1", "x")))
	undo := mustCommit(t, db, model.ChangeSet{ID: ids.NewChangeSet(), At: *at(t1), Source: "ui", Ops: []model.Op{model.Inverse(ins.ChangeSet.Ops[0])}})
	want := fmt.Sprintf(`{"deleted":true,"deleted_at":%q,"deleted_by":%q}`, at(t1), undo.ChangeSet.ID)
	if got := string(undo.ChangeSet.Ops[0].After); got != want {
		t.Errorf("undo insert: %s, want %s", got, want)
	}
	var deletedAt, deletedBy string
	if err := db.sql.QueryRow(`SELECT deleted_at, deleted_by FROM nodes WHERE id = ?`, string(id)).Scan(&deletedAt, &deletedBy); err != nil {
		t.Fatal(err)
	}
	if deletedAt != at(t1).String() || deletedBy != string(undo.ChangeSet.ID) {
		t.Errorf("row: %s %s", deletedAt, deletedBy)
	}
	redo := mustCommit(t, db, set(model.Inverse(undo.ChangeSet.Ops[0])))
	if redo.ChangeSet.Ops[0].Kind != model.OpRestore {
		t.Errorf("redo kind %s", redo.ChangeSet.Ops[0].Kind)
	}

	// A merge's delete keeps merged_into through undo and redo.
	merge := kindOp(t, id, model.OpDelete, model.DeleteFields{Deleted: true, MergedInto: &survivor})
	merged := mustCommit(t, db, set(merge))
	mergedKey := fmt.Sprintf(`"merged_into":%q`, survivor)
	if !strings.Contains(string(merged.ChangeSet.Ops[0].After), mergedKey) {
		t.Errorf("merge after: %s", merged.ChangeSet.Ops[0].After)
	}
	undone := mustCommit(t, db, set(model.Inverse(merged.ChangeSet.Ops[0])))
	if !strings.Contains(string(undone.ChangeSet.Ops[0].Before), mergedKey) {
		t.Errorf("restore before: %s", undone.ChangeSet.Ops[0].Before)
	}
	again := mustCommit(t, db, set(model.Inverse(undone.ChangeSet.Ops[0])))
	if !strings.Contains(string(again.ChangeSet.Ops[0].After), mergedKey) {
		t.Errorf("redo after: %s", again.ChangeSet.Ops[0].After)
	}
}

func TestCommit_DeleteAndRestoreSubtree(t *testing.T) {
	db, root := commitDB(t)
	parent, child := ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, parent, root, "a1", "p"), insertOp(t, child, parent, "a0", "c")))
	mustCommit(t, db, set(deleteOp(t, child), deleteOp(t, parent)))
	if _, err := db.Commit(context.Background(), set(restoreOp(t, child)), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("restore under a trashed parent: %v", err)
	}
	mustCommit(t, db, set(restoreOp(t, parent), restoreOp(t, child)))
	if n := queryInt(t, db.sql, `SELECT count(*) FROM nodes WHERE deleted_at IS NOT NULL OR deleted_by IS NOT NULL`); n != 0 {
		t.Errorf("%d still deleted", n)
	}
	// A trashed node can move to a live parent before it is restored (D-017).
	mustCommit(t, db, set(deleteOp(t, child)))
	mustCommit(t, db, set(kindOp(t, child, model.OpMove, model.MoveFields{ParentID: root, Position: "a2"}), restoreOp(t, child)))
}

func TestCommit_DeleteKeepsItsNamedDeletion(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	a := ids.NewNode()
	mustCommit(t, db, set(insertOp(t, a, root, "a1", "a")))
	first := mustCommit(t, db, set(deleteOp(t, a)))
	restored := mustCommit(t, db, model.ChangeSet{At: *at(t1), Source: "ui", Ops: []model.Op{model.Inverse(first.ChangeSet.Ops[0])}})
	again := mustCommit(t, db, model.ChangeSet{At: *at(t1), Source: "ui", Ops: []model.Op{model.Inverse(restored.ChangeSet.Ops[0])}})

	d, err := db.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range d.Nodes {
		if n.ID == a && (n.DeletedBy != first.ChangeSet.ID || n.DeletedAt.String() != at(t0).String()) {
			t.Errorf("node: %+v, want deletion %s", n, first.ChangeSet.ID)
		}
	}
	after, err := again.ChangeSet.Ops[0].DecodeAfter()
	if err != nil {
		t.Fatal(err)
	}
	if f := after.(*model.DeleteFields); *f.DeletedBy != first.ChangeSet.ID {
		t.Errorf("recorded after: %+v", f)
	}

	half := kindOp(t, a, model.OpRestore, model.DeleteFields{})
	mustCommit(t, db, set(half))
	by := first.ChangeSet.ID
	bad := kindOp(t, a, model.OpDelete, model.DeleteFields{Deleted: true, DeletedBy: &by})
	if _, err := db.Commit(ctx, set(bad), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("half a deletion: %v", err)
	}
	nope := model.ChangeSetID("NOPE")
	bogus := kindOp(t, a, model.OpDelete, model.DeleteFields{Deleted: true, DeletedAt: at(t0), DeletedBy: &nope})
	if _, err := db.Commit(ctx, set(bogus), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad deleted_by: %v", err)
	}
}

func TestCommit_TrashedNodeMovesUnderTrashedParent(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	a, b, c := ids.NewNode(), ids.NewNode(), ids.NewNode()
	mustCommit(t, db, set(insertOp(t, a, root, "a1", "a"), insertOp(t, b, root, "a2", "b"), insertOp(t, c, root, "a3", "c")))
	mustCommit(t, db, set(deleteOp(t, a), deleteOp(t, b)))
	move := func(id, parent model.NodeID) model.ChangeSet {
		return set(kindOp(t, id, model.OpMove, model.MoveFields{ParentID: parent, Position: "a5"}))
	}
	mustCommit(t, db, move(b, a))
	if _, err := db.Commit(ctx, move(c, a), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("live node under trashed parent: %v", err)
	}
	if _, err := db.Commit(ctx, move(b, ids.NewNode()), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing parent: %v", err)
	}
}

func TestChangeSetByID(t *testing.T) {
	ctx := context.Background()
	db, root := commitDB(t)
	a := ids.NewNode()
	first := mustCommit(t, db, set(insertOp(t, a, root, "a1", "a")))
	cs := set(kindOp(t, a, model.OpEditText, model.TextFields{Text: "b"}), kindOp(t, a, model.OpEditPage, model.PageFields{Markdown: "p"}))
	cs.Description, cs.UndoOf = "two ops", first.ChangeSet.ID
	want := mustCommit(t, db, cs).ChangeSet

	got, err := db.ChangeSetByID(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "two ops" || got.UndoOf != first.ChangeSet.ID || got.Source != "ui" || got.At.String() != want.At.String() || len(got.Ops) != 2 {
		t.Fatalf("got %+v", got)
	}
	for i, op := range got.Ops {
		if op.NodeID != want.Ops[i].NodeID || op.Kind != want.Ops[i].Kind || string(op.Before) != string(want.Ops[i].Before) || string(op.After) != string(want.Ops[i].After) {
			t.Errorf("op %d: %+v, want %+v", i, op, want.Ops[i])
		}
	}
	if _, err := db.ChangeSetByID(ctx, ids.NewChangeSet()); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}
