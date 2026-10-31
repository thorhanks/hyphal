package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"time"

	"modernc.org/sqlite"
)

// driverName is the database/sql name of modernc.org/sqlite, the driver chosen
// in P0-05 (plan/reports/p0-05-sqlite-driver.md).
const driverName = "sqlite"

// dsn returns a file URI whose pragmas the driver applies to every new
// connection in the pool (§8.1, CONTRACTS §4). Write transactions begin
// IMMEDIATE so a busy writer waits up to busy instead of failing on upgrade
// (§8.6).
func dsn(path string, busy time.Duration) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "foreign_keys(ON)")
	if runtime.GOOS == "darwin" {
		q.Add("_pragma", "fullfsync(ON)") // §9.4
	}
	q.Set("_txlock", "immediate")
	return fileURI(path, q)
}

// fileURI escapes path as an SQLite URI filename, so names containing ?, #
// or % still mean the file.
func fileURI(path string, q url.Values) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: q.Encode()}
	if !filepath.IsAbs(path) {
		u = url.URL{Scheme: "file", Opaque: (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath(), RawQuery: q.Encode()}
	}
	return u.String()
}

// backuper is the online backup API of a modernc.org/sqlite connection.
type backuper interface {
	NewBackup(dstURI string) (*sqlite.Backup, error)
	NewRestore(srcURI string) (*sqlite.Backup, error)
}

// backupTo copies the main database of conn to a new file at dst with the
// online backup API, while other connections keep writing (§8.5).
func backupTo(ctx context.Context, conn *sql.Conn, dst string) error {
	return runBackup(ctx, conn, func(b backuper) (*sqlite.Backup, error) { return b.NewBackup(fileURI(dst, nil)) })
}

// restoreFrom copies the database file at src into the main database of conn
// with the online backup API. Files are never renamed over the live database,
// and other open connections see the restored content (§8.5). src is opened
// read-only, so a missing file fails instead of restoring an empty database.
func restoreFrom(ctx context.Context, conn *sql.Conn, src string) error {
	ro := url.Values{"mode": {"ro"}}
	return runBackup(ctx, conn, func(b backuper) (*sqlite.Backup, error) { return b.NewRestore(fileURI(src, ro)) })
}

func runBackup(ctx context.Context, conn *sql.Conn, start func(backuper) (*sqlite.Backup, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return conn.Raw(func(dc any) error {
		b, ok := dc.(backuper)
		if !ok {
			return fmt.Errorf("backup: driver connection %T has no backup API", dc)
		}
		bk, err := start(b)
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		// One step copies everything inside one read transaction. In WAL mode
		// that never blocks writers. Smaller steps would restart each time
		// another connection writes, so a busy writer could starve them.
		_, err = bk.Step(-1)
		return errors.Join(err, bk.Finish())
	})
}
