package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thorhanks/hyphal/internal/model"
)

// baseVersion is the schema written by createSchema.
const baseVersion = 1

// Migration moves the schema forward one step (§7.4). Each runs in its own
// transaction with the bump of schema_version.
type Migration struct {
	From, To int
	SQL      string
	Go       func(*sql.Tx) error // optional, runs after SQL
}

// migrations lists every migration in order.
var migrations []Migration

// knownVersion is the newest schema this build understands.
func knownVersion() int {
	if len(migrations) == 0 {
		return baseVersion
	}
	return migrations[len(migrations)-1].To
}

// migrate runs the pending migrations, after one snapshot of the database as
// it stands (§7.4, §8.5). A failed snapshot stops before anything changes. A
// file created by this open holds nothing to protect, so it takes no snapshot.
func (db *DB) migrate(ctx context.Context, known int, snapshot bool) error {
	version, err := readVersion(ctx, db.sql)
	if err != nil || version >= known {
		return err
	}
	if snapshot {
		if _, err := db.snapshotTo(ctx, "pre-migration"); err != nil {
			return fmt.Errorf("snapshot before migrating: %w", err)
		}
	}
	for _, m := range migrations {
		if m.From < version {
			continue
		}
		if err := db.runMigration(ctx, m); err != nil {
			return fmt.Errorf("migrate %d to %d: %w", m.From, m.To, err)
		}
		version = m.To
	}
	if version != known {
		return fmt.Errorf("no migration path from schema %d to %d", version, known)
	}
	return nil
}

// runMigration applies m unless another process already has.
func (db *DB) runMigration(ctx context.Context, m Migration) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	version, err := readVersion(ctx, tx)
	if err != nil {
		return err
	}
	if version >= m.To {
		return nil
	}
	if version != m.From {
		return fmt.Errorf("schema is at %d", version)
	}
	if m.SQL != "" {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
	}
	if m.Go != nil {
		if err := m.Go(tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(m.To)); err != nil {
		return err
	}
	return tx.Commit()
}

// snapshotTo copies the database to a new 0600 file in the snapshot directory,
// named per D-014, and returns its path. P1-09 adds checks and retention.
func (db *DB) snapshotTo(ctx context.Context, reason string) (string, error) {
	if err := os.MkdirAll(db.opts.SnapshotDir, 0o700); err != nil {
		return "", err
	}
	path, f, err := db.newSnapshotFile(reason)
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	if err := backupTo(ctx, conn, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// newSnapshotFile creates the file. If the name is taken it moves to the next
// free second, so names always keep the D-014 format.
func (db *DB) newSnapshotFile(reason string) (string, *os.File, error) {
	name := strings.TrimSuffix(filepath.Base(db.path), filepath.Ext(db.path))
	for at := model.NewTime(db.opts.Clock()); ; at.Time = at.Add(time.Second) {
		stamp := at.Format("20060102T150405Z")
		path := filepath.Join(db.opts.SnapshotDir, fmt.Sprintf("%s-%s-%s.db", name, stamp, reason))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return path, f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
}
