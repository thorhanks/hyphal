package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain doubles as a helper process for cross-process tests: with
// STORE_TEST_HELPER=write it opens STORE_TEST_DB, writes one row and exits.
func TestMain(m *testing.M) {
	if os.Getenv("STORE_TEST_HELPER") == "write" {
		if err := helperWrite(os.Getenv("STORE_TEST_DB")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperWrite(path string) error {
	db, err := sql.Open(driverName, dsn(path, 5*time.Second))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO t (cs, v) VALUES (-1, 'from helper')`)
	return err
}

// openTestDB opens path with the store's pragmas and a table t where every
// commit writes ten rows that share one cs value.
func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dsn(path, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, cs INTEGER NOT NULL, v TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func commitTen(ctx context.Context, db *sql.DB, cs int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k := range 10 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO t (cs, v) VALUES (?, ?)`, cs, fmt.Sprintf("row %d.%d with some text", cs, k)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func queryInt(t *testing.T, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string) int64 {
	t.Helper()
	var v int64
	if err := q.QueryRowContext(context.Background(), query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func checkIntegrity(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open(driverName, dsn(path, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var got string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&got); err != nil || got != "ok" {
		t.Fatalf("integrity_check(%s) = %q, %v", path, got, err)
	}
}

func TestBackupWhileWriting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "outline.db")
	db := openTestDB(t, path)
	for cs := range 500 {
		if err := commitTen(ctx, db, cs); err != nil {
			t.Fatal(err)
		}
	}

	// A second pool writes continuously, as the CLI would while the TUI snapshots.
	writer := openTestDB(t, path)
	var commits atomic.Int64
	stop := make(chan struct{})
	done := make(chan error)
	go func() {
		for cs := 500; ; cs++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := commitTen(ctx, writer, cs); err != nil {
				done <- err
				return
			}
			commits.Add(1)
		}
	}()
	for commits.Load() < 3 {
		time.Sleep(time.Millisecond)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, "snap #1?.db") // URI-special characters
	err = backupTo(ctx, conn, snap)
	_ = conn.Close()
	close(stop)
	if werr := <-done; werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatal(err)
	}

	checkIntegrity(t, snap)
	s := openTestDB(t, snap)
	sets := queryInt(t, s, `SELECT count(DISTINCT cs) FROM t`)
	rows := queryInt(t, s, `SELECT count(*) FROM t`)
	if rows != sets*10 || sets < 503 {
		t.Fatalf("snapshot has %d commits, %d rows: not one consistent state", sets, rows)
	}
	if got := queryInt(t, writer, `SELECT count(*) FROM t`); got < rows {
		t.Fatalf("live db has %d rows, fewer than the snapshot's %d", got, rows)
	}
}

func TestRestoreIntoLiveDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "outline.db")
	db := openTestDB(t, path)
	for cs := range 100 {
		if err := commitTen(ctx, db, cs); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, "snap.db")
	if err := backupTo(ctx, conn, snap); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	// Diverge, and keep another connection open across the restore.
	if _, err := db.Exec(`DELETE FROM t WHERE cs >= 50`); err != nil {
		t.Fatal(err)
	}
	other := openTestDB(t, path)
	otherConn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = otherConn.Close() }()
	if got := queryInt(t, otherConn, `SELECT count(*) FROM t`); got != 500 {
		t.Fatalf("before restore: %d rows", got)
	}

	conn, err = db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := restoreFrom(ctx, conn, filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("restore from a missing file succeeded")
	}
	if err := restoreFrom(ctx, conn, snap); err != nil {
		t.Fatal(err)
	}

	if got := queryInt(t, otherConn, `SELECT count(*) FROM t`); got != 1000 {
		t.Fatalf("open connection sees %d rows after restore, want 1000", got)
	}
	var mode string
	if err := otherConn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode after restore = %q, %v", mode, err)
	}
	if err := commitTen(ctx, db, 1000); err != nil {
		t.Fatalf("write after restore: %v", err)
	}
	checkIntegrity(t, path)
}

func TestDataVersionAcrossProcesses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outline.db")
	db := openTestDB(t, path)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	dataVersion := func() int64 {
		var v int64
		if err := conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	v0 := dataVersion()
	if got := dataVersion(); got != v0 {
		t.Fatalf("data_version moved without a write: %d → %d", v0, got)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "STORE_TEST_HELPER=write", "STORE_TEST_DB="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	if got := dataVersion(); got == v0 {
		t.Fatalf("data_version stayed %d after another process wrote", v0)
	}
}

func TestFileURI(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/data/outline.db", "file:///data/outline.db"},
		{"/a b/x?#%.db", "file:///a%20b/x%3F%23%25.db"},
		{"rel/outline.db", "file:rel/outline.db"},
	}
	for _, c := range cases {
		if got := fileURI(c.path, url.Values{}); got != c.want {
			t.Errorf("fileURI(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
