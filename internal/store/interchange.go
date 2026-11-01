package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/thorhanks/hyphal/internal/fracindex"
	"github.com/thorhanks/hyphal/internal/ids"
	"github.com/thorhanks/hyphal/internal/model"
)

const (
	exportFormat  = "hyphal-export" // archtest:allow-app-name (D-005)
	exportVersion = 1
)

var (
	// ErrNotEmpty means a JSON import without --replace found a database holding
	// more than Home (§7.5).
	ErrNotEmpty = errors.New("database is not empty; --replace replaces its contents")
	// ErrParse means an import file can't be read or breaks an invariant
	// (CONTRACTS §6, §8). Nothing was changed.
	ErrParse = errors.New("cannot import file")
)

// ImportMode says what ImportJSON does with an existing database (§7.5).
type ImportMode int

// The import modes.
const (
	// ImportEmpty refuses a database holding anything besides Home, with ErrNotEmpty.
	ImportEmpty ImportMode = iota
	// ImportReplace takes a pre-import snapshot, then replaces the contents.
	ImportReplace
)

// exportFile is the JSON export format (CONTRACTS §6). Rows hold columns as
// stored, so an export and its import are exact.
type exportFile struct {
	Format        string            `json:"format"`
	FormatVersion int               `json:"format_version"`
	SchemaVersion int               `json:"schema_version"`
	ExportedBy    string            `json:"exported_by"`
	ExportedAt    string            `json:"exported_at"`
	Meta          map[string]string `json:"meta"`
	Nodes         []exportNode      `json:"nodes"`
	Pages         []exportPage      `json:"pages"`
	Stars         []exportStar      `json:"stars"`
	ViewState     []exportView      `json:"view_state"`
	UIState       []exportUI        `json:"ui_state"`
	History       *exportHistory    `json:"history,omitempty"`
}

type exportNode struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
	Position    string  `json:"position"`
	Text        string  `json:"text"`
	IsTask      bool    `json:"is_task"`
	IsHeading   bool    `json:"is_heading"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	Version     int64   `json:"version"`
	DeletedAt   *string `json:"deleted_at"`
	DeletedBy   *string `json:"deleted_by"`
}

type exportPage struct {
	NodeID    string `json:"node_id"`
	Markdown  string `json:"markdown"`
	UpdatedAt string `json:"updated_at"`
	Version   int64  `json:"version"`
}

type exportStar struct {
	NodeID    string `json:"node_id"`
	StarredAt string `json:"starred_at"`
}

type exportView struct {
	NodeID    string `json:"node_id"`
	Collapsed bool   `json:"collapsed"`
}

type exportUI struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type exportHistory struct {
	ChangeSets []exportChangeSet `json:"change_sets"`
	Ops        []exportOp        `json:"ops"`
}

type exportChangeSet struct {
	ID          string  `json:"id"`
	At          string  `json:"at"`
	Source      string  `json:"source"`
	Description *string `json:"description"`
	UndoOf      *string `json:"undo_of"`
}

type exportOp struct {
	ID          int64           `json:"id"`
	ChangeSetID string          `json:"change_set_id"`
	NodeID      string          `json:"node_id"`
	Kind        string          `json:"kind"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
}

// exportedMeta reports whether a meta key belongs in an export (CONTRACTS §6).
func exportedMeta(key string) bool {
	return key != "schema_version" && key != "description" && !strings.HasPrefix(key, "last_")
}

// ExportJSON writes the lossless JSON export, read at one moment, with arrays
// sorted by primary key (§7.5, CONTRACTS §6). History is included only if asked.
func (db *DB) ExportJSON(ctx context.Context, w io.Writer, withHistory bool, exportedBy string) error {
	f, err := db.readExport(ctx, withHistory)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	f.ExportedBy = exportedBy
	f.ExportedAt = model.NewTime(db.opts.Clock()).String()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // bullet text keeps its <red> tags readable
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

func (db *DB) readExport(ctx context.Context, withHistory bool) (*exportFile, error) {
	tx, err := db.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	f := &exportFile{
		Format: exportFormat, FormatVersion: exportVersion,
		Meta: map[string]string{}, Nodes: []exportNode{}, Pages: []exportPage{},
		Stars: []exportStar{}, ViewState: []exportView{}, UIState: []exportUI{},
	}
	version, _, err := metaValue(ctx, tx, "schema_version")
	if err != nil {
		return nil, err
	}
	if f.SchemaVersion, err = strconv.Atoi(version); err != nil {
		return nil, fmt.Errorf("schema_version %q: %w", version, err)
	}
	if err := eachRow(ctx, tx, `SELECT key, coalesce(value, '') FROM meta`, func(rows *sql.Rows) error {
		var key, value string
		err := rows.Scan(&key, &value)
		if exportedMeta(key) {
			f.Meta[key] = value
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, tx, `SELECT id, parent_id, position, text, is_task, is_heading, completed_at,
		created_at, updated_at, version, deleted_at, deleted_by FROM nodes ORDER BY id`, func(rows *sql.Rows) error {
		var n exportNode
		err := rows.Scan(&n.ID, &n.ParentID, &n.Position, &n.Text, &n.IsTask, &n.IsHeading, &n.CompletedAt,
			&n.CreatedAt, &n.UpdatedAt, &n.Version, &n.DeletedAt, &n.DeletedBy)
		f.Nodes = append(f.Nodes, n)
		return err
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, tx, `SELECT node_id, markdown, updated_at, version FROM pages ORDER BY node_id`, func(rows *sql.Rows) error {
		var p exportPage
		err := rows.Scan(&p.NodeID, &p.Markdown, &p.UpdatedAt, &p.Version)
		f.Pages = append(f.Pages, p)
		return err
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, tx, `SELECT node_id, starred_at FROM stars ORDER BY node_id`, func(rows *sql.Rows) error {
		var s exportStar
		err := rows.Scan(&s.NodeID, &s.StarredAt)
		f.Stars = append(f.Stars, s)
		return err
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, tx, `SELECT node_id, collapsed FROM view_state ORDER BY node_id`, func(rows *sql.Rows) error {
		var v exportView
		err := rows.Scan(&v.NodeID, &v.Collapsed)
		f.ViewState = append(f.ViewState, v)
		return err
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, tx, `SELECT key, value FROM ui_state ORDER BY key`, func(rows *sql.Rows) error {
		var key, value string
		err := rows.Scan(&key, &value)
		f.UIState = append(f.UIState, exportUI{key, json.RawMessage(value)})
		return err
	}); err != nil {
		return nil, err
	}
	if withHistory {
		if f.History, err = readHistory(ctx, tx); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func readHistory(ctx context.Context, tx *sql.Tx) (*exportHistory, error) {
	h := &exportHistory{ChangeSets: []exportChangeSet{}, Ops: []exportOp{}}
	if err := eachRow(ctx, tx, `SELECT id, at, source, description, undo_of FROM change_sets ORDER BY id`, func(rows *sql.Rows) error {
		var c exportChangeSet
		err := rows.Scan(&c.ID, &c.At, &c.Source, &c.Description, &c.UndoOf)
		h.ChangeSets = append(h.ChangeSets, c)
		return err
	}); err != nil {
		return nil, err
	}
	err := eachRow(ctx, tx, `SELECT id, change_set_id, node_id, kind, before, after FROM ops ORDER BY id`, func(rows *sql.Rows) error {
		var o exportOp
		var before, after sql.NullString
		err := rows.Scan(&o.ID, &o.ChangeSetID, &o.NodeID, &o.Kind, &before, &after)
		o.Before, o.After = rawOrNull(before), rawOrNull(after)
		h.Ops = append(h.Ops, o)
		return err
	})
	return h, err
}

// rawOrNull is the inverse of nullIfJSONNull.
func rawOrNull(s sql.NullString) json.RawMessage {
	if !s.Valid {
		return json.RawMessage("null")
	}
	return json.RawMessage(s.String)
}

// ImportJSON replaces the database's contents with a JSON export, in one
// transaction, after validating the whole file (§7.5, CONTRACTS §6, §8). Errors:
// ErrReadOnly, ErrParse (the file is bad), ErrNotEmpty and ErrBusy; on any of
// them nothing changes. Local bookkeeping stays: schema_version, description and
// the last_* meta keys are never imported (A-006).
func (db *DB) ImportJSON(ctx context.Context, r io.Reader, mode ImportMode) error {
	if db.readOnly != ReadOnlyNone {
		return ErrReadOnly
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	f, err := parseExport(data)
	if err != nil {
		return err
	}
	if err := validateExport(f); err != nil {
		return err
	}
	err = db.writeImport(ctx, f, mode)
	if isBusy(err) {
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return err
}

func parsef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrParse, fmt.Sprintf(format, args...))
}

// parseExport reads the header first, so a file from a newer format is reported
// as that rather than as an unknown field. Fields it doesn't know are refused,
// since dropping them would lose data.
func parseExport(data []byte) (*exportFile, error) {
	var header struct {
		Format        string `json:"format"`
		FormatVersion int    `json:"format_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, parsef("not valid JSON: %v", err)
	}
	if header.Format != exportFormat {
		return nil, parsef("format is %q, not %q", header.Format, exportFormat)
	}
	if header.FormatVersion != exportVersion {
		return nil, parsef("format_version %d is not known", header.FormatVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	f := &exportFile{}
	if err := dec.Decode(f); err != nil {
		return nil, parsef("%v", err)
	}
	return f, nil
}

// compactRaw undoes the export's indentation, so stored JSON matches the source
// row. A missing field is an error; JSON null arrives here as the text null.
func compactRaw(what string, raw json.RawMessage) (json.RawMessage, error) {
	if raw == nil {
		return nil, parsef("%s is missing", what)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return nil, parsef("%s: %v", what, err)
	}
	return out.Bytes(), nil
}

func checkTimestamp(what, s string) error {
	t, err := model.ParseTime(s)
	if err != nil || t.String() != s {
		return parsef("%s: timestamp %q isn't in the stored form", what, s)
	}
	return nil
}

func checkOptionalTimestamp(what string, s *string) error {
	if s == nil {
		return nil
	}
	return checkTimestamp(what, *s)
}

// checkUnique records id in seen and refuses a repeat.
func checkUnique[K comparable](table string, seen map[K]bool, id K) error {
	if seen[id] {
		return parsef("%s: %v appears twice", table, id)
	}
	seen[id] = true
	return nil
}

// validateExport checks the file against CONTRACTS §6 and the invariants of §8,
// and compacts its embedded JSON.
func validateExport(f *exportFile) error {
	if f.SchemaVersion < 1 || f.SchemaVersion > knownVersion() {
		return parsef("schema_version %d is not known", f.SchemaVersion)
	}
	if got := f.Meta["format_id"]; got != formatID {
		return parsef("meta.format_id is %q, not %q", got, formatID)
	}
	nodes, err := validateNodes(f)
	if err != nil {
		return err
	}
	if err := validateRefs(f, nodes); err != nil {
		return err
	}
	if f.History != nil {
		return validateHistory(f.History)
	}
	return nil
}

// validateNodes checks invariants 1 to 4 and 6 to 9, and returns the nodes by ID.
func validateNodes(f *exportFile) (map[string]*exportNode, error) {
	nodes := make(map[string]*exportNode, len(f.Nodes))
	root := f.Meta["root_id"]
	roots := 0
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if !ids.Valid(n.ID) {
			return nil, parsef("node ID %q is invalid", n.ID)
		}
		if nodes[n.ID] != nil {
			return nil, parsef("node %s appears twice", n.ID)
		}
		nodes[n.ID] = n
		if err := validateNode(n); err != nil {
			return nil, err
		}
		if n.ParentID == nil {
			roots++
			if n.ID != root {
				return nil, parsef("node %s has no parent but meta.root_id is %q", n.ID, root)
			}
			if n.Text != "" || n.CompletedAt != nil || n.DeletedAt != nil {
				return nil, parsef("root %s must be empty, open and live", n.ID)
			}
		}
	}
	if roots != 1 {
		return nil, parsef("%d nodes have no parent, not one", roots)
	}
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.ParentID == nil {
			continue
		}
		parent := nodes[*n.ParentID]
		if parent == nil {
			return nil, parsef("node %s: parent %q doesn't exist", n.ID, *n.ParentID)
		}
		if n.DeletedAt == nil && parent.DeletedAt != nil {
			return nil, parsef("node %s is live under trashed parent %s", n.ID, parent.ID)
		}
	}
	if id := findCycle(f.Nodes, nodes); id != "" {
		return nil, parsef("node %s is in a cycle", id)
	}
	if inbox, ok := f.Meta["inbox_id"]; ok && nodes[inbox] == nil {
		return nil, parsef("meta.inbox_id %q doesn't exist", inbox)
	}
	return nodes, nil
}

func validateNode(n *exportNode) error {
	what := "node " + n.ID
	if err := checkText(n.Text); err != nil {
		return parsef("%s: %v", what, err)
	}
	if !fracindex.Valid(n.Position) {
		return parsef("%s: position %q is invalid", what, n.Position)
	}
	if n.Version < 1 {
		return parsef("%s: version %d is below 1", what, n.Version)
	}
	if (n.DeletedAt != nil) != (n.DeletedBy != nil) {
		return parsef("%s: deleted_at and deleted_by disagree", what)
	}
	if n.DeletedBy != nil && !ids.Valid(*n.DeletedBy) {
		return parsef("%s: deleted_by %q is invalid", what, *n.DeletedBy)
	}
	if err := checkTimestamp(what+" created_at", n.CreatedAt); err != nil {
		return err
	}
	if err := checkTimestamp(what+" updated_at", n.UpdatedAt); err != nil {
		return err
	}
	if err := checkOptionalTimestamp(what+" completed_at", n.CompletedAt); err != nil {
		return err
	}
	return checkOptionalTimestamp(what+" deleted_at", n.DeletedAt)
}

// findCycle returns a node on a parent cycle, or "". Every parent exists by now.
func findCycle(list []exportNode, nodes map[string]*exportNode) string {
	const onPath, done = 1, 2
	state := make(map[string]int, len(list))
	for i := range list {
		var path []string
		id := list[i].ID
		for state[id] == 0 {
			state[id] = onPath
			path = append(path, id)
			parent := nodes[id].ParentID
			if parent == nil {
				break
			}
			id = *parent
		}
		if state[id] == onPath && nodes[id].ParentID != nil {
			return id
		}
		for _, p := range path {
			state[p] = done
		}
	}
	return ""
}

// validateRefs checks the rows that reference nodes (invariant 5) and ui_state.
func validateRefs(f *exportFile, nodes map[string]*exportNode) error {
	root := f.Meta["root_id"]
	ref := func(table, id string, rootAllowed bool) error {
		if nodes[id] == nil {
			return parsef("%s: node %q doesn't exist", table, id)
		}
		if id == root && !rootAllowed {
			return parsef("%s: the root can't have one", table)
		}
		return nil
	}
	seen := map[string]bool{}
	for _, p := range f.Pages {
		if err := checkUnique("pages", seen, p.NodeID); err != nil {
			return err
		}
		if err := ref("pages", p.NodeID, false); err != nil {
			return err
		}
		if p.Version < 1 {
			return parsef("pages: %s: version %d is below 1", p.NodeID, p.Version)
		}
		if err := checkTimestamp("page "+p.NodeID+" updated_at", p.UpdatedAt); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for _, s := range f.Stars {
		if err := checkUnique("stars", seen, s.NodeID); err != nil {
			return err
		}
		if err := ref("stars", s.NodeID, false); err != nil {
			return err
		}
		if err := checkTimestamp("star "+s.NodeID, s.StarredAt); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for _, v := range f.ViewState {
		if err := checkUnique("view_state", seen, v.NodeID); err != nil {
			return err
		}
		if err := ref("view_state", v.NodeID, true); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for i := range f.UIState {
		u := &f.UIState[i]
		if err := checkUnique("ui_state", seen, u.Key); err != nil {
			return err
		}
		var err error
		if u.Value, err = compactRaw("ui_state "+u.Key, u.Value); err != nil {
			return err
		}
	}
	return nil
}

// validateHistory checks change sets and ops. Ops keep their IDs, since those
// order them (CONTRACTS §5). A deleted_by or undo_of may name a change set the
// file doesn't hold: history can be purged or left out of the export (§8.4).
func validateHistory(h *exportHistory) error {
	sets := map[string]bool{}
	for _, c := range h.ChangeSets {
		if !ids.Valid(c.ID) {
			return parsef("change set ID %q is invalid", c.ID)
		}
		if err := checkUnique("change_sets", sets, c.ID); err != nil {
			return err
		}
		if err := checkTimestamp("change set "+c.ID, c.At); err != nil {
			return err
		}
		if !validSource(c.Source) {
			return parsef("change set %s: source %q is invalid", c.ID, c.Source)
		}
		if c.UndoOf != nil && !ids.Valid(*c.UndoOf) {
			return parsef("change set %s: undo_of %q is invalid", c.ID, *c.UndoOf)
		}
	}
	seen := map[int64]bool{}
	for i := range h.Ops {
		o := &h.Ops[i]
		what := fmt.Sprintf("op %d", o.ID)
		if err := checkUnique("ops", seen, o.ID); err != nil {
			return err
		}
		if !sets[o.ChangeSetID] {
			return parsef("%s: change set %q doesn't exist", what, o.ChangeSetID)
		}
		if !ids.Valid(o.NodeID) {
			return parsef("%s: node ID %q is invalid", what, o.NodeID)
		}
		var err error
		if o.Before, err = compactRaw(what+" before", o.Before); err != nil {
			return err
		}
		if o.After, err = compactRaw(what+" after", o.After); err != nil {
			return err
		}
		op := model.Op{NodeID: model.NodeID(o.NodeID), Kind: model.OpKind(o.Kind), Before: o.Before, After: o.After}
		if _, err := op.DecodeBefore(); err != nil {
			return parsef("%s: %v", what, err)
		}
		if _, err := op.DecodeAfter(); err != nil {
			return parsef("%s: %v", what, err)
		}
	}
	return nil
}

// writeImport swaps the contents in one transaction. It takes the write lock
// before the snapshot, so no other writer's change can fall between the two.
func (db *DB) writeImport(ctx context.Context, f *exportFile, mode ImportMode) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if mode == ImportReplace {
		if _, err := db.snapshotTo(ctx, "pre-import"); err != nil {
			return fmt.Errorf("snapshot before importing: %w", err)
		}
	} else if err := checkEmpty(ctx, tx); err != nil {
		return err
	}
	// Nodes arrive sorted by ID, so children can precede their parents.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM ops`, `DELETE FROM change_sets`, `DELETE FROM ui_state`, `DELETE FROM view_state`,
		`DELETE FROM stars`, `DELETE FROM pages`, `DELETE FROM nodes`,
		`DELETE FROM meta WHERE key NOT IN ('schema_version', 'description') AND key NOT LIKE 'last\_%' ESCAPE '\'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("import: %w", err)
		}
	}
	if err := insertRows(ctx, tx, f); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	return tx.Commit()
}

// checkEmpty allows only Home. History counts: a database whose bullets were all
// purged still holds a record that an import would erase (A-006).
func checkEmpty(ctx context.Context, tx *sql.Tx) error {
	var nodes, sets int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM nodes), (SELECT count(*) FROM change_sets)`).Scan(&nodes, &sets); err != nil {
		return err
	}
	if nodes > 1 || sets > 0 {
		return ErrNotEmpty
	}
	return nil
}

func insertRows(ctx context.Context, tx *sql.Tx, f *exportFile) error {
	for key, value := range f.Meta {
		if !exportedMeta(key) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, key, value); err != nil {
			return err
		}
	}
	err := errors.Join(
		insertAll(ctx, tx, `INSERT INTO nodes (id, parent_id, position, text, is_task, is_heading, completed_at,
			created_at, updated_at, version, deleted_at, deleted_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.Nodes,
			func(n exportNode) []any {
				return []any{n.ID, n.ParentID, n.Position, n.Text, n.IsTask, n.IsHeading, n.CompletedAt,
					n.CreatedAt, n.UpdatedAt, n.Version, n.DeletedAt, n.DeletedBy}
			}),
		insertAll(ctx, tx, `INSERT INTO pages (node_id, markdown, updated_at, version) VALUES (?, ?, ?, ?)`, f.Pages,
			func(p exportPage) []any { return []any{p.NodeID, p.Markdown, p.UpdatedAt, p.Version} }),
		insertAll(ctx, tx, `INSERT INTO stars (node_id, starred_at) VALUES (?, ?)`, f.Stars,
			func(s exportStar) []any { return []any{s.NodeID, s.StarredAt} }),
		insertAll(ctx, tx, `INSERT INTO view_state (node_id, collapsed) VALUES (?, ?)`, f.ViewState,
			func(v exportView) []any { return []any{v.NodeID, v.Collapsed} }),
		insertAll(ctx, tx, `INSERT INTO ui_state (key, value) VALUES (?, ?)`, f.UIState,
			func(u exportUI) []any { return []any{u.Key, string(u.Value)} }),
	)
	if err != nil || f.History == nil {
		return err
	}
	return errors.Join(
		insertAll(ctx, tx, `INSERT INTO change_sets (id, at, source, description, undo_of) VALUES (?, ?, ?, ?, ?)`, f.History.ChangeSets,
			func(c exportChangeSet) []any { return []any{c.ID, c.At, c.Source, c.Description, c.UndoOf} }),
		insertAll(ctx, tx, `INSERT INTO ops (id, change_set_id, node_id, kind, before, after) VALUES (?, ?, ?, ?, ?, ?)`, f.History.Ops,
			func(o exportOp) []any {
				return []any{o.ID, o.ChangeSetID, o.NodeID, o.Kind, nullIfJSONNull(o.Before), nullIfJSONNull(o.After)}
			}),
	)
}

func insertAll[T any](ctx context.Context, tx *sql.Tx, query string, rows []T, args func(T) []any) error {
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, args(row)...); err != nil {
			return err
		}
	}
	return nil
}
