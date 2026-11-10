package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
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
