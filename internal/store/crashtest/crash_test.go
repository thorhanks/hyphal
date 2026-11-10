//go:build crash

package crashtest

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

// iterations returns the number of kills: def in CI, CRASH_ITERATIONS when
// set (nightly runs 2,000 of the typing test, TESTING §7).
func iterations(t *testing.T, def int) int {
	if s := os.Getenv("CRASH_ITERATIONS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			t.Fatalf("CRASH_ITERATIONS=%q", s)
		}
		return max(n, def)
	}
	return def
}

// childEnv points the child at temp directories only (plan/README.md §7).
func childEnv(t *testing.T, extra ...string) []string {
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_RUNTIME_DIR=" + filepath.Join(home, "run"),
		"HYPHAL_DB=" + filepath.Join(home, "unused.db"),
	}
	return append(append(os.Environ(), env...), extra...)
}

// killLoop starts the child n times and kills it with SIGKILL after a random
// delay in [lo, hi), calling check after each kill. A child that exits by
// itself has failed.
func killLoop(t *testing.T, n int, lo, hi time.Duration, env []string, check func(i int)) {
	t.Helper()
	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	for i := range n {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(slices.Clone(env), envSeed+"="+strconv.Itoa(i))
		var stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stderr, &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		delay := lo + time.Duration(r.Int64N(int64(hi-lo)))
		select {
		case err := <-exited:
			t.Fatalf("iteration %d: the child exited by itself: %v\n%s", i, err, stderr.String())
		case <-time.After(delay):
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := <-exited; err == nil {
			t.Fatalf("iteration %d: the child finished before the kill", i)
		}
		check(i)
	}
}

// TestCrash_Typing kills a child doing structural actions and typing (AC1).
// After each kill the file passes integrity_check and the invariants, every
// change set recorded as committed is there with all its ops, any change set
// the kill left unrecorded is a whole one the interrupted step could make, and
// otherwise the outline equals the state after the last committed change set,
// so only unsealed typing is lost (§7.3, TESTING §7).
func TestCrash_Typing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "outline.db")
	sidePath := filepath.Join(dir, "side.jsonl")

	db, err := store.Open(dbPath, store.OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Sync: true, Source: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	testgen.Actions(ctx, t, rand.New(rand.NewPCG(1, 2)), o, 30)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := appendDone(ctx, dbPath, sidePath, 0); err != nil {
		t.Fatal(err)
	}

	var verified, unrecorded int
	start := committedCount(t, sidePath)
	env := childEnv(t, envMode+"=typing", envDB+"="+dbPath, envSide+"="+sidePath)
	killLoop(t, iterations(t, 200), 5*time.Millisecond, 500*time.Millisecond, env, func(i int) {
		hash, err := checkFile(ctx, dbPath)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		committed, last, pending, err := readSide(sidePath)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := rawDB(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := changeSetsAfter(ctx, raw, 0)
		_ = raw.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(stored) < len(committed) || !slices.Equal(stored[:len(committed)], committed) {
			t.Fatalf("iteration %d: a committed change set is missing or changed\nrecorded %v\nstored   %v", i, committed, stored)
		}
		trail := stored[len(committed):]
		if !trailAllowed(pending, trail) {
			t.Fatalf("iteration %d: after %+v the database holds %v: a partial or unexpected change set", i, pending, trail)
		}
		if len(trail) == 0 {
			if hash != last.Hash {
				t.Fatalf("iteration %d: the outline differs from the state after the last committed change set", i)
			}
			verified++
			return
		}
		// The kill fell after the commit and before the child recorded it.
		unrecorded++
		if err := appendDone(ctx, dbPath, sidePath, last.Rowid); err != nil {
			t.Fatal(err)
		}
	})
	total := committedCount(t, sidePath) - start
	t.Logf("%d change sets committed; %d kills compared the whole state, %d fell between a commit and its record", total, verified, unrecorded)
	if total < 200 {
		t.Errorf("only %d change sets committed: the child did too little to test anything", total)
	}
}

// appendDone records every change set after rowid, and the state hash, as
// committed.
func appendDone(ctx context.Context, dbPath, sidePath string, rowid int64) error {
	hash, err := checkFile(ctx, dbPath)
	if err != nil {
		return err
	}
	raw, err := rawDB(dbPath)
	if err != nil {
		return err
	}
	sets, last, err := changeSetsAfter(ctx, raw, rowid)
	_ = raw.Close()
	if err != nil {
		return err
	}
	side, err := openSide(sidePath)
	if err != nil {
		return err
	}
	defer func() { _ = side.Close() }()
	return side.write(record{T: "done", Sets: sets, Hash: hash, Rowid: last})
}

func committedCount(t *testing.T, sidePath string) int {
	committed, _, _, err := readSide(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	return len(committed)
}

// TestCrash_ImportReplace kills a child replacing the outline with one of two
// exports, over and over (AC2). The file always holds one of them whole.
func TestCrash_ImportReplace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	exports := makeExports(t, dir)
	hashes := make([]string, len(exports))
	for i, f := range exports {
		hashes[i] = importInto(t, filepath.Join(t.TempDir(), "outline.db"), f)
	}
	if hashes[0] == hashes[1] {
		t.Fatal("the two exports hold the same outline")
	}
	dbPath := filepath.Join(dir, "live", "outline.db")
	if got := importInto(t, dbPath, exports[0]); got != hashes[0] {
		t.Fatal("importing the same export twice gave different outlines")
	}

	seen := map[string]int{}
	tmps := 0
	env := childEnv(t, envMode+"=import", envDB+"="+dbPath, envFiles+"="+strings.Join(exports, string(filepath.ListSeparator)))
	killLoop(t, iterations(t, 40), 5*time.Millisecond, 300*time.Millisecond, env, func(i int) {
		hash, err := checkFile(ctx, dbPath)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if !slices.Contains(hashes, hash) {
			t.Fatalf("iteration %d: the outline is neither export: an import was left half done", i)
		}
		seen[hash]++
		tmps += checkSnapshots(t, filepath.Join(filepath.Dir(dbPath), "snapshots"))
	})
	t.Logf("first export %d times, second %d times; %d interrupted pre-import snapshots", seen[hashes[0]], seen[hashes[1]], tmps)
	if seen[hashes[1]] == 0 {
		t.Error("no import finished: the kills came too early to test anything")
	}
}

// makeExports writes two JSON exports, with history, of different outlines.
func makeExports(t *testing.T, dir string) []string {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(dir, "source", "outline.db"), store.OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Sync: true, Source: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(3, 4))
	var files []string
	for i := range 2 {
		testgen.Actions(ctx, t, r, o, 150)
		path := filepath.Join(dir, fmt.Sprintf("export-%d.json", i))
		var b bytes.Buffer
		if err := db.ExportJSON(ctx, &b, true, "crashtest"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	return files
}

// importInto imports an export into a new database and returns its hash.
func importInto(t *testing.T, dbPath, export string) string {
	ctx := context.Background()
	db, err := store.Open(dbPath, store.OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(export)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := db.ImportJSON(ctx, f, store.ImportEmpty); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	hash, err := checkFile(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// checkSnapshots fails unless every listed snapshot passes quick_check and
// no .tmp file is listed (§8.5). It then empties the directory, so the disk
// doesn't fill, and returns how many interrupted .tmp copies it found.
func checkSnapshots(t *testing.T, dir string) int {
	t.Helper()
	ctx := context.Background()
	list, err := store.ListSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if strings.Contains(s.Name, ".tmp") {
			t.Fatalf("partial snapshot %s is listed", s.Name)
		}
		raw, err := rawDB(filepath.Join(dir, s.Name))
		if err != nil {
			t.Fatal(err)
		}
		err = pragmaOK(ctx, raw, "quick_check")
		_ = raw.Close()
		if err != nil {
			t.Fatalf("listed snapshot %s is damaged: %v", s.Name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	tmps := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".db.tmp") {
			tmps++
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return tmps
}

// TestCrash_Snapshot kills a child taking snapshots of a 4 MB outline (AC3).
// The live database is untouched, and an interrupted copy is never listed as a
// good snapshot.
func TestCrash_Snapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "outline.db")
	db, err := store.Open(dbPath, store.OpenOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Sync: true, Source: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Repeat("Spores travel far on the wind. ", 13_000) // about 400 KB
	for i := range 10 {
		p := core.PlanInsert(o.Ctx(), o.Tree().Root(), core.Anchor{}, fmt.Sprintf("bullet %d", i), core.InsertFlags{})
		if _, err := o.Do(ctx, p); err != nil {
			t.Fatal(err)
		}
		id := p.ChangeSet.Ops[0].NodeID
		if _, err := o.Do(ctx, core.PlanSetPage(o.Ctx(), id, fmt.Sprintf("# Page %d\n\n%s\n", i, page))); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	want, err := checkFile(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}

	tmps := 0
	env := childEnv(t, envMode+"=snapshot", envDB+"="+dbPath)
	killLoop(t, iterations(t, 40), 5*time.Millisecond, 300*time.Millisecond, env, func(i int) {
		hash, err := checkFile(ctx, dbPath)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if hash != want {
			t.Fatalf("iteration %d: a snapshot changed the live database", i)
		}
		tmps += checkSnapshots(t, filepath.Join(dir, "snapshots"))
	})
	t.Logf("%d kills interrupted a copy", tmps)
	if tmps == 0 {
		t.Error("no kill interrupted a snapshot copy: the test exercised nothing")
	}
}
