package store

import (
	"context"
	"database/sql"
	_ "embed" // schema/0001_init.sql
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

//go:embed schema/0001_init.sql
var schemaV1 string

const (
	formatID           = "tree-outline" // D-005
	defaultBusyTimeout = 5 * time.Second
	walRetryDelay      = 10 * time.Millisecond
	description        = "An outline stored as a tree. nodes holds every bullet, " +
		"ordered among its siblings by the fractional key in position; deleted_at marks " +
		"the ones in the Trash. pages holds an optional Markdown document per bullet, and " +
		"stars the starred bullets. change_sets and ops record every change, with before " +
		"and after values as JSON; the current rows are authoritative and history is a " +
		"record beside them. view_state and ui_state are per device and are not history."
)

// Errors returned by the store (ARCHITECTURE §4.2).
var (
	// ErrReadOnly is returned by every write on a read-only database (§7.4).
	ErrReadOnly = errors.New("database is read-only")
	// ErrNotOutline means the file holds other tables and isn't an outline, so
	// it is left untouched.
	ErrNotOutline = errors.New("file is not an outline database")
	// ErrNeedsMigration means a read-only open found an older schema, which
	// can't be migrated without writing.
	ErrNeedsMigration = errors.New("database needs a migration and is opened read-only")
	// ErrConflict means an Expect failed at commit (§7.3).
	ErrConflict = errors.New("version conflict")
	// ErrInvalid means a commit would break a tree invariant (CONTRACTS §4, §8).
	ErrInvalid = errors.New("invalid change")
	// ErrBusy means another writer held the database past the busy timeout (§8.6).
	ErrBusy = errors.New("database is busy")
)

// OpenOptions configures Open (ARCHITECTURE §4.2).
type OpenOptions struct {
	Create      bool             // create file and schema if missing
	BusyTimeout time.Duration    // default 5s (§8.6)
	ReadOnly    bool             // force read-only
	Clock       func() time.Time // default time.Now
	SnapshotDir string           // default <dir of db>/snapshots (D-014)
}

// ReadOnlyReason says why a database is read-only (§4.8, §7.4).
type ReadOnlyReason int

// The read-only reasons.
const (
	ReadOnlyNone ReadOnlyReason = iota
	ReadOnlyNewerSchema
	ReadOnlyIntegrityFailed
	ReadOnlyLockHeld
	ReadOnlyForced
)

// DB is an open outline database (§7).
type DB struct {
	sql      *sql.DB
	path     string
	opts     OpenOptions
	readOnly ReadOnlyReason

	commitHook func(op int) error // test only: runs before each op, an error aborts
}

// Open opens the database at path, creating the file and schema if asked and
// migrating an older schema after taking a snapshot. A newer schema opens
// read-only (§7.2, §7.4, §8.1).
func Open(path string, o OpenOptions) (*DB, error) {
	if o.BusyTimeout == 0 {
		o.BusyTimeout = defaultBusyTimeout
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.SnapshotDir == "" {
		o.SnapshotDir = filepath.Join(filepath.Dir(path), "snapshots")
	}
	create := o.Create && !o.ReadOnly
	if err := prepareFile(path, create); err != nil {
		return nil, err
	}
	conn, err := sql.Open(driverName, dsn(path, o.BusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db := &DB{sql: conn, path: path, opts: o}
	if err := db.prepareSchema(context.Background(), create); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// prepareFile creates a missing database file 0600 in a 0700 directory, since
// SQLite would use the umask (§7.2). The -wal and -shm files copy its mode.
func prepareFile(path string, create bool) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) || !create {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	return f.Close()
}

// prepareSchema brings the schema to the known version, or switches to
// read-only when it can't or shouldn't be changed (§7.4).
func (db *DB) prepareSchema(ctx context.Context, create bool) error {
	version, err := readVersion(ctx, db.sql)
	if err != nil {
		return err
	}
	known := knownVersion()
	switch {
	case version > known:
		return db.switchReadOnly(ReadOnlyNewerSchema)
	case version == 0 && !create:
		return fmt.Errorf("%w: it has no schema", ErrNotOutline)
	case version != 0 && version < known && db.opts.ReadOnly:
		return ErrNeedsMigration
	case db.opts.ReadOnly:
		return db.switchReadOnly(ReadOnlyForced)
	}
	if err := db.ensureWAL(ctx); err != nil {
		return err
	}
	created := false
	if version == 0 {
		if created, err = db.createSchema(ctx); err != nil {
			return err
		}
	}
	return db.migrate(ctx, known, !created)
}

// ensureWAL puts the file in WAL mode (§8.1). Changing the mode needs the file
// to itself and doesn't always wait out busy_timeout, so it retries for that
// long when another connection is opening the same new file.
func (db *DB) ensureWAL(ctx context.Context) error {
	deadline := time.Now().Add(db.opts.BusyTimeout)
	for {
		var mode string
		err := db.sql.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&mode)
		switch {
		case err == nil && mode != "wal":
			return fmt.Errorf("journal mode is %q, not wal", mode)
		case err == nil:
			return nil
		case !isBusy(err) || time.Now().After(deadline):
			return fmt.Errorf("set WAL mode: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(walRetryDelay):
		}
	}
}

// switchReadOnly reopens the pool so that SQLite refuses writes on every
// connection, in addition to the ErrReadOnly checks (§7.4).
func (db *DB) switchReadOnly(reason ReadOnlyReason) error {
	if err := db.sql.Close(); err != nil {
		return err
	}
	conn, err := sql.Open(driverName, dsn(db.path, db.opts.BusyTimeout, "query_only(ON)"))
	if err != nil {
		return err
	}
	db.sql = conn
	db.readOnly = reason
	return nil
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readVersion returns meta.schema_version, or 0 for a file with no tables. A
// file with tables must carry our format_id (D-005), or it is someone else's.
func readVersion(ctx context.Context, q querier) (int, error) {
	var tables, hasMeta int
	if err := q.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(name = 'meta'), 0) FROM sqlite_master WHERE type = 'table'`).Scan(&tables, &hasMeta); err != nil {
		return 0, err
	}
	if tables == 0 {
		return 0, nil
	}
	if hasMeta == 0 {
		return 0, fmt.Errorf("%w: it has no meta table", ErrNotOutline)
	}
	format, _, err := metaValue(ctx, q, "format_id")
	if err != nil {
		return 0, err
	}
	if format != formatID {
		return 0, fmt.Errorf("%w: format_id is %q", ErrNotOutline, format)
	}
	value, _, err := metaValue(ctx, q, "schema_version")
	if err != nil {
		return 0, err
	}
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("%w: schema_version is %q", ErrNotOutline, value)
	}
	return version, nil
}

// createSchema writes schema v1, the root row and the meta rows in one
// transaction (CONTRACTS §4). It returns false if another process got there first.
func (db *DB) createSchema(ctx context.Context) (bool, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	version, err := readVersion(ctx, tx)
	if err != nil {
		return false, err
	}
	if version != 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
		return false, fmt.Errorf("create schema: %w", err)
	}
	root := ids.NewNode()
	now := model.NewTime(db.opts.Clock()).String()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO nodes (id, parent_id, position, text, created_at, updated_at) VALUES (?, NULL, 'a0', '', ?, ?)`,
		string(root), now, now); err != nil {
		return false, fmt.Errorf("create root: %w", err)
	}
	for key, value := range map[string]string{
		"schema_version": "1",
		"format_id":      formatID,
		"root_id":        string(root),
		"description":    description,
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, key, value); err != nil {
			return false, fmt.Errorf("write meta: %w", err)
		}
	}
	return true, tx.Commit()
}

// Close closes the database.
func (db *DB) Close() error {
	return db.sql.Close()
}

// ReadOnly returns why the database is read-only, or ReadOnlyNone (§7.4).
func (db *DB) ReadOnly() ReadOnlyReason {
	return db.readOnly
}

// Path returns the database file's path.
func (db *DB) Path() string {
	return db.path
}

// Meta returns one row of the meta table (CONTRACTS §4).
func (db *DB) Meta(ctx context.Context, key string) (string, bool, error) {
	return metaValue(ctx, db.sql, key)
}

func metaValue(ctx context.Context, q querier, key string) (string, bool, error) {
	var value string
	err := q.QueryRowContext(ctx, `SELECT coalesce(value, '') FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}
