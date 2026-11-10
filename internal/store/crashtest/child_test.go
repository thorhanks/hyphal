//go:build crash

package crashtest

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
	"github.com/thorhanks/hyphal/internal/testgen"
)

// The child reads its job from the environment. It runs until it is killed;
// it exits by itself only on an error, which fails the test.
const (
	envMode  = "HYPHAL_CRASH_CHILD" // typing, import or snapshot
	envDB    = "HYPHAL_CRASH_DB"
	envSide  = "HYPHAL_CRASH_SIDE"
	envSeed  = "HYPHAL_CRASH_SEED"
	envFiles = "HYPHAL_CRASH_FILES" // import: the two export files, separated by the list separator
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(envMode); mode != "" {
		err := runChild(context.Background(), mode)
		fmt.Fprintln(os.Stderr, "crash child:", err)
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func runChild(ctx context.Context, mode string) error {
	db, err := store.Open(os.Getenv(envDB), store.OpenOptions{})
	if err != nil {
		return err
	}
	seed, _ := strconv.ParseUint(os.Getenv(envSeed), 10, 64)
	r := rand.New(rand.NewPCG(seed, uint64(os.Getpid())))
	switch mode {
	case "typing":
		return childTyping(ctx, db, r)
	case "import":
		return childImport(ctx, db, r)
	case "snapshot":
		return childSnapshot(ctx, db)
	}
	return fmt.Errorf("unknown mode %q", mode)
}

// typingChars never form a leading marker or a line break.
var typingChars = []string{"a", "b", "z", " ", "é", "🍄", "*", "`", "1"}

// childTyping mixes random structural actions with typing bursts under a fake
// clock, so seals happen by the §7.3 rules. Around every step that may commit
// it records what it expects and then what it committed (TESTING §7).
func childTyping(ctx context.Context, db *store.DB, r *rand.Rand) error {
	raw, err := rawDB(db.Path())
	if err != nil {
		return err
	}
	side, err := openSide(os.Getenv(envSide))
	if err != nil {
		return err
	}
	_, last, _, err := readSide(os.Getenv(envSide))
	if err != nil {
		return err
	}
	rowid := last.Rowid
	now := time.Now().UTC()
	o, err := core.OpenOutline(ctx, db, core.OutlineOptions{Sync: true, Source: "ui", Clock: func() time.Time { return now }})
	if err != nil {
		return err
	}
	for {
		p := record{T: "pending", Seal: o.PendingTyping() != nil}
		var step func() error
		switch k := r.IntN(10); {
		case k < 4:
			name, plan := testgen.Action(r, o.Ctx())
			if plan.NoOp {
				continue
			}
			p.Ops = len(plan.ChangeSet.Ops)
			step = func() error {
				_, err := o.Do(ctx, plan)
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				return nil
			}
		case k < 9:
			now = now.Add(time.Duration(50+r.IntN(550)) * time.Millisecond)
			node, ok := typingTarget(o, r)
			if !ok {
				continue
			}
			n, _ := o.Tree().Node(node)
			text := n.Text + typingChars[r.IntN(len(typingChars))]
			step = func() error {
				if err := o.TypeInto(ctx, node, text, now); err != nil && !errors.Is(err, store.ErrInvalid) {
					return err
				}
				return nil
			}
		default:
			now = now.Add(time.Duration(r.IntN(3000)) * time.Millisecond)
			step = func() error {
				_, err := o.SealIfDue(ctx, now)
				return err
			}
		}
		if err := side.write(p); err != nil {
			return err
		}
		if err := step(); err != nil {
			return err
		}
		sets, last, err := changeSetsAfter(ctx, raw, rowid)
		if err != nil {
			return err
		}
		if !trailAllowed(&p, sets) {
			return fmt.Errorf("step %+v committed %v", p, sets)
		}
		rowid = last
		hash, err := stateHash(ctx, db)
		if err != nil {
			return err
		}
		if err := side.write(record{T: "done", Sets: sets, Hash: hash, Rowid: rowid}); err != nil {
			return err
		}
	}
}

// typingTarget keeps typing in the open node most of the time, and otherwise
// moves to another live bullet, which seals the open typing.
func typingTarget(o *core.Outline, r *rand.Rand) (model.NodeID, bool) {
	if t := o.PendingTyping(); t != nil && r.IntN(4) != 0 {
		return t.Node, true
	}
	var live []model.NodeID
	for _, id := range o.Tree().AllNodes() {
		if n, _ := o.Tree().Node(id); id != o.Tree().Root() && n.DeletedAt == nil {
			live = append(live, id)
		}
	}
	if len(live) == 0 {
		return "", false
	}
	return live[r.IntN(len(live))], true
}

func trailAllowed(p *record, sets []changeSet) bool {
	ops := make([]int, len(sets))
	for i, s := range sets {
		ops[i] = s.Ops
	}
	return slices.ContainsFunc(allowedTrail(p), func(a []int) bool { return slices.Equal(a, ops) })
}

// childImport replaces the outline with one export, then the other, forever.
func childImport(ctx context.Context, db *store.DB, r *rand.Rand) error {
	files := filepath.SplitList(os.Getenv(envFiles))
	for i := r.IntN(2); ; i++ {
		f, err := os.Open(files[i%2])
		if err != nil {
			return err
		}
		err = db.ImportJSON(ctx, f, store.ImportReplace)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
}

// childSnapshot takes snapshots forever, deleting each one so the disk
// doesn't fill.
func childSnapshot(ctx context.Context, db *store.DB) error {
	dir := filepath.Join(filepath.Dir(db.Path()), "snapshots")
	for {
		name, err := db.Snapshot(ctx, "daily")
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
}
