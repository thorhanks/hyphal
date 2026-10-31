package driverspike

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain doubles as the helper process for the cross-process checks.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SPIKE_HELPER"); mode != "" {
		if err := helper(mode, os.Getenv("SPIKE_DRIVER"), os.Getenv("SPIKE_DB"), os.Getenv("SPIKE_ARG")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func driverByName(name string) Driver {
	for _, d := range Drivers {
		if d.Name == name {
			return d
		}
	}
	panic("unknown driver " + name)
}

func helper(mode, driver, path, arg string) error {
	d := driverByName(driver)
	db, err := d.Open(path, 5000)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	switch mode {
	case "write":
		_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO meta (key, value) VALUES ('from_helper', ?)`, arg)
		return err
	case "hold":
		ms, _ := strconv.Atoi(arg)
		tx, err := db.BeginTx(ctx, nil) // _txlock=immediate takes the write lock now
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO meta (key, value) VALUES ('held', '1')`); err != nil {
			return err
		}
		fmt.Println("locked")
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return tx.Commit()
	}
	return fmt.Errorf("unknown helper mode %q", mode)
}

func runHelper(t *testing.T, mode string, d Driver, path, arg string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SPIKE_HELPER="+mode, "SPIKE_DRIVER="+d.Name, "SPIKE_DB="+path, "SPIKE_ARG="+arg)
	cmd.Stderr = os.Stderr
	return cmd
}

func forEachDriver(t *testing.T, f func(t *testing.T, d Driver)) {
	for _, d := range Drivers {
		t.Run(d.Name, func(t *testing.T) { f(t, d) })
	}
}

func openSeeded(t testing.TB, d Driver, dir string, n int) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(dir, "outline.db")
	db, err := d.Open(path, 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Seed(context.Background(), db, n); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func scalar[T any](t testing.TB, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string) T {
	t.Helper()
	var v T
	if err := q.QueryRowContext(context.Background(), query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func integrityOK(t *testing.T, d Driver, path string) {
	t.Helper()
	db, err := d.Open(path, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if got := scalar[string](t, db, `PRAGMA integrity_check`); got != "ok" {
		t.Fatalf("integrity_check(%s) = %q", path, got)
	}
}

func TestPragmasOnEveryConnection(t *testing.T) {
	forEachDriver(t, func(t *testing.T, d Driver) {
		db, _ := openSeeded(t, d, t.TempDir(), 10)
		ctx := context.Background()
		conns := make([]*sql.Conn, 4)
		for i := range conns {
			c, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			conns[i] = c
		}
		wantFull := 0
		if runtime.GOOS == "darwin" {
			wantFull = 1
		}
		for i, c := range conns {
			if got := scalar[string](t, c, `PRAGMA journal_mode`); got != "wal" {
				t.Errorf("conn %d journal_mode = %q", i, got)
			}
			if got := scalar[int](t, c, `PRAGMA synchronous`); got != 2 {
				t.Errorf("conn %d synchronous = %d", i, got)
			}
			if got := scalar[int](t, c, `PRAGMA foreign_keys`); got != 1 {
				t.Errorf("conn %d foreign_keys = %d", i, got)
			}
			if got := scalar[int](t, c, `PRAGMA fullfsync`); got != wantFull {
				t.Errorf("conn %d fullfsync = %d, want %d", i, got, wantFull)
			}
			if got := scalar[int](t, c, `PRAGMA busy_timeout`); got != 5000 {
				t.Errorf("conn %d busy_timeout = %d", i, got)
			}
		}
	})
}

// §8.5: back up a live WAL database while another connection keeps writing.
func TestBackupWhileWriting(t *testing.T) {
	forEachDriver(t, func(t *testing.T, d Driver) {
		dir := t.TempDir()
		db, path := openSeeded(t, d, dir, 5000)
		ctx := context.Background()

		writer, err := d.Open(path, 5000)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Close() }()
		var commits atomic.Int64
		stop := make(chan struct{})
		done := make(chan error)
		go func() {
			for seq := 0; ; seq++ {
				select {
				case <-stop:
					done <- nil
					return
				default:
				}
				if err := CommitTenOps(ctx, writer, seq, 5000); err != nil {
					done <- err
					return
				}
				commits.Add(1)
			}
		}()

		for commits.Load() < 3 {
			time.Sleep(time.Millisecond)
		}
		before := commits.Load()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "snap.db")
		if err := d.Backup(ctx, conn, dst); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		after := commits.Load()
		close(stop)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if after == before {
			t.Log("note: no commit landed during the backup")
		}

		integrityOK(t, d, dst)
		snap, err := d.Open(dst, 5000)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = snap.Close() }()
		// The snapshot is one consistent state: every change set has all 10 ops.
		got := scalar[int64](t, snap, `SELECT count(*) FROM change_sets`)
		ops := scalar[int64](t, snap, `SELECT count(*) FROM ops`)
		if ops != got*10 || got < before {
			t.Fatalf("snapshot has %d change sets, %d ops; %d committed before backup", got, ops, before)
		}
		t.Logf("backup during writes: %d commits before, %d after, snapshot holds %d", before, after, got)
	})
}

// §8.5: restore copies a snapshot into the live database with the backup API;
// connections that stay open see the restored state.
func TestRestoreIntoLiveDB(t *testing.T) {
	forEachDriver(t, func(t *testing.T, d Driver) {
		dir := t.TempDir()
		ctx := context.Background()
		db, path := openSeeded(t, d, dir, 1000)
		if _, err := db.ExecContext(ctx, `INSERT INTO meta VALUES ('mark', 'snapshot')`); err != nil {
			t.Fatal(err)
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		snapPath := filepath.Join(dir, "snap.db")
		if err := d.Backup(ctx, conn, snapPath); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()

		// Diverge the live database, and keep a second connection open on it.
		if _, err := db.ExecContext(ctx, `UPDATE meta SET value = 'live' WHERE key = 'mark'; DELETE FROM nodes WHERE id > 'n00000000500'`); err != nil {
			t.Fatal(err)
		}
		other, err := d.Open(path, 5000)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = other.Close() }()
		otherConn, err := other.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = otherConn.Close() }()
		if got := scalar[string](t, otherConn, `SELECT value FROM meta WHERE key = 'mark'`); got != "live" {
			t.Fatalf("before restore other sees %q", got)
		}

		conn, err = db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Restore(ctx, conn, snapPath); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()

		if got := scalar[string](t, otherConn, `SELECT value FROM meta WHERE key = 'mark'`); got != "snapshot" {
			t.Fatalf("after restore other sees %q", got)
		}
		if got := scalar[int](t, otherConn, `SELECT count(*) FROM nodes`); got != 1000 {
			t.Fatalf("after restore other sees %d nodes", got)
		}
		if got := scalar[string](t, otherConn, `PRAGMA journal_mode`); got != "wal" {
			t.Fatalf("journal_mode after restore = %q", got)
		}
		// The restored DB stays writable and consistent.
		if err := CommitTenOps(ctx, db, 1, 1000); err != nil {
			t.Fatal(err)
		}
		integrityOK(t, d, path)
	})
}

// §9.3: PRAGMA data_version changes on a connection after another process writes.
func TestDataVersionAcrossProcesses(t *testing.T) {
	forEachDriver(t, func(t *testing.T, d Driver) {
		ctx := context.Background()
		db, path := openSeeded(t, d, t.TempDir(), 10)
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		v0 := scalar[int64](t, conn, `PRAGMA data_version`)
		if got := scalar[int64](t, conn, `PRAGMA data_version`); got != v0 {
			t.Fatalf("data_version changed without writes: %d → %d", v0, got)
		}
		if err := runHelper(t, "write", d, path, "x").Run(); err != nil {
			t.Fatal(err)
		}
		v1 := scalar[int64](t, conn, `PRAGMA data_version`)
		if v1 == v0 {
			t.Fatalf("data_version unchanged (%d) after another process wrote", v0)
		}
		if got := scalar[string](t, conn, `SELECT value FROM meta WHERE key = 'from_helper'`); got != "x" {
			t.Fatalf("helper write not visible: %q", got)
		}
		// A write on the same connection does not change its own data_version.
		if _, err := conn.ExecContext(ctx, `INSERT INTO meta VALUES ('self', '1')`); err != nil {
			t.Fatal(err)
		}
		if got := scalar[int64](t, conn, `PRAGMA data_version`); got != v1 {
			t.Fatalf("own write changed data_version %d → %d", v1, got)
		}
	})
}

// §8.6: busy_timeout waits for another process's write lock, and gives up past it.
func TestBusyTimeoutAcrossProcesses(t *testing.T) {
	forEachDriver(t, func(t *testing.T, d Driver) {
		ctx := context.Background()
		dir := t.TempDir()
		_, path := openSeeded(t, d, dir, 10)

		hold := func(ms int) *exec.Cmd {
			cmd := runHelper(t, "hold", d, path, strconv.Itoa(ms))
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(out).ReadString('\n')
			if err != nil || line != "locked\n" {
				t.Fatalf("helper: %q %v", line, err)
			}
			return cmd
		}
		write := func(busyMillis int) (time.Duration, error) {
			db, err := d.Open(path, busyMillis)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			start := time.Now()
			_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO meta VALUES ('waiter', '1')`)
			return time.Since(start), err
		}

		cmd := hold(400)
		waited, err := write(5000)
		if err != nil {
			t.Fatalf("write within busy_timeout failed: %v", err)
		}
		if waited < 200*time.Millisecond {
			t.Fatalf("write did not wait for the lock (%v)", waited)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}

		cmd = hold(2000)
		waited, err = write(200)
		if err == nil {
			t.Fatal("write succeeded while another process held the lock past busy_timeout")
		}
		if waited < 150*time.Millisecond || waited > 1500*time.Millisecond {
			t.Errorf("busy error after %v, want about 200ms", waited)
		}
		t.Logf("busy after %v: %v", waited, err)
		if err := cmd.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatal(err)
		}
	})
}
