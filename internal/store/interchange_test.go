package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/ids"
)

const (
	fixRoot = "r00t00000000"
	fixA    = "aaaaaaaaaaaa"
	fixB    = "bbbbbbbbbbbb"
	fixC    = "cccccccccccc"
	fixSet1 = "s00000000001"
	fixSet2 = "s00000000002"
)

func fixedClock() time.Time { return t1 }

// fixtureDB writes rows directly, so every ID and timestamp is fixed and the
// export is the same bytes each run.
func fixtureDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "outline.db"), OpenOptions{Create: true, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const ts, ts2 = "2026-11-01T12:00:00.000Z", "2026-11-01T13:00:00.000Z"
	for _, s := range []struct {
		query string
		args  []any
	}{
		{`UPDATE nodes SET id = ?, created_at = ?, updated_at = ?`, []any{fixRoot, ts, ts}},
		{`UPDATE meta SET value = ? WHERE key = 'root_id'`, []any{fixRoot}},
		{`INSERT INTO meta (key, value) VALUES ('inbox_id', ?), ('ui_opened', '1'), ('last_snapshot_at', ?)`, []any{fixC, ts}},
		{`INSERT INTO nodes (id, parent_id, position, text, is_task, is_heading, completed_at, created_at, updated_at, version, deleted_at, deleted_by)
			VALUES (?, ?, 'a0', '<red>Alpha</red> & **bold**', 1, 0, ?, ?, ?, 3, NULL, NULL)`, []any{fixA, fixRoot, ts2, ts, ts2}},
		{`INSERT INTO nodes (id, parent_id, position, text, is_task, is_heading, completed_at, created_at, updated_at, version, deleted_at, deleted_by)
			VALUES (?, ?, 'a1', 'trashed', 0, 0, NULL, ?, ?, 2, ?, ?)`, []any{fixB, fixRoot, ts, ts, ts2, fixSet2}},
		{`INSERT INTO nodes (id, parent_id, position, text, is_task, is_heading, completed_at, created_at, updated_at, version, deleted_at, deleted_by)
			VALUES (?, ?, 'a0', '日本語 child', 0, 1, NULL, ?, ?, 1, NULL, NULL)`, []any{fixC, fixA, ts, ts}},
		{`INSERT INTO pages (node_id, markdown, updated_at, version) VALUES (?, ?, ?, 2)`, []any{fixA, "# Page\n\nBody\n", ts2}},
		{`INSERT INTO stars (node_id, starred_at) VALUES (?, ?)`, []any{fixA, ts2}},
		{`INSERT INTO view_state (node_id, collapsed) VALUES (?, 1), (?, 0)`, []any{fixA, fixRoot}},
		{`INSERT INTO ui_state (key, value) VALUES ('zoom', ?), ('cursor:x', ?)`, []any{`"` + fixA + `"`, `{"node":"` + fixA + `","offset":3}`}},
		{`INSERT INTO change_sets (id, at, source, description, undo_of) VALUES (?, ?, 'ui', 'Added a page', NULL), (?, ?, 'cli', NULL, ?)`,
			[]any{fixSet1, ts, fixSet2, ts2, fixSet1}},
		{`INSERT INTO ops (id, change_set_id, node_id, kind, before, after) VALUES (1, ?, ?, 'edit_page', NULL, ?), (2, ?, ?, 'delete', ?, ?)`,
			[]any{
				fixSet1, fixA, `{"markdown":"<b>"}`,
				fixSet2, fixB, `{"deleted":false,"deleted_at":null,"deleted_by":null}`,
				`{"deleted":true,"deleted_at":"` + ts2 + `","deleted_by":"` + fixSet2 + `"}`,
			}},
	} {
		if _, err := db.sql.Exec(s.query, s.args...); err != nil {
			t.Fatalf("%s: %v", s.query, err)
		}
	}
	return db
}

func exportBytes(t *testing.T, db *DB, history bool) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := db.ExportJSON(context.Background(), &out, history, "test 1.0"); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func newImportDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "outline.db"), OpenOptions{Create: true, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestExportJSON_Golden(t *testing.T) {
	got := exportBytes(t, fixtureDB(t), true)
	golden := filepath.Join("testdata", "golden", "TestExportJSON_Golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("export differs from %s:\n%s", golden, got)
	}
}

func TestExportJSON_History(t *testing.T) {
	db := fixtureDB(t)
	keys := func(data []byte) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if _, ok := keys(exportBytes(t, db, false))["history"]; ok {
		t.Error("history present without asking")
	}
	h, ok := keys(exportBytes(t, db, true))["history"]
	if !ok {
		t.Fatal("history absent")
	}
	if !strings.Contains(string(h), fixSet2) {
		t.Errorf("history: %s", h)
	}
}

func TestExportJSON_EmptyHistoryIsNotNull(t *testing.T) {
	out := string(exportBytes(t, newImportDB(t), true))
	for _, want := range []string{`"pages": []`, `"stars": []`, `"view_state": []`, `"ui_state": []`, `"change_sets": []`, `"ops": []`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in:\n%s", want, out)
		}
	}
}

// TestExportJSON_MetaExcludesLocalKeys covers CONTRACTS §6: no schema_version,
// description or last_* in meta.
func TestExportJSON_MetaExcludesLocalKeys(t *testing.T) {
	var f exportFile
	if err := json.Unmarshal(exportBytes(t, fixtureDB(t), false), &f); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "description", "last_snapshot_at"} {
		if _, ok := f.Meta[key]; ok {
			t.Errorf("meta holds %s", key)
		}
	}
	for _, key := range []string{"root_id", "inbox_id", "ui_opened", "format_id"} {
		if _, ok := f.Meta[key]; !ok {
			t.Errorf("meta lacks %s", key)
		}
	}
}

func TestImportJSON_Modes(t *testing.T) {
	ctx := context.Background()
	src := exportBytes(t, fixtureDB(t), true)

	t.Run("empty database accepts, and the export is identical", func(t *testing.T) {
		db := newImportDB(t)
		if err := db.ImportJSON(ctx, bytes.NewReader(src), ImportEmpty); err != nil {
			t.Fatal(err)
		}
		if got := exportBytes(t, db, true); !bytes.Equal(got, src) {
			t.Errorf("round trip differs:\n%s", got)
		}
	})

	t.Run("non-empty database refuses", func(t *testing.T) {
		db, root := commitDB(t)
		mustCommit(t, db, set(insertOp(t, ids.NewNode(), root, "a1", "mine")))
		err := db.ImportJSON(ctx, bytes.NewReader(src), ImportEmpty)
		if !errors.Is(err, ErrNotEmpty) || !strings.Contains(err.Error(), "--replace") {
			t.Fatalf("got %v", err)
		}
		d, err := db.LoadAll(ctx)
		if err != nil || len(d.Nodes) != 2 {
			t.Errorf("database changed: %+v, %v", d, err)
		}
	})

	t.Run("a purged database still counts as non-empty", func(t *testing.T) {
		db := newImportDB(t)
		if _, err := db.sql.Exec(`INSERT INTO change_sets (id, at, source) VALUES (?, '2026-11-01T12:00:00.000Z', 'ui')`, fixSet1); err != nil {
			t.Fatal(err)
		}
		if err := db.ImportJSON(ctx, bytes.NewReader(src), ImportEmpty); !errors.Is(err, ErrNotEmpty) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("replace snapshots, then replaces", func(t *testing.T) {
		db, root := commitDB(t)
		mine := ids.NewNode()
		mustCommit(t, db, set(insertOp(t, mine, root, "a1", "mine")))
		if err := func() error {
			_, err := db.sql.Exec(`INSERT INTO meta (key, value) VALUES ('last_export_at', 'kept')`)
			return err
		}(); err != nil {
			t.Fatal(err)
		}
		if err := db.ImportJSON(ctx, bytes.NewReader(src), ImportReplace); err != nil {
			t.Fatal(err)
		}
		d, err := db.LoadAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Nodes) != 4 || d.Meta["root_id"] != fixRoot {
			t.Errorf("content not replaced: %+v", d.Meta)
		}
		for _, n := range d.Nodes {
			if n.ID == mine {
				t.Error("old node survived")
			}
		}
		if d.Meta["last_export_at"] != "kept" || d.Meta["schema_version"] != "1" || d.Meta["description"] == "" {
			t.Errorf("local meta lost: %+v", d.Meta)
		}

		found, err := filepath.Glob(filepath.Join(filepath.Dir(db.Path()), "snapshots", "*-pre-import.db"))
		if err != nil || len(found) != 1 {
			t.Fatalf("snapshots: %v, %v", found, err)
		}
		info, err := os.Stat(found[0])
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("snapshot mode: %v, %v", info, err)
		}
		snap, err := Open(found[0], OpenOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = snap.Close() }()
		old, err := snap.LoadAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(old.Nodes) != 2 || old.Meta["root_id"] != string(root) {
			t.Errorf("snapshot isn't the old database: %+v", old.Meta)
		}
	})

	t.Run("without history, history is emptied", func(t *testing.T) {
		db := fixtureDB(t)
		noHistory := exportBytes(t, db, false)
		if err := db.ImportJSON(ctx, bytes.NewReader(noHistory), ImportReplace); err != nil {
			t.Fatal(err)
		}
		var sets int
		if err := db.sql.QueryRow(`SELECT count(*) FROM change_sets`).Scan(&sets); err != nil || sets != 0 {
			t.Errorf("%d change sets, %v", sets, err)
		}
	})

	t.Run("read-only refuses", func(t *testing.T) {
		db := fixtureDB(t)
		ro, err := Open(db.Path(), OpenOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ro.Close() }()
		if err := ro.ImportJSON(ctx, bytes.NewReader(src), ImportReplace); !errors.Is(err, ErrReadOnly) {
			t.Errorf("got %v", err)
		}
	})
}

// A read-only database still exports, since that is how you rescue one (§7.4).
func TestExportJSON_ReadOnly(t *testing.T) {
	db := fixtureDB(t)
	ro, err := Open(db.Path(), OpenOptions{ReadOnly: true, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if got := exportBytes(t, ro, true); !bytes.Equal(got, exportBytes(t, db, true)) {
		t.Error("read-only export differs")
	}
}

func TestImportJSON_Invalid(t *testing.T) {
	ctx := context.Background()
	str := func(s string) *string { return &s }
	node := func(f *exportFile, id string) *exportNode {
		for i := range f.Nodes {
			if f.Nodes[i].ID == id {
				return &f.Nodes[i]
			}
		}
		t.Fatalf("no node %s", id)
		return nil
	}
	cases := []struct {
		name   string
		mutate func(*exportFile)
		want   string
	}{
		{"unknown format", func(f *exportFile) { f.Format = "other" }, "format"},
		{"unknown format version", func(f *exportFile) { f.FormatVersion = 99 }, "format_version"},
		{"unknown schema version", func(f *exportFile) { f.SchemaVersion = 99 }, "schema_version"},
		{"wrong format_id", func(f *exportFile) { f.Meta["format_id"] = "other" }, "format_id"},
		{"no root_id", func(f *exportFile) { delete(f.Meta, "root_id") }, "root_id"},
		{"two roots", func(f *exportFile) { node(f, fixA).ParentID = nil }, "root_id"},
		{"no root", func(f *exportFile) { node(f, fixRoot).ParentID = str(fixA) }, "no parent"},
		{"root with text", func(f *exportFile) { node(f, fixRoot).Text = "Home" }, "root"},
		{"missing parent", func(f *exportFile) { node(f, fixC).ParentID = str("zzzzzzzzzzzz") }, "zzzzzzzzzzzz"},
		{"cycle", func(f *exportFile) {
			node(f, fixA).ParentID = str(fixC)
		}, "cycle"},
		{"live under trashed", func(f *exportFile) { node(f, fixC).ParentID = str(fixB) }, "trashed"},
		{"page without node", func(f *exportFile) { f.Pages[0].NodeID = "zzzzzzzzzzzz" }, "pages"},
		{"page on root", func(f *exportFile) { f.Pages[0].NodeID = fixRoot }, "root"},
		{"star on root", func(f *exportFile) { f.Stars[0].NodeID = fixRoot }, "root"},
		{"view state without node", func(f *exportFile) { f.ViewState[0].NodeID = "zzzzzzzzzzzz" }, "view_state"},
		{"line break in text", func(f *exportFile) { node(f, fixA).Text = "a\nb" }, "line break"},
		{"leading marker", func(f *exportFile) { node(f, fixA).Text = "## a" }, "marker"},
		{"bad ID", func(f *exportFile) { node(f, fixC).ID = "NOPE" }, "NOPE"},
		{"bad position", func(f *exportFile) { node(f, fixA).Position = "!" }, "position"},
		{"deleted_by without deleted_at", func(f *exportFile) { node(f, fixB).DeletedAt = nil }, "disagree"},
		{"inbox missing", func(f *exportFile) { f.Meta["inbox_id"] = "zzzzzzzzzzzz" }, "inbox_id"},
		{"bad timestamp", func(f *exportFile) { node(f, fixA).CreatedAt = "2026-11-01" }, "timestamp"},
		{"local timestamp", func(f *exportFile) { node(f, fixA).CreatedAt = "2026-11-01T05:00:00-07:00" }, "timestamp"},
		{"version zero", func(f *exportFile) { node(f, fixA).Version = 0 }, "version"},
		{"duplicate node", func(f *exportFile) { f.Nodes = append(f.Nodes, f.Nodes[0]) }, "twice"},
		{"duplicate page", func(f *exportFile) { f.Pages = append(f.Pages, f.Pages[0]) }, "twice"},
		{"duplicate ui key", func(f *exportFile) { f.UIState = append(f.UIState, f.UIState[0]) }, "twice"},
		{"op in missing change set", func(f *exportFile) { f.History.Ops[0].ChangeSetID = "zzzzzzzzzzzz" }, "change set"},
		{"bad source", func(f *exportFile) { f.History.ChangeSets[0].Source = "nope" }, "source"},
		{"duplicate op", func(f *exportFile) { f.History.Ops[1].ID = f.History.Ops[0].ID }, "twice"},
		{"bad op kind", func(f *exportFile) { f.History.Ops[0].Kind = "nope" }, "op 1"},
		{"bad op payload", func(f *exportFile) { f.History.Ops[0].After = json.RawMessage(`{"x":1}`) }, "op 1"},
	}
	db := fixtureDB(t)
	before := exportBytes(t, db, true)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var f exportFile
			if err := json.Unmarshal(before, &f); err != nil {
				t.Fatal(err)
			}
			c.mutate(&f)
			data, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			err = db.ImportJSON(ctx, bytes.NewReader(data), ImportReplace)
			if !errors.Is(err, ErrParse) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want ErrParse naming %q", err, c.want)
			}
			if after := exportBytes(t, db, true); !bytes.Equal(after, before) {
				t.Error("database changed")
			}
		})
	}
	for name, data := range map[string]string{
		"not JSON":      `{`,
		"trailing data": string(before) + `{}`,
		"unknown field": strings.Replace(string(before), `"format": `, `"extra": 1, "format": `, 1),
		"ui value missing": strings.Replace(string(before), `"key": "zoom",
      "value": "`+fixA+`"`, `"key": "zoom"`, 1),
		"op before missing": strings.Replace(string(before), `"before": null,`, ``, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if data == string(before) {
				t.Fatal("the test's replacement matched nothing")
			}
			if err := db.ImportJSON(ctx, strings.NewReader(data), ImportReplace); !errors.Is(err, ErrParse) {
				t.Fatalf("got %v", err)
			}
		})
	}
	found, err := filepath.Glob(filepath.Join(filepath.Dir(db.Path()), "snapshots", "*"))
	if err != nil || len(found) != 0 {
		t.Errorf("an invalid file took a snapshot: %v, %v", found, err)
	}
}
