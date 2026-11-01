package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/thorhanks/hyphal/internal/model"
)

// Dump is everything needed to build the in-memory tree, read at one moment
// (§7.3, §9.3). Page markdown isn't in it; pages hold only their metadata.
type Dump struct {
	Nodes     []model.Node // live and trashed
	Pages     map[model.NodeID]model.Page
	Stars     map[model.NodeID]model.Time
	Collapsed map[model.NodeID]bool
	UIState   map[string]json.RawMessage
	Meta      map[string]string
	Seq       int64 // HistorySeq when this was read
}

// LoadAll reads the outline in one read transaction, without page markdown so
// that the first frame doesn't wait for it (§6.2, §9.3).
func (db *DB) LoadAll(ctx context.Context) (*Dump, error) {
	tx, err := db.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	d := &Dump{
		Pages:     map[model.NodeID]model.Page{},
		Stars:     map[model.NodeID]model.Time{},
		Collapsed: map[model.NodeID]bool{},
		UIState:   map[string]json.RawMessage{},
		Meta:      map[string]string{},
	}
	if d.Seq, err = historySeq(ctx, tx); err != nil {
		return nil, fmt.Errorf("load history position: %w", err)
	}
	if d.Nodes, err = loadNodes(ctx, tx); err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	if err := eachRow(ctx, tx, `SELECT node_id, '', updated_at, version FROM pages`, func(rows *sql.Rows) error {
		p, err := scanPage(rows)
		d.Pages[p.NodeID] = p
		return err
	}); err != nil {
		return nil, fmt.Errorf("load pages: %w", err)
	}
	if err := eachRow(ctx, tx, `SELECT node_id, starred_at FROM stars`, func(rows *sql.Rows) error {
		var id, starred string
		if err := rows.Scan(&id, &starred); err != nil {
			return err
		}
		t, err := model.ParseTime(starred)
		d.Stars[model.NodeID(id)] = t
		return err
	}); err != nil {
		return nil, fmt.Errorf("load stars: %w", err)
	}
	if err := eachRow(ctx, tx, `SELECT node_id, collapsed FROM view_state`, func(rows *sql.Rows) error {
		var id string
		var collapsed bool
		err := rows.Scan(&id, &collapsed)
		d.Collapsed[model.NodeID(id)] = collapsed
		return err
	}); err != nil {
		return nil, fmt.Errorf("load view state: %w", err)
	}
	if err := eachRow(ctx, tx, `SELECT key, value FROM ui_state`, func(rows *sql.Rows) error {
		var key, value string
		err := rows.Scan(&key, &value)
		d.UIState[key] = json.RawMessage(value)
		return err
	}); err != nil {
		return nil, fmt.Errorf("load ui state: %w", err)
	}
	if err := eachRow(ctx, tx, `SELECT key, coalesce(value, '') FROM meta`, func(rows *sql.Rows) error {
		var key, value string
		err := rows.Scan(&key, &value)
		d.Meta[key] = value
		return err
	}); err != nil {
		return nil, fmt.Errorf("load meta: %w", err)
	}
	return d, nil
}

func loadNodes(ctx context.Context, tx *sql.Tx) ([]model.Node, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM nodes`).Scan(&count); err != nil {
		return nil, err
	}
	nodes := make([]model.Node, 0, count)
	err := eachRow(ctx, tx, `SELECT id, coalesce(parent_id, ''), position, text, is_task, is_heading,
		completed_at, created_at, updated_at, version, deleted_at, coalesce(deleted_by, '') FROM nodes`, func(rows *sql.Rows) error {
		var n model.Node
		var id, parent, deletedBy, created, updated string
		var completed, deleted sql.NullString
		if err := rows.Scan(&id, &parent, &n.Position, &n.Text, &n.IsTask, &n.IsHeading,
			&completed, &created, &updated, &n.Version, &deleted, &deletedBy); err != nil {
			return err
		}
		n.ID, n.ParentID, n.DeletedBy = model.NodeID(id), model.NodeID(parent), model.ChangeSetID(deletedBy)
		var err error
		if n.CompletedAt, err = parseTime(completed); err != nil {
			return err
		}
		if n.DeletedAt, err = parseTime(deleted); err != nil {
			return err
		}
		if n.CreatedAt, err = model.ParseTime(created); err != nil {
			return err
		}
		if n.UpdatedAt, err = model.ParseTime(updated); err != nil {
			return err
		}
		nodes = append(nodes, n)
		return nil
	})
	return nodes, err
}

// eachRow runs query and calls scan for every row.
func eachRow(ctx context.Context, tx *sql.Tx, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

type scanner interface {
	Scan(...any) error
}

func scanPage(s scanner) (model.Page, error) {
	var p model.Page
	var id, updated string
	if err := s.Scan(&id, &p.Markdown, &updated, &p.Version); err != nil {
		return model.Page{}, err
	}
	p.NodeID = model.NodeID(id)
	var err error
	p.UpdatedAt, err = model.ParseTime(updated)
	return p, err
}

// LoadPages reads every page with its markdown, for the background load after
// the first frame (§6.2).
func (db *DB) LoadPages(ctx context.Context) (map[model.NodeID]model.Page, error) {
	tx, err := db.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	pages := map[model.NodeID]model.Page{}
	err = eachRow(ctx, tx, `SELECT node_id, markdown, updated_at, version FROM pages`, func(rows *sql.Rows) error {
		p, err := scanPage(rows)
		pages[p.NodeID] = p
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load pages: %w", err)
	}
	return pages, nil
}

// Page reads one page, and reports whether it exists (§5.7).
func (db *DB) Page(ctx context.Context, id model.NodeID) (model.Page, bool, error) {
	p, err := scanPage(db.sql.QueryRowContext(ctx, `SELECT node_id, markdown, updated_at, version FROM pages WHERE node_id = ?`, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Page{}, false, nil
	}
	if err != nil {
		return model.Page{}, false, err
	}
	return p, true, nil
}
