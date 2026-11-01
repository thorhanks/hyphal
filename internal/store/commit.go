package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

// Expect is a version the caller requires of a node or page at commit (§7.3).
// A missing row counts as version 0.
type Expect struct {
	NodeID  model.NodeID
	Version int64
	Page    bool
}

// CommitResult is what Commit recorded (§8.2).
type CommitResult struct {
	ChangeSet model.ChangeSet // as recorded: before values recomputed from the database
	Stale     []model.NodeID  // nodes whose version differed from what the caller last saw, sorted
}

// Commit applies a change set and writes its history in one transaction
// (§7.3, §8.2). It recomputes every before value from the current rows; a
// merge's merged_into is kept as given (CONTRACTS §5). An empty cs.ID or zero
// cs.At is filled in. Errors: ErrReadOnly, ErrConflict (an Expect failed),
// ErrInvalid (a tree invariant would break) and ErrBusy; nothing is written.
func (db *DB) Commit(ctx context.Context, cs model.ChangeSet, expect []Expect, seen map[model.NodeID]int64) (CommitResult, error) {
	if db.readOnly != ReadOnlyNone {
		return CommitResult{}, ErrReadOnly
	}
	if cs.ID == "" {
		cs.ID = ids.NewChangeSet()
	}
	if cs.At.IsZero() {
		cs.At = model.NewTime(db.opts.Clock())
	}
	cs.At = model.NewTime(cs.At.Time)
	if !ids.Valid(string(cs.ID)) {
		return CommitResult{}, invalidf("change set ID %q", cs.ID)
	}
	if !validSource(cs.Source) {
		return CommitResult{}, invalidf("source %q", cs.Source)
	}
	res, err := db.commit(ctx, cs, expect, seen)
	if isBusy(err) {
		return CommitResult{}, fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return res, err
}

func (db *DB) commit(ctx context.Context, cs model.ChangeSet, expect []Expect, seen map[model.NodeID]int64) (CommitResult, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return CommitResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	c := committer{ctx: ctx, tx: tx, at: cs.At, cs: cs.ID}

	for _, e := range expect {
		version, err := c.currentVersion(e.NodeID, e.Page)
		if err != nil {
			return CommitResult{}, err
		}
		if version != e.Version {
			return CommitResult{}, fmt.Errorf("%w: %s is at version %d, not %d", ErrConflict, e.NodeID, version, e.Version)
		}
	}
	var stale []model.NodeID
	for id, want := range seen {
		version, err := c.currentVersion(id, false)
		if err != nil {
			return CommitResult{}, err
		}
		if version != want {
			stale = append(stale, id)
		}
	}
	slices.Sort(stale)

	if err := c.insertChangeSet(cs); err != nil {
		return CommitResult{}, err
	}
	recorded := cs
	recorded.Ops = make([]model.Op, 0, len(cs.Ops))
	for i, op := range cs.Ops {
		if db.commitHook != nil {
			if err := db.commitHook(i); err != nil {
				return CommitResult{}, err
			}
		}
		rec, err := c.apply(op)
		if err != nil {
			return CommitResult{}, fmt.Errorf("op %d (%s on %s): %w", i, op.Kind, op.NodeID, err)
		}
		if err := c.insertOp(rec); err != nil {
			return CommitResult{}, err
		}
		recorded.Ops = append(recorded.Ops, rec)
	}
	if err := tx.Commit(); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{ChangeSet: recorded, Stale: stale}, nil
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// validSource reports whether s is a change set source (§8.2).
func validSource(s string) bool {
	switch s {
	case "ui", "cli", "import", "restore":
		return true
	}
	name, ok := strings.CutPrefix(s, "agent:")
	return ok && name != ""
}

// HasLeadingMarker reports whether text starts with a marker that bullet text
// may never start with (§5.4). The store can't import markup; P0-16 tests that
// this agrees with markup.ConvertLeadingMarker (A-003).
func HasLeadingMarker(text string) bool {
	for _, marker := range []string{"[ ] ", "[x] ", "[X] ", "## "} {
		if strings.HasPrefix(text, marker) {
			return true
		}
	}
	return false
}

func checkText(text string) error {
	if strings.ContainsAny(text, "\n\r") {
		return invalidf("text has a line break")
	}
	if HasLeadingMarker(text) {
		return invalidf("text starts with a leading marker: %q", text)
	}
	return nil
}

// committer applies the ops of one change set inside its transaction.
type committer struct {
	ctx context.Context
	tx  *sql.Tx
	at  model.Time
	cs  model.ChangeSetID
}

func (c committer) currentVersion(id model.NodeID, page bool) (int64, error) {
	query := `SELECT version FROM nodes WHERE id = ?`
	if page {
		query = `SELECT version FROM pages WHERE node_id = ?`
	}
	var version int64
	err := c.tx.QueryRowContext(c.ctx, query, string(id)).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func (c committer) insertChangeSet(cs model.ChangeSet) error {
	var taken int
	if err := c.tx.QueryRowContext(c.ctx, `SELECT count(*) FROM change_sets WHERE id = ?`, string(cs.ID)).Scan(&taken); err != nil {
		return err
	}
	if taken != 0 {
		return invalidf("change set %s already exists", cs.ID)
	}
	_, err := c.tx.ExecContext(c.ctx, `INSERT INTO change_sets (id, at, source, description, undo_of) VALUES (?, ?, ?, ?, ?)`,
		string(cs.ID), cs.At.String(), cs.Source, nullIfEmpty(cs.Description), nullIfEmpty(string(cs.UndoOf)))
	return err
}

func (c committer) insertOp(op model.Op) error {
	_, err := c.tx.ExecContext(c.ctx, `INSERT INTO ops (change_set_id, node_id, kind, before, after) VALUES (?, ?, ?, ?, ?)`,
		string(c.cs), string(op.NodeID), string(op.Kind), nullIfJSONNull(op.Before), nullIfJSONNull(op.After))
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullIfJSONNull stores a null payload as SQL NULL (§8.2).
func nullIfJSONNull(raw []byte) any {
	if string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func timeArg(t *model.Time) any {
	if t == nil {
		return nil
	}
	return t.String()
}

// parseTime reads a nullable timestamp column.
func parseTime(s sql.NullString) (*model.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := model.ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// row holds the columns of a node that committing needs.
type row struct {
	parent            sql.NullString
	text              string
	isTask, isHeading bool
	completedAt       *model.Time
	deletedAt         *model.Time
	deletedBy         *model.ChangeSetID
}

func (c committer) loadRow(id model.NodeID) (row, bool, error) {
	var r row
	var completed, deleted, by sql.NullString
	err := c.tx.QueryRowContext(c.ctx,
		`SELECT parent_id, text, is_task, is_heading, completed_at, deleted_at, deleted_by FROM nodes WHERE id = ?`, string(id)).
		Scan(&r.parent, &r.text, &r.isTask, &r.isHeading, &completed, &deleted, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, false, nil
	}
	if err != nil {
		return row{}, false, err
	}
	if r.completedAt, err = parseTime(completed); err != nil {
		return row{}, false, err
	}
	if r.deletedAt, err = parseTime(deleted); err != nil {
		return row{}, false, err
	}
	if by.Valid {
		v := model.ChangeSetID(by.String)
		r.deletedBy = &v
	}
	return r, true, nil
}

// checkLiveParent requires a parent that exists and isn't in the Trash (CONTRACTS §4).
func (c committer) checkLiveParent(parent model.NodeID) error {
	r, found, err := c.loadRow(parent)
	if err != nil {
		return err
	}
	if !found {
		return invalidf("parent %q doesn't exist", parent)
	}
	if r.deletedAt != nil {
		return invalidf("parent %s is in the Trash", parent)
	}
	return nil
}

// apply validates op against the current rows, writes it and returns the op to
// record, with its before recomputed and its after canonical (CONTRACTS §5).
func (c committer) apply(op model.Op) (model.Op, error) {
	if !ids.Valid(string(op.NodeID)) {
		return model.Op{}, invalidf("node ID %q", op.NodeID)
	}
	after, err := op.DecodeAfter()
	if err != nil {
		return model.Op{}, invalidf("%v", err)
	}
	var given any
	if len(op.Before) > 0 { // the caller may leave before out; it isn't trusted anyway
		if given, err = op.DecodeBefore(); err != nil {
			return model.Op{}, invalidf("%v", err)
		}
	}
	cur, found, err := c.loadRow(op.NodeID)
	if err != nil {
		return model.Op{}, err
	}
	switch {
	case op.Kind == model.OpInsert && found:
		return model.Op{}, invalidf("node already exists")
	case op.Kind != model.OpInsert && !found:
		return model.Op{}, invalidf("node doesn't exist")
	case found && !cur.parent.Valid:
		return model.Op{}, invalidf("the root can't change")
	}

	rec := model.Op{NodeID: op.NodeID, Kind: op.Kind}
	var before any
	switch a := after.(type) {
	case *model.InsertFields:
		err = c.applyInsert(op.NodeID, a)
	case *model.DeleteFields:
		if op.Kind == model.OpDelete {
			before, err = c.applyDelete(op.NodeID, cur, a)
		} else {
			before, err = c.applyRestore(op.NodeID, cur, a, given)
		}
	case *model.TextFields:
		before = &model.TextFields{Text: cur.text}
		if err = checkText(a.Text); err == nil {
			err = c.updateNode(op.NodeID, `text = ?`, a.Text)
		}
	case *model.MoveFields:
		before, err = c.applyMove(op.NodeID, cur, a)
	case *model.TaskFields:
		before = &model.TaskFields{IsTask: cur.isTask}
		err = c.updateNode(op.NodeID, `is_task = ?`, a.IsTask)
	case *model.HeadingFields:
		before = &model.HeadingFields{IsHeading: cur.isHeading}
		err = c.updateNode(op.NodeID, `is_heading = ?`, a.IsHeading)
	case *model.CompleteFields:
		before = &model.CompleteFields{CompletedAt: cur.completedAt}
		err = c.updateNode(op.NodeID, `completed_at = ?`, timeArg(a.CompletedAt))
	case *model.StarFields:
		before, err = c.applyStar(op.NodeID, a)
	default: // *model.PageFields, or nil for a deleted page
		page, _ := after.(*model.PageFields)
		before, err = c.applyPage(op.NodeID, page)
	}
	if err != nil {
		return model.Op{}, err
	}
	if err := rec.EncodeBefore(before); err != nil {
		return model.Op{}, err
	}
	if err := rec.EncodeAfter(after); err != nil {
		return model.Op{}, err
	}
	return rec, nil
}

// updateNode sets one field, bumps the version and stamps updated_at (CONTRACTS §4).
func (c committer) updateNode(id model.NodeID, set string, value any) error {
	_, err := c.tx.ExecContext(c.ctx, `UPDATE nodes SET `+set+`, version = version + 1, updated_at = ? WHERE id = ?`,
		value, c.at.String(), string(id))
	return err
}

func (c committer) applyInsert(id model.NodeID, a *model.InsertFields) error {
	if err := checkText(a.Text); err != nil {
		return err
	}
	if !fracindex.Valid(a.Position) {
		return invalidf("position %q", a.Position)
	}
	if err := c.checkLiveParent(a.ParentID); err != nil {
		return err
	}
	_, err := c.tx.ExecContext(c.ctx,
		`INSERT INTO nodes (id, parent_id, position, text, is_task, is_heading, completed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(id), string(a.ParentID), a.Position, a.Text, a.IsTask, a.IsHeading, timeArg(a.CompletedAt), a.CreatedAt.String(), c.at.String())
	return err
}

// applyDelete trashes one node, stamped with this change set whatever the op
// says, and fills those values into a for recording (CONTRACTS §4, D-008, D-049).
func (c committer) applyDelete(id model.NodeID, cur row, a *model.DeleteFields) (any, error) {
	if !a.Deleted {
		return nil, invalidf("a delete must set deleted")
	}
	if cur.deletedAt != nil {
		return nil, invalidf("node is already in the Trash")
	}
	var liveChildren int
	if err := c.tx.QueryRowContext(c.ctx, `SELECT count(*) FROM nodes WHERE parent_id = ? AND deleted_at IS NULL`, string(id)).Scan(&liveChildren); err != nil {
		return nil, err
	}
	if liveChildren != 0 {
		return nil, invalidf("%d live children; children are deleted first", liveChildren)
	}
	if a.MergedInto != nil && !ids.Valid(string(*a.MergedInto)) {
		return nil, invalidf("merged_into %q", *a.MergedInto)
	}
	a.DeletedAt, a.DeletedBy = &c.at, &c.cs
	_, err := c.tx.ExecContext(c.ctx, `UPDATE nodes SET deleted_at = ?, deleted_by = ?, version = version + 1 WHERE id = ?`,
		c.at.String(), string(c.cs), string(id))
	return &model.DeleteFields{}, err
}

// applyRestore brings one node back. Its before comes from the row, except
// merged_into, which no row holds (D-049).
func (c committer) applyRestore(id model.NodeID, cur row, a *model.DeleteFields, given any) (any, error) {
	if a.Deleted || a.DeletedAt != nil || a.DeletedBy != nil {
		return nil, invalidf("a restore must clear deleted")
	}
	if cur.deletedAt == nil {
		return nil, invalidf("node isn't in the Trash")
	}
	if err := c.checkLiveParent(model.NodeID(cur.parent.String)); err != nil {
		return nil, err
	}
	before := &model.DeleteFields{Deleted: true, DeletedAt: cur.deletedAt, DeletedBy: cur.deletedBy}
	if g, ok := given.(*model.DeleteFields); ok {
		before.MergedInto = g.MergedInto
	}
	_, err := c.tx.ExecContext(c.ctx, `UPDATE nodes SET deleted_at = NULL, deleted_by = NULL, version = version + 1 WHERE id = ?`, string(id))
	return before, err
}

// applyMove sets parent and position. A trashed node may move, so a restore can
// place it under a live parent first (D-017).
func (c committer) applyMove(id model.NodeID, cur row, a *model.MoveFields) (any, error) {
	if !fracindex.Valid(a.Position) {
		return nil, invalidf("position %q", a.Position)
	}
	if err := c.checkLiveParent(a.ParentID); err != nil {
		return nil, err
	}
	var cycle int
	err := c.tx.QueryRowContext(c.ctx, `WITH RECURSIVE up(id) AS (
			SELECT ?
			UNION
			SELECT parent_id FROM nodes JOIN up ON nodes.id = up.id WHERE parent_id IS NOT NULL
		) SELECT count(*) FROM up WHERE id = ?`, string(a.ParentID), string(id)).Scan(&cycle)
	if err != nil {
		return nil, err
	}
	if cycle != 0 {
		return nil, invalidf("moving under %s would make a cycle", a.ParentID)
	}
	var position string
	if err := c.tx.QueryRowContext(c.ctx, `SELECT position FROM nodes WHERE id = ?`, string(id)).Scan(&position); err != nil {
		return nil, err
	}
	before := &model.MoveFields{ParentID: model.NodeID(cur.parent.String), Position: position}
	_, err = c.tx.ExecContext(c.ctx, `UPDATE nodes SET parent_id = ?, position = ?, version = version + 1 WHERE id = ?`,
		string(a.ParentID), a.Position, string(id))
	return before, err
}

// applyStar sets or clears a star, which bumps nothing (D-006).
func (c committer) applyStar(id model.NodeID, a *model.StarFields) (any, error) {
	var was sql.NullString
	err := c.tx.QueryRowContext(c.ctx, `SELECT starred_at FROM stars WHERE node_id = ?`, string(id)).Scan(&was)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	starredAt, err := parseTime(was)
	if err != nil {
		return nil, err
	}
	if a.StarredAt == nil {
		_, err = c.tx.ExecContext(c.ctx, `DELETE FROM stars WHERE node_id = ?`, string(id))
	} else {
		_, err = c.tx.ExecContext(c.ctx, `INSERT INTO stars (node_id, starred_at) VALUES (?, ?)
			ON CONFLICT (node_id) DO UPDATE SET starred_at = excluded.starred_at`, string(id), a.StarredAt.String())
	}
	return &model.StarFields{StarredAt: starredAt}, err
}

// applyPage creates, changes or deletes a page; page is nil to delete. It bumps
// the page's version and the node's (D-006). It returns nil before for a new page.
func (c committer) applyPage(id model.NodeID, page *model.PageFields) (any, error) {
	var was string
	err := c.tx.QueryRowContext(c.ctx, `SELECT markdown FROM pages WHERE node_id = ?`, string(id)).Scan(&was)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if page == nil {
		if !exists {
			return nil, invalidf("no page to delete")
		}
		_, err = c.tx.ExecContext(c.ctx, `DELETE FROM pages WHERE node_id = ?`, string(id))
	} else {
		_, err = c.tx.ExecContext(c.ctx, `INSERT INTO pages (node_id, markdown, updated_at) VALUES (?, ?, ?)
			ON CONFLICT (node_id) DO UPDATE SET markdown = excluded.markdown, updated_at = excluded.updated_at, version = version + 1`,
			string(id), page.Markdown, c.at.String())
	}
	if err != nil {
		return nil, err
	}
	if _, err := c.tx.ExecContext(c.ctx, `UPDATE nodes SET version = version + 1, updated_at = ? WHERE id = ?`, c.at.String(), string(id)); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return &model.PageFields{Markdown: was}, nil
}
