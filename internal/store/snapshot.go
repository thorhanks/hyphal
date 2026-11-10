package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/platform"
)

// snapshotReasons are the reasons in snapshot names (D-014).
var snapshotReasons = []string{"daily", "pre-migration", "pre-restore", "pre-import"}

// snapshotStamp is the UTC stamp in snapshot names (D-014).
const snapshotStamp = "20060102T150405Z"

// tmpSuffix marks a snapshot still being written. ListSnapshots never lists it.
const tmpSuffix = ".tmp"

// SnapshotInfo describes one good snapshot (§8.5, CONTRACTS §7).
type SnapshotInfo struct {
	Name      string // file name in the snapshot directory
	Size      int64
	CreatedAt time.Time
	Reason    string
}

// Snapshot copies the database into the snapshot directory with the online
// backup API and returns the snapshot's file name (§8.5, D-014). The copy is
// written to a .tmp file, passes quick_check, and only then is renamed to its
// snapshot name, so a crash never leaves a partial file that looks like a good
// snapshot. A copy that fails the check is deleted and reported. P1-09 adds
// retention, the due logic and restore.
func (db *DB) Snapshot(ctx context.Context, reason string) (string, error) {
	path, err := db.snapshotTo(ctx, reason)
	if err != nil {
		return "", err
	}
	return filepath.Base(path), nil
}

// snapshotTo takes a snapshot and returns its path.
func (db *DB) snapshotTo(ctx context.Context, reason string) (string, error) {
	if !slices.Contains(snapshotReasons, reason) {
		return "", fmt.Errorf("snapshot: unknown reason %q", reason)
	}
	dir := db.opts.SnapshotDir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path, f, err := db.newSnapshotFile(reason)
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			removeSnapshotTmp(tmp)
		}
	}()
	if err := f.Close(); err != nil {
		return "", err
	}
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return "", err
	}
	err = backupTo(ctx, conn, tmp)
	_ = conn.Close()
	if err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	if err := finishSnapshot(ctx, tmp); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	ok = true
	if err := platform.SyncDir(dir); err != nil {
		return path, fmt.Errorf("snapshot %s: %w", filepath.Base(path), err)
	}
	return path, nil
}

// newSnapshotFile reserves a snapshot name and creates its .tmp file (0600).
// If the name is taken it moves to the next free second, so names always keep
// the D-014 format.
func (db *DB) newSnapshotFile(reason string) (string, *os.File, error) {
	name := strings.TrimSuffix(filepath.Base(db.path), filepath.Ext(db.path))
	for at := model.NewTime(db.opts.Clock()); ; at.Time = at.Add(time.Second) {
		path := filepath.Join(db.opts.SnapshotDir, fmt.Sprintf("%s-%s-%s.db", name, at.Format(snapshotStamp), reason))
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		f, err := os.OpenFile(path+tmpSuffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return path, f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
}

// finishSnapshot makes the copy a single self-contained file (rollback
// journal instead of WAL) and runs quick_check on it (§8.5).
func finishSnapshot(ctx context.Context, path string) error {
	conn, err := sql.Open(driverName, fileURI(path, url.Values{"_pragma": {"synchronous(FULL)"}}))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var mode string
	if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode = DELETE`).Scan(&mode); err != nil {
		return err
	}
	return quickCheck(ctx, conn)
}

// quickCheck runs PRAGMA quick_check and returns an error unless it says ok.
func quickCheck(ctx context.Context, q *sql.DB) error {
	rows, err := q.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var problems []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return err
		}
		if s != "ok" {
			problems = append(problems, s)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("quick_check: %s", strings.Join(problems, "; "))
	}
	return nil
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// removeSnapshotTmp deletes a failed copy and any journal SQLite left beside it.
func removeSnapshotTmp(tmp string) {
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		_ = os.Remove(tmp + suffix)
	}
}

// ListSnapshots lists the good snapshots in dir, newest first (§8.5). Only
// files named per D-014 count; .tmp files are copies a crash interrupted and
// are never listed. A missing directory has no snapshots.
func ListSnapshots(dir string) ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SnapshotInfo
	for _, e := range entries {
		at, reason, ok := parseSnapshotName(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, SnapshotInfo{Name: e.Name(), Size: info.Size(), CreatedAt: at, Reason: reason})
	}
	slices.SortFunc(out, func(a, b SnapshotInfo) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	return out, nil
}

// parseSnapshotName reads `<dbname>-<stamp>-<reason>.db` (D-014).
func parseSnapshotName(name string) (time.Time, string, bool) {
	base, ok := strings.CutSuffix(name, ".db")
	if !ok {
		return time.Time{}, "", false
	}
	for _, reason := range snapshotReasons {
		rest, ok := strings.CutSuffix(base, "-"+reason)
		if !ok || len(rest) < len(snapshotStamp)+2 {
			continue
		}
		stamp := rest[len(rest)-len(snapshotStamp):]
		if rest[len(rest)-len(snapshotStamp)-1] != '-' {
			continue
		}
		at, err := time.Parse(snapshotStamp, stamp)
		if err != nil {
			continue
		}
		return at, reason, true
	}
	return time.Time{}, "", false
}
