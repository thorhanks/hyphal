//go:build crash

package crashtest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// stateHash hashes the durable outline: every row LoadAll and LoadPages
// return, without the history position and the local last_* bookkeeping,
// which an import resets (A-006).
func stateHash(ctx context.Context, db *store.DB) (string, error) {
	d, err := db.LoadAll(ctx)
	if err != nil {
		return "", err
	}
	pages, err := db.LoadPages(ctx)
	if err != nil {
		return "", err
	}
	d.Seq = 0
	for k := range d.Meta {
		if strings.HasPrefix(k, "last_") {
			delete(d.Meta, k)
		}
	}
	slices.SortFunc(d.Nodes, func(a, b model.Node) int { return strings.Compare(string(a.ID), string(b.ID)) })
	b, err := json.Marshal(struct {
		Dump  *store.Dump
		Pages map[model.NodeID]model.Page
	}{d, pages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// changeSet is one change set as stored, with its op count.
type changeSet struct {
	ID  string `json:"id"`
	Ops int    `json:"ops"`
}

// rawDB opens the file with database/sql directly, for checks the store
// doesn't offer yet (P1-08 adds them).
func rawDB(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}
	return sql.Open("sqlite", u.String())
}

// changeSetsAfter lists the change sets stored after rowid, in order.
func changeSetsAfter(ctx context.Context, db *sql.DB, rowid int64) ([]changeSet, int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.rowid, c.id, (SELECT count(*) FROM ops o WHERE o.change_set_id = c.id)
		FROM change_sets c WHERE c.rowid > ? ORDER BY c.rowid`, rowid)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []changeSet
	last := rowid
	for rows.Next() {
		var cs changeSet
		if err := rows.Scan(&last, &cs.ID, &cs.Ops); err != nil {
			return nil, 0, err
		}
		out = append(out, cs)
	}
	return out, last, rows.Err()
}

// pragmaOK runs an integrity pragma and fails unless it returns only "ok".
func pragmaOK(ctx context.Context, db *sql.DB, pragma string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA "+pragma)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var problems []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return err
		}
		if s != "ok" {
			problems = append(problems, s)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s: %s", pragma, strings.Join(problems, "; "))
	}
	return nil
}

// checkFile runs integrity_check on the file and the tree invariants of
// CONTRACTS §8 on what it holds, and returns the state hash.
func checkFile(ctx context.Context, path string) (string, error) {
	raw, err := rawDB(path)
	if err != nil {
		return "", err
	}
	err = pragmaOK(ctx, raw, "integrity_check")
	_ = raw.Close()
	if err != nil {
		return "", err
	}
	db, err := store.Open(path, store.OpenOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	d, err := db.LoadAll(ctx)
	if err != nil {
		return "", err
	}
	if problems := core.NewTree(d).Check(); len(problems) > 0 {
		return "", fmt.Errorf("invariants: %v", problems)
	}
	return stateHash(ctx, db)
}

// record is one line of the side file. The child writes "pending" before a
// step that may commit and "done" after it; the parent writes "done" for
// change sets a kill left unrecorded, once it has checked them.
type record struct {
	T     string      `json:"t"` // "pending" or "done"
	Seal  bool        `json:"seal,omitempty"`
	Ops   int         `json:"ops,omitempty"` // pending: ops of the planned action, 0 for none
	Sets  []changeSet `json:"sets,omitempty"`
	Hash  string      `json:"hash,omitempty"`
	Rowid int64       `json:"rowid,omitempty"`
}

// sideFile appends records, each synced before the call returns.
type sideFile struct{ f *os.File }

func openSide(path string) (*sideFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &sideFile{f}, nil
}

func (s *sideFile) write(r record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *sideFile) Close() error { return s.f.Close() }

// readSide returns the change sets recorded as committed, the last "done"
// record, and the pending record after it, if the side file ends with one.
func readSide(path string) (committed []changeSet, last record, pending *record, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, record{}, nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	seen := false
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, record{}, nil, fmt.Errorf("side file: %w", err)
		}
		switch r.T {
		case "pending":
			pending = &r
		case "done":
			committed = append(committed, r.Sets...)
			last, pending, seen = r, nil, true
		default:
			return nil, record{}, nil, fmt.Errorf("side file: record %q", r.T)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, record{}, nil, err
	}
	if !seen {
		return nil, record{}, nil, errors.New("side file has no done record")
	}
	return committed, last, pending, nil
}

// allowedTrail lists the change sets a step can commit: a seal of open typing
// (one edit_text op), then the planned action.
func allowedTrail(p *record) [][]int {
	out := [][]int{{}}
	if p == nil {
		return out
	}
	if p.Seal {
		out = append(out, []int{1})
	}
	if p.Ops > 0 {
		out = append(out, []int{p.Ops})
		if p.Seal {
			out = append(out, []int{1, p.Ops})
		}
	}
	return out
}
