package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/thorhanks/hyphal/internal/model"
)

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
