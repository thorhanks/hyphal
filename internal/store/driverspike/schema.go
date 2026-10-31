package driverspike

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
)

// Schema is the v1 DDL from SPEC §7.2 and §8.2, enough for realistic
// commit and load measurements.
const Schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE nodes (
  id           TEXT PRIMARY KEY,
  parent_id    TEXT REFERENCES nodes(id),
  position     TEXT NOT NULL,
  text         TEXT NOT NULL,
  is_task      INTEGER NOT NULL DEFAULT 0,
  is_heading   INTEGER NOT NULL DEFAULT 0,
  completed_at TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  version      INTEGER NOT NULL DEFAULT 1,
  deleted_at   TEXT,
  deleted_by   TEXT
);
CREATE TABLE pages (
  node_id    TEXT PRIMARY KEY REFERENCES nodes(id),
  markdown   TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  version    INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX nodes_by_parent ON nodes(parent_id, position);
CREATE INDEX nodes_in_trash  ON nodes(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE TABLE change_sets (
  id          TEXT PRIMARY KEY,
  at          TEXT NOT NULL,
  source      TEXT NOT NULL,
  description TEXT,
  undo_of     TEXT
);
CREATE INDEX change_sets_by_at ON change_sets(at);
CREATE TABLE ops (
  id            INTEGER PRIMARY KEY,
  change_set_id TEXT NOT NULL REFERENCES change_sets(id),
  node_id       TEXT NOT NULL,
  kind          TEXT NOT NULL,
  before        TEXT,
  after         TEXT
);
CREATE INDEX ops_by_node ON ops(node_id);
CREATE INDEX ops_by_change_set ON ops(change_set_id);
`

const stamp = "2026-10-26T09:30:00.123Z"

// NodeID returns a deterministic 12-character ID for node i.
func NodeID(i int) string { return fmt.Sprintf("n%011d", i) }

// Seed creates the schema and n nodes in a random tree (fan-out up to 20).
func Seed(ctx context.Context, db *sql.DB, n int) error {
	if _, err := db.ExecContext(ctx, Schema); err != nil {
		return err
	}
	r := rand.New(rand.NewPCG(1, uint64(n)))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	st, err := tx.PrepareContext(ctx, `INSERT INTO nodes
		(id, parent_id, position, text, is_task, completed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	for i := range n {
		var parent any
		if i > 0 {
			parent = NodeID(max(0, i-1-r.IntN(20)))
		}
		var completed any
		if r.IntN(10) == 0 {
			completed = stamp
		}
		text := strings.Repeat("word ", 2+r.IntN(14))
		if _, err := st.ExecContext(ctx, NodeID(i), parent, fmt.Sprintf("a%d", i), text,
			r.IntN(5) == 0, completed, stamp, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Node mirrors the row shape LoadAll reads (ARCHITECTURE §4.1).
type Node struct {
	ID, ParentID, Position, Text string
	IsTask, IsHeading            bool
	CompletedAt                  *string
	CreatedAt, UpdatedAt         string
	Version                      int64
	DeletedAt, DeletedBy         *string
}

// LoadAll reads every node row into structs.
func LoadAll(ctx context.Context, db *sql.DB) ([]Node, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, coalesce(parent_id, ''), position, text,
		is_task, is_heading, completed_at, created_at, updated_at, version, deleted_at, deleted_by
		FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.ParentID, &n.Position, &n.Text, &n.IsTask, &n.IsHeading,
			&n.CompletedAt, &n.CreatedAt, &n.UpdatedAt, &n.Version, &n.DeletedAt, &n.DeletedBy); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CommitTenOps writes one change set with 10 edit_text ops, the way
// store.Commit will: read before values, update rows, insert history.
func CommitTenOps(ctx context.Context, db *sql.DB, seq, n int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	cs := fmt.Sprintf("c%011d", seq)
	if _, err := tx.ExecContext(ctx, `INSERT INTO change_sets (id, at, source, description) VALUES (?, ?, 'cli', 'spike')`,
		cs, stamp); err != nil {
		return err
	}
	for k := range 10 {
		id := NodeID((seq*10 + k*7919) % n)
		var before string
		if err := tx.QueryRowContext(ctx, `SELECT text FROM nodes WHERE id = ?`, id).Scan(&before); err != nil {
			return err
		}
		after := fmt.Sprintf("edited %d.%d", seq, k)
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET text = ?, updated_at = ?, version = version + 1 WHERE id = ?`,
			after, stamp, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ops (change_set_id, node_id, kind, before, after)
			VALUES (?, ?, 'edit_text', json_object('text', ?), json_object('text', ?))`, cs, id, before, after); err != nil {
			return err
		}
	}
	return tx.Commit()
}
