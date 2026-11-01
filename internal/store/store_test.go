package store

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

func openNew(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(path, OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setVersion(t *testing.T, path string, version int) {
	t.Helper()
	conn := openTestDB(t, path)
	defer func() { _ = conn.Close() }()
	if _, err := conn.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, fmt.Sprint(version)); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_CreatesSchema(t *testing.T) {
	ctx := context.Background()
	db := openNew(t, filepath.Join(t.TempDir(), "outline.db"))

	rows, err := db.sql.Query(`SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got strings.Builder
	for rows.Next() {
		var typ, name, tbl, ddl string
		if err := rows.Scan(&typ, &name, &tbl, &ddl); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&got, "-- %s %s on %s\n%s\n\n", typ, name, tbl, ddl)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "golden", "TestOpen_CreatesSchema.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Errorf("sqlite_master differs from %s:\n%s", golden, got.String())
	}

	var keys []string
	keyRows, err := db.sql.Query(`SELECT key FROM meta ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = keyRows.Close() }()
	for keyRows.Next() {
		var k string
		if err := keyRows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if want := []string{"description", "format_id", "root_id", "schema_version"}; !slices.Equal(keys, want) {
		t.Errorf("meta keys %v, want %v", keys, want)
	}
	for key, want := range map[string]string{"schema_version": "1", "format_id": "tree-outline"} {
		if v, _, _ := db.Meta(ctx, key); v != want {
			t.Errorf("%s = %q, want %q", key, v, want)
		}
	}
	if v, _, _ := db.Meta(ctx, "description"); strings.Contains(strings.ToLower(v), "hyphal") || len(v) < 80 {
		t.Errorf("description: %q", v)
	}

	rootID, ok, err := db.Meta(ctx, "root_id")
	if err != nil || !ok || !ids.Valid(rootID) {
		t.Fatalf("root_id %q, %v, %v", rootID, ok, err)
	}
	var parent sql.NullString
	var position, text, created string
	var version int
	if err := db.sql.QueryRow(`SELECT parent_id, position, text, created_at, version FROM nodes WHERE id = ?`, rootID).
		Scan(&parent, &position, &text, &created, &version); err != nil {
		t.Fatal(err)
	}
	if parent.Valid || position != "a0" || text != "" || version != 1 {
		t.Errorf("root: parent %v, position %q, text %q, version %d", parent, position, text, version)
	}
	if _, err := model.ParseTime(created); err != nil {
		t.Error(err)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM nodes`); n != 1 {
		t.Errorf("%d nodes", n)
	}
}

func TestOpen_ReopenKeepsRoot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outline.db")
	db := openNew(t, path)
	first, _, _ := db.Meta(ctx, "root_id")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openNew(t, path)
	if again, _, _ := db.Meta(ctx, "root_id"); again != first {
		t.Errorf("root changed from %s to %s", first, again)
	}
}

func TestOpen_Permissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "data")
	path := filepath.Join(dir, "outline.db")
	old := setUmask(0)
	defer setUmask(old)
	db := openNew(t, path)
	if _, err := db.sql.Exec(`INSERT INTO ui_state (key, value) VALUES ('zoom', '"x"')`); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode %o", got)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode %o", filepath.Base(name), got)
		}
	}
}

func TestOpen_Pragmas(t *testing.T) {
	ctx := context.Background()
	db := openNew(t, filepath.Join(t.TempDir(), "outline.db"))
	// Holding several connections at once forces the pool to open new ones.
	for range 4 {
		conn, err := db.sql.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		var mode string
		if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
			t.Errorf("journal_mode %q, %v", mode, err)
		}
		if got := queryInt(t, conn, `PRAGMA foreign_keys`); got != 1 {
			t.Errorf("foreign_keys %d", got)
		}
		if got := queryInt(t, conn, `PRAGMA synchronous`); got != 2 {
			t.Errorf("synchronous %d", got)
		}
		if got := queryInt(t, conn, `PRAGMA busy_timeout`); got != defaultBusyTimeout.Milliseconds() {
			t.Errorf("busy_timeout %d", got)
		}
	}
}

func TestOpen_NewerSchemaReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline.db")
	if err := openNew(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	setVersion(t, path, 99)

	db, err := Open(path, OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if db.ReadOnly() != ReadOnlyNewerSchema {
		t.Errorf("reason %d", db.ReadOnly())
	}
	if _, err := db.Commit(context.Background(), model.ChangeSet{}, nil, nil); !errors.Is(err, ErrReadOnly) {
		t.Errorf("commit: %v", err)
	}
	if v, _, err := db.Meta(context.Background(), "schema_version"); err != nil || v != "99" {
		t.Errorf("schema_version %q, %v", v, err)
	}
	if _, err := db.sql.Exec(`INSERT INTO ui_state (key, value) VALUES ('zoom', '"x"')`); err == nil {
		t.Error("SQLite accepted a write")
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(path), "snapshots"))
	if err == nil && len(entries) > 0 {
		t.Errorf("took a snapshot: %v", entries)
	}
}

func TestOpen_ForcedReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline.db")
	if _, err := Open(path, OpenOptions{Create: true, ReadOnly: true}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("read-only open created the file")
	}
	if err := openNew(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path, OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if db.ReadOnly() != ReadOnlyForced {
		t.Errorf("reason %d", db.ReadOnly())
	}
	if _, err := db.Commit(context.Background(), model.ChangeSet{}, nil, nil); !errors.Is(err, ErrReadOnly) {
		t.Errorf("commit: %v", err)
	}
}

func TestOpen_MissingWithoutCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.db")
	if _, err := Open(path, OpenOptions{}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("got %v", err)
	}
}

func TestOpen_LeavesOtherFilesAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	conn := openTestDB(t, path) // holds a table t and no meta
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, OpenOptions{Create: true}); !errors.Is(err, ErrNotOutline) {
		t.Errorf("got %v", err)
	}

	text := filepath.Join(t.TempDir(), "notes.db")
	content := []byte(strings.Repeat("not a database\n", 100))
	if err := os.WriteFile(text, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(text, OpenOptions{Create: true}); err == nil {
		t.Error("opened a text file")
	}
	if got, _ := os.ReadFile(text); string(got) != string(content) {
		t.Error("text file changed")
	}
}

func withMigrations(t *testing.T, ms ...Migration) {
	t.Helper()
	old := migrations
	migrations = ms
	t.Cleanup(func() { migrations = old })
}

func TestMigrate_SnapshotFirst(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "outline.db")
	if err := openNew(t, path).Close(); err != nil {
		t.Fatal(err)
	}

	runs := 0
	snapshotVersion := ""
	withMigrations(t, Migration{From: 1, To: 2, SQL: `CREATE TABLE added (id INTEGER PRIMARY KEY)`, Go: func(*sql.Tx) error {
		runs++
		// The snapshot already exists, and holds the old schema.
		files, err := filepath.Glob(filepath.Join(dir, "snapshots", "*"))
		if err != nil || len(files) != 1 {
			return fmt.Errorf("snapshots at migration time: %v, %w", files, err)
		}
		info, err := os.Stat(files[0])
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("snapshot mode %o", info.Mode().Perm())
		}
		if !strings.HasPrefix(filepath.Base(files[0]), "outline-20261101T120000Z-pre-migration") {
			return fmt.Errorf("snapshot name %s", files[0])
		}
		snap, err := sql.Open(driverName, fileURI(files[0], nil))
		if err != nil {
			return err
		}
		defer func() { _ = snap.Close() }()
		return snap.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&snapshotVersion)
	}})
	clock := func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }

	for range 2 { // the second open must not migrate again
		db, err := Open(path, OpenOptions{Clock: clock})
		if err != nil {
			t.Fatal(err)
		}
		if v, _, _ := db.Meta(ctx, "schema_version"); v != "2" {
			t.Errorf("schema_version %q", v)
		}
		if n := queryInt(t, db.sql, `SELECT count(*) FROM added`); n != 0 {
			t.Errorf("added holds %d rows", n)
		}
		if db.ReadOnly() != ReadOnlyNone {
			t.Errorf("reason %d", db.ReadOnly())
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Errorf("ran %d times", runs)
	}
	if snapshotVersion != "1" {
		t.Errorf("snapshot holds schema %q", snapshotVersion)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "snapshots", "*"))
	if len(files) != 1 {
		t.Errorf("snapshots: %v", files)
	}
}

func TestMigrate_FailureLeavesVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outline.db")
	if err := openNew(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	withMigrations(t, Migration{From: 1, To: 2, SQL: `CREATE TABLE added (id INTEGER PRIMARY KEY)`, Go: func(*sql.Tx) error {
		return errors.New("boom")
	}})
	if _, err := Open(path, OpenOptions{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	withMigrations(t)
	db := openNew(t, path)
	if v, _, _ := db.Meta(ctx, "schema_version"); v != "1" {
		t.Errorf("schema_version %q", v)
	}
	if n := queryInt(t, db.sql, `SELECT count(*) FROM sqlite_master WHERE name = 'added'`); n != 0 {
		t.Error("the failed migration left a table")
	}
}

func TestMigrate_ReadOnlyRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline.db")
	if err := openNew(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	withMigrations(t, Migration{From: 1, To: 2})
	if _, err := Open(path, OpenOptions{ReadOnly: true}); !errors.Is(err, ErrNeedsMigration) {
		t.Errorf("got %v", err)
	}
}

// TestOrderMatchesSQLite checks fracindex order against ORDER BY position on
// the real column, with 10,000 random keys (P0-03 AC5).
func TestOrderMatchesSQLite(t *testing.T) {
	db := openNew(t, filepath.Join(t.TempDir(), "outline.db"))
	r := rand.New(rand.NewPCG(3, 4))
	first, err := fracindex.Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{first}
	for len(keys) < 10_000 {
		i := r.IntN(len(keys) + 1)
		var a, b string
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		k, err := fracindex.Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		keys = slices.Insert(keys, i, k)
	}
	rootID, _, err := db.Meta(context.Background(), "root_id")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, i := range r.Perm(len(keys)) {
		if _, err := tx.Exec(`INSERT INTO nodes (id, parent_id, position, text, created_at, updated_at) VALUES (?, ?, ?, '', '', '')`,
			string(ids.NewNode()), rootID, keys[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.sql.Query(`SELECT position FROM nodes WHERE parent_id = ? ORDER BY position`, rootID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		got = append(got, k)
	}
	if !slices.Equal(got, keys) {
		t.Error("ORDER BY position differs from fracindex order")
	}
}
