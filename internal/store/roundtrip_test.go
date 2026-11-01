package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

const exportedBy = "test 1.0"

// writeFixture regenerates real-1.json. It isn't -update, since a new fixture has
// all new IDs and golden updates shouldn't churn it.
var writeFixture = flag.Bool("write-real-fixture", false, "regenerate testdata/outlines/real-1.json")

var exportClock = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// openStore opens a new database with a fixed clock, so that exports compare by
// bytes. A property test closes it per case, to stay under the file limit.
func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "outline.db"), store.OpenOptions{Create: true, Clock: func() time.Time { return exportClock }})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func closeStore(t *testing.T, db *store.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func export(t *testing.T, db *store.DB, history bool) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := db.ExportJSON(context.Background(), &out, history, exportedBy); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func rows(t *testing.T, db *store.DB) map[string][]string {
	t.Helper()
	r, err := db.RowsForTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// fataler is what testing.T and rapid.T share.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func compareRows(t fataler, want, got map[string][]string, tables ...string) {
	t.Helper()
	for _, table := range tables {
		w, g := want[table], got[table]
		if len(w) != len(g) {
			t.Fatalf("%s: %d rows, want %d", table, len(g), len(w))
		}
		for i := range w {
			if w[i] != g[i] {
				t.Fatalf("%s row %d:\n got %s\nwant %s", table, i, g[i], w[i])
			}
		}
	}
}

// randomHistory fills db through the real planners, with undos and redos so that
// history holds undo_of and repeated deletions, plus view and UI state.
func randomHistory(t *testing.T, db *store.DB, seed uint64, actions int) {
	t.Helper()
	ctx := context.Background()
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(seed, 14))
	for range actions {
		var err error
		switch r.IntN(12) {
		case 0:
			_, err = o.Undo(ctx)
		case 1:
			_, err = o.Redo(ctx)
		default:
			_, p := testgen.Action(r, o.Ctx())
			_, err = o.Do(ctx, p)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range o.Tree().AllNodes() {
		if i%4 == 0 {
			if err := db.ExecForTest(`INSERT INTO view_state (node_id, collapsed) VALUES (?, ?)`, string(id), i%8 == 0); err != nil {
				t.Fatal(err)
			}
		}
		if i%9 == 0 {
			key, value := fmt.Sprintf("cursor:%s", id), fmt.Sprintf(`{"node":%q,"offset":%d,"note":"<日本語> & é"}`, id, i)
			if err := db.ExecForTest(`INSERT INTO ui_state (key, value) VALUES (?, ?)`, key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.ExecForTest(`INSERT INTO ui_state (key, value) VALUES ('show_completed', 'false'), ('palette_recent', '["a","b"]')`); err != nil {
		t.Fatal(err)
	}
}

// casesInCI runs this test's rapid.Check 200 times (P0-14), unless -rapid.checks
// was set on the command line.
func casesInCI(t *testing.T) {
	t.Helper()
	f := flag.Lookup("rapid.checks")
	if f == nil {
		t.Fatal("rapid has no checks flag")
	}
	explicit := false
	flag.Visit(func(v *flag.Flag) { explicit = explicit || v.Name == f.Name })
	if explicit {
		return
	}
	old := f.Value.String()
	if err := f.Value.Set("200"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Value.Set(old) })
}

func TestJSONRoundTrip_Property(t *testing.T) {
	casesInCI(t)
	ctx := context.Background()
	rapid.Check(t, func(rt *rapid.T) {
		seed := rapid.Uint64().Draw(rt, "seed")
		actions := rapid.IntRange(0, 40).Draw(rt, "actions")
		src := openStore(t)
		defer closeStore(t, src)
		randomHistory(t, src, seed, actions)
		first := export(t, src, true)

		dst := openStore(t)
		defer closeStore(t, dst)
		if err := dst.ImportJSON(ctx, bytes.NewReader(first), store.ImportEmpty); err != nil {
			rt.Fatal(err)
		}
		if second := export(t, dst, true); !bytes.Equal(first, second) {
			rt.Fatalf("exports differ:\n%s\n---\n%s", first, second)
		}
		compareRows(rt, rows(t, src), rows(t, dst), "meta", "nodes", "pages", "stars", "view_state", "ui_state", "change_sets", "ops")
	})
}

func TestJSONRoundTrip_NoHistory(t *testing.T) {
	ctx := context.Background()
	rapid.Check(t, func(rt *rapid.T) {
		seed := rapid.Uint64().Draw(rt, "seed")
		actions := rapid.IntRange(0, 40).Draw(rt, "actions")
		src := openStore(t)
		defer closeStore(t, src)
		randomHistory(t, src, seed, actions)
		first := export(t, src, false)

		dst := openStore(t)
		defer closeStore(t, dst)
		if err := dst.ImportJSON(ctx, bytes.NewReader(first), store.ImportEmpty); err != nil {
			rt.Fatal(err)
		}
		if second := export(t, dst, false); !bytes.Equal(first, second) {
			rt.Fatalf("exports differ:\n%s\n---\n%s", first, second)
		}
		got := rows(t, dst)
		compareRows(rt, rows(t, src), got, "meta", "nodes", "pages", "stars", "view_state", "ui_state")
		if len(got["change_sets"]) != 0 || len(got["ops"]) != 0 {
			rt.Fatalf("history came along: %d change sets, %d ops", len(got["change_sets"]), len(got["ops"]))
		}
	})
}

func TestJSONRoundTrip_RealFixture(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join("testdata", "outlines", "real-1.json")
	if *writeFixture {
		writeRealFixture(t, path)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	db := openStore(t)
	defer closeStore(t, db)
	if err := db.ImportJSON(ctx, bytes.NewReader(file), store.ImportEmpty); err != nil {
		t.Fatal(err)
	}
	if got := export(t, db, false); !bytes.Equal(file, got) {
		t.Error("the export differs from the fixture")
	}

	// A second copy holds the same rows, and the result is a sound outline.
	again := openStore(t)
	defer closeStore(t, again)
	if err := again.ImportJSON(ctx, bytes.NewReader(file), store.ImportEmpty); err != nil {
		t.Fatal(err)
	}
	r := rows(t, db)
	compareRows(t, r, rows(t, again), "meta", "nodes", "pages", "stars", "view_state", "ui_state")
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	if p := o.Tree().Check(); len(p) != 0 {
		t.Errorf("invariants: %v", p)
	}

	// The fixture really holds what the card asks for.
	var content struct {
		Nodes []struct {
			Text      string  `json:"text"`
			DeletedAt *string `json:"deleted_at"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(file, &content); err != nil {
		t.Fatal(err)
	}
	trashed, wide := 0, 0
	for _, n := range content.Nodes {
		if n.DeletedAt != nil {
			trashed++
		}
		if len(n.Text) != len([]rune(n.Text)) {
			wide++
		}
	}
	if len(content.Nodes) < 2000 || trashed == 0 || wide == 0 || len(r["pages"]) == 0 || len(r["stars"]) == 0 {
		t.Errorf("fixture: %d nodes, %d trashed, %d with non-ASCII text, %d pages, %d stars",
			len(content.Nodes), trashed, wide, len(r["pages"]), len(r["stars"]))
	}
}

func writeRealFixture(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	db := openStore(t)
	defer closeStore(t, db)
	now := exportClock.Add(-90 * 24 * time.Hour)
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true, Clock: func() time.Time {
		now = now.Add(37 * time.Second)
		return now
	}})
	if err != nil {
		t.Fatal(err)
	}
	testgen.RealOutline(ctx, t, rand.New(rand.NewPCG(2026, 11)), o, 2000)
	first := o.Tree().Children(o.Tree().Root())[0]
	if err := db.ExecForTest(`INSERT INTO view_state (node_id, collapsed) VALUES (?, 1)`, string(first)); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecForTest(`INSERT INTO ui_state (key, value) VALUES ('show_completed', 'true')`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, export(t, db, false), 0o644); err != nil {
		t.Fatal(err)
	}
}
