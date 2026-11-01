package core_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

var typingStart = testgen.FixtureTime

func after(d time.Duration) time.Time {
	return typingStart.Add(d)
}

// typingOutline returns an outline holding one empty bullet per name.
func typingOutline(t *testing.T, names ...string) (*core.Outline, *store.DB, []model.NodeID) {
	t.Helper()
	ctx := context.Background()
	db := testgen.TempDB(t)
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true, Clock: func() time.Time { return typingStart }})
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]model.NodeID, len(names))
	for i, name := range names {
		p := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{}, name, core.InsertFlags{})
		if _, err := o.Do(ctx, p); err != nil {
			t.Fatal(err)
		}
		nodes[i] = p.ChangeSet.Ops[0].NodeID
	}
	return o, db, nodes
}

func typeAt(t *testing.T, o *core.Outline, node model.NodeID, text string, at time.Duration) {
	t.Helper()
	if err := o.TypeInto(context.Background(), node, text, after(at)); err != nil {
		t.Fatal(err)
	}
}

// edits returns every edit_text op in order, one change set per group, as
// "cs<n> before -> after" with the change sets numbered from 1.
func edits(t *testing.T, db *store.DB) []string {
	t.Helper()
	conn, err := sql.Open("sqlite", db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	rows, err := conn.Query(`SELECT change_set_id, before, after FROM ops WHERE kind = 'edit_text' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	var sets []string
	for rows.Next() {
		var cs, before, afterText string
		if err := rows.Scan(&cs, &before, &afterText); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(sets, cs) {
			sets = append(sets, cs)
		}
		out = append(out, fmt.Sprintf("cs%d %s -> %s", len(sets), before, afterText))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func wantEdits(t *testing.T, db *store.DB, want ...string) {
	t.Helper()
	if got := edits(t, db); !slices.Equal(got, want) {
		t.Errorf("edits:\n%q\nwant\n%q", got, want)
	}
}

func textOf(t *testing.T, o *core.Outline, id model.NodeID) string {
	t.Helper()
	n, ok := o.Tree().Node(id)
	if !ok {
		t.Fatalf("no node %s", id)
	}
	return n.Text
}

func sealIfDue(t *testing.T, o *core.Outline, at time.Duration) bool {
	t.Helper()
	sealed, err := o.SealIfDue(context.Background(), after(at))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestTyping_SealsAfterIdle(t *testing.T) {
	o, db, n := typingOutline(t, "")
	typeAt(t, o, n[0], "a", 0)
	typeAt(t, o, n[0], "ab", 500*time.Millisecond)
	typeAt(t, o, n[0], "abc", time.Second)
	if got := textOf(t, o, n[0]); got != "abc" {
		t.Errorf("the tree shows %q before the seal", got)
	}
	wantEdits(t, db)
	if sealIfDue(t, o, 2999*time.Millisecond) {
		t.Error("sealed before 2 s of idle")
	}
	if !sealIfDue(t, o, 3*time.Second) {
		t.Error("didn't seal after 2 s of idle")
	}
	wantEdits(t, db, `cs1 {"text":""} -> {"text":"abc"}`)
	if sealIfDue(t, o, time.Minute) {
		t.Error("sealed with nothing open")
	}
}

func TestTyping_SealsAfterFiveSeconds(t *testing.T) {
	o, db, n := typingOutline(t, "")
	text := ""
	for at := time.Duration(0); at <= 6*time.Second; at += 250 * time.Millisecond {
		sealIfDue(t, o, at)
		text += "x"
		typeAt(t, o, n[0], text, at)
		sealed := len(edits(t, db)) == 1
		if want := at >= 5*time.Second; sealed != want {
			t.Fatalf("at %v: sealed %v", at, sealed)
		}
	}
	first := text[:20] // the keystrokes from 0 to 4.75 s
	wantEdits(t, db, fmt.Sprintf(`cs1 {"text":""} -> {"text":%q}`, first))
	p := o.PendingTyping()
	if p == nil || p.Before != first || !p.Opened.Equal(after(5*time.Second)) {
		t.Errorf("new operation: %+v", p)
	}
}

// A keystroke after a missed tick seals what was due, so history doesn't depend
// on when the tick ran.
func TestTyping_OverdueKeystroke(t *testing.T) {
	o, db, n := typingOutline(t, "")
	typeAt(t, o, n[0], "a", 0)
	typeAt(t, o, n[0], "ab", 3*time.Second)
	wantEdits(t, db, `cs1 {"text":""} -> {"text":"a"}`)
	if p := o.PendingTyping(); p == nil || p.Before != "a" || p.Text != "ab" {
		t.Errorf("new operation: %+v", p)
	}
}

func TestTyping_SealsOnNodeChange(t *testing.T) {
	o, db, n := typingOutline(t, "", "")
	typeAt(t, o, n[0], "in A", 0)
	typeAt(t, o, n[1], "in B", 100*time.Millisecond)
	wantEdits(t, db, `cs1 {"text":""} -> {"text":"in A"}`)
	if p := o.PendingTyping(); p == nil || p.Node != n[1] {
		t.Errorf("pending: %+v", p)
	}
	if _, _, err := o.Seal(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantEdits(t, db, `cs1 {"text":""} -> {"text":"in A"}`, `cs2 {"text":""} -> {"text":"in B"}`)
}

func TestTyping_NoOpSeal(t *testing.T) {
	o, db, n := typingOutline(t, "same")
	version := func() int64 {
		node, _ := o.Tree().Node(n[0])
		return node.Version
	}
	was := version()
	typeAt(t, o, n[0], "samex", 0)
	typeAt(t, o, n[0], "same", 100*time.Millisecond)
	id, sealed, err := o.Seal(context.Background())
	if err != nil || id != "" || !sealed {
		t.Errorf("Seal: %q %v %v", id, sealed, err)
	}
	wantEdits(t, db)
	if version() != was {
		t.Error("a no-op seal bumped the version")
	}
	if o.PendingTyping() != nil {
		t.Error("still pending")
	}
}

func TestTyping_Pending(t *testing.T) {
	ctx := context.Background()
	o, _, n := typingOutline(t, "x")
	if o.PendingTyping() != nil {
		t.Error("pending before typing")
	}
	typeAt(t, o, n[0], "x", 0)
	if o.PendingTyping() != nil {
		t.Error("pending after typing that changed nothing")
	}
	typeAt(t, o, n[0], "xy", time.Second)
	p := o.PendingTyping()
	if p == nil || p.Node != n[0] || p.Before != "x" || p.Text != "xy" {
		t.Fatalf("pending: %+v", p)
	}
	p.Before = "changed by the caller"
	if o.PendingTyping().Before != "x" {
		t.Error("the caller reached the open typing")
	}
	id, sealed, err := o.Seal(ctx)
	if err != nil || id == "" || !sealed {
		t.Errorf("Seal: %q %v %v", id, sealed, err)
	}
	if o.PendingTyping() != nil {
		t.Error("pending after the seal")
	}
	if id, sealed, err := o.Seal(ctx); err != nil || id != "" || sealed {
		t.Errorf("Seal with nothing open: %q %v %v", id, sealed, err)
	}
}

// A sealed tree equals a reload, including the version and time the seal wrote.
func TestTyping_SealMatchesReload(t *testing.T) {
	ctx := context.Background()
	o, db, n := typingOutline(t, "")
	typeAt(t, o, n[0], "abc", 0)
	sealIfDue(t, o, 2*time.Second)
	fresh, err := core.OpenOutline(ctx, db, core.OutlineOptions{Source: "ui", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := o.Tree().Node(n[0])
	want, _ := fresh.Tree().Node(n[0])
	if got.Text != want.Text || got.Version != want.Version || got.UpdatedAt.String() != want.UpdatedAt.String() {
		t.Errorf("tree %+v, reload %+v", got, want)
	}
	if got.UpdatedAt.String() != model.NewTime(after(2*time.Second)).String() {
		t.Errorf("sealed at %s", got.UpdatedAt)
	}
}

func TestTyping_DoSealsFirst(t *testing.T) {
	ctx := context.Background()
	o, db, n := typingOutline(t, "", "")
	typeAt(t, o, n[0], "typed", 0)

	// A no-op changes nothing, so the typing stays open.
	if _, err := o.Do(ctx, core.Plan{NoOp: true}); err != nil {
		t.Fatal(err)
	}
	if o.PendingTyping() == nil {
		t.Error("a no-op sealed the typing")
	}
	if _, err := o.Do(ctx, core.PlanSetTask(o.Ctx(), n[:1], true)); err != nil {
		t.Fatal(err)
	}
	wantEdits(t, db, `cs1 {"text":""} -> {"text":"typed"}`)
	if o.PendingTyping() != nil {
		t.Error("a structural action left the typing open")
	}
}

func TestTyping_Refused(t *testing.T) {
	ctx := context.Background()
	o, _, n := typingOutline(t, "keep", "gone")
	if _, err := o.Do(ctx, core.PlanDelete(o.Ctx(), n[1:])); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		node model.NodeID
		text string
	}{
		"line break":     {n[0], "a\nb"},
		"leading marker": {n[0], "## a"},
		"trashed":        {n[1], "a"},
		"root":           {o.Tree().Root(), "a"},
		"unknown":        {"nope", "a"},
	} {
		if err := o.TypeInto(ctx, c.node, c.text, typingStart); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := textOf(t, o, n[0]); got != "keep" {
		t.Errorf("text is %q", got)
	}
	if o.PendingTyping() != nil {
		t.Error("refused text opened typing")
	}
}

// A failed write keeps the typing open and the text in the tree (§8.1).
func TestTyping_FailedSealStaysOpen(t *testing.T) {
	ctx := context.Background()
	o, db, n := typingOutline(t, "", "")
	typeAt(t, o, n[0], "abc", 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.Seal(ctx); err == nil {
		t.Error("Seal succeeded on a closed database")
	}
	if _, err := o.SealIfDue(ctx, after(time.Minute)); err == nil {
		t.Error("SealIfDue succeeded on a closed database")
	}
	if err := o.TypeInto(ctx, n[1], "other", after(time.Second)); err == nil {
		t.Error("TypeInto left A's typing unwritten")
	}
	if p := o.PendingTyping(); p == nil || p.Node != n[0] || p.Text != "abc" {
		t.Errorf("pending: %+v", p)
	}
	if got := textOf(t, o, n[0]); got != "abc" {
		t.Errorf("text is %q", got)
	}
	if got := textOf(t, o, n[1]); got != "" {
		t.Errorf("B's text is %q", got)
	}
}

// Another connection's change stays in history and the typing lands after it
// (§7.3).
func TestTyping_ConcurrentEdit(t *testing.T) {
	ctx := context.Background()
	o, db, n := typingOutline(t, "start")
	typeAt(t, o, n[0], "typed", 0)

	other, err := store.Open(db.Path(), store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	cli, err := core.OpenOutline(ctx, other, core.OutlineOptions{Source: "cli", Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Do(ctx, core.PlanEditText(cli.Ctx(), n[0], "from cli")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	wantEdits(t, db,
		`cs1 {"text":"start"} -> {"text":"from cli"}`,
		`cs2 {"text":"from cli"} -> {"text":"typed"}`)
}
