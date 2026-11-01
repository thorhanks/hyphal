package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/thorhanks/hyphal/internal/model"
)

// epochShift keeps a history position's epoch clear of change set rowids.
const epochShift = 40

// HistorySeq returns a number that changes whenever anyone changes the outline,
// from any connection (§8.2). A new change set adds 1, stars included. Rowids can
// repeat after rows are removed, so whatever replaces or removes change sets calls
// bumpHistoryEpoch (A-009).
func (db *DB) HistorySeq(ctx context.Context) (int64, error) {
	return historySeq(ctx, db.sql)
}

func historySeq(ctx context.Context, q querier) (int64, error) {
	var seq int64
	err := q.QueryRowContext(ctx, `SELECT
		(coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'last_history_epoch'), 0) << ?)
		+ coalesce((SELECT max(rowid) FROM change_sets), 0)`, epochShift).Scan(&seq)
	return seq, err
}

// bumpHistoryEpoch moves the history position so it can't match any earlier one.
// The last_ prefix keeps the key local: it is never exported or imported.
func bumpHistoryEpoch(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('last_history_epoch', '1')
		ON CONFLICT (key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)`)
	return err
}

// ChangeSetByID reads one change set with its ops in order, or returns
// ErrNotFound (§8.2).
func (db *DB) ChangeSetByID(ctx context.Context, id model.ChangeSetID) (model.ChangeSet, error) {
	tx, err := db.readTx(ctx)
	if err != nil {
		return model.ChangeSet{}, err
	}
	defer func() { _ = tx.Rollback() }()
	cs := model.ChangeSet{ID: id}
	var at string
	var description, undoOf sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT at, source, description, undo_of FROM change_sets WHERE id = ?`, string(id)).
		Scan(&at, &cs.Source, &description, &undoOf)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ChangeSet{}, fmt.Errorf("%w: change set %s", ErrNotFound, id)
	}
	if err != nil {
		return model.ChangeSet{}, err
	}
	if cs.At, err = model.ParseTime(at); err != nil {
		return model.ChangeSet{}, err
	}
	cs.Description, cs.UndoOf = description.String, model.ChangeSetID(undoOf.String)
	rows, err := tx.QueryContext(ctx, `SELECT node_id, kind, before, after FROM ops WHERE change_set_id = ? ORDER BY id`, string(id))
	if err != nil {
		return model.ChangeSet{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var node, kind string
		var before, after sql.NullString
		if err := rows.Scan(&node, &kind, &before, &after); err != nil {
			return model.ChangeSet{}, err
		}
		cs.Ops = append(cs.Ops, model.Op{NodeID: model.NodeID(node), Kind: model.OpKind(kind), Before: rawOrNull(before), After: rawOrNull(after)})
	}
	return cs, rows.Err()
}
