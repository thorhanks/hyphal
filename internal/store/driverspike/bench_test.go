package driverspike

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// measureDir returns DRIVERSPIKE_DIR when set, so numbers can come from a real
// disk instead of a tmpfs t.TempDir.
func measureDir(tb testing.TB) string {
	if d := os.Getenv("DRIVERSPIKE_DIR"); d != "" {
		dir, err := os.MkdirTemp(d, "spike-")
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	return tb.TempDir()
}

// TestCommitLatency reports median and p99 of a 10-op transaction on a
// 10k-node DB (card item 4). It only measures; it never fails on numbers.
func TestCommitLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	const n, iters = 10_000, 300
	for _, d := range Drivers {
		t.Run(d.Name, func(t *testing.T) {
			db, _ := openSeeded(t, d, measureDir(t), n)
			ctx := context.Background()
			for i := range 20 { // warm up
				if err := CommitTenOps(ctx, db, i, n); err != nil {
					t.Fatal(err)
				}
			}
			ds := make([]time.Duration, iters)
			for i := range ds {
				start := time.Now()
				if err := CommitTenOps(ctx, db, 100+i, n); err != nil {
					t.Fatal(err)
				}
				ds[i] = time.Since(start)
			}
			slices.Sort(ds)
			fmt.Printf("RESULT commit10ops %s %s median=%v p99=%v\n", d.Name, filepath.Base(os.Getenv("DRIVERSPIKE_DIR")),
				ds[len(ds)/2].Round(time.Microsecond), ds[len(ds)*99/100].Round(time.Microsecond))
		})
	}
}

func BenchmarkCommitTenOps10k(b *testing.B) {
	for _, d := range Drivers {
		b.Run(d.Name, func(b *testing.B) {
			db, _ := openSeeded(b, d, measureDir(b), 10_000)
			ctx := context.Background()
			seq := 0
			for b.Loop() {
				if err := CommitTenOps(ctx, db, seq, 10_000); err != nil {
					b.Fatal(err)
				}
				seq++
			}
		})
	}
}

func BenchmarkLoadAll(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		for _, d := range Drivers {
			b.Run(fmt.Sprintf("%dk/%s", n/1000, d.Name), func(b *testing.B) {
				db, _ := openSeeded(b, d, measureDir(b), n)
				ctx := context.Background()
				for b.Loop() {
					nodes, err := LoadAll(ctx, db)
					if err != nil {
						b.Fatal(err)
					}
					if len(nodes) != n {
						b.Fatalf("loaded %d", len(nodes))
					}
				}
			})
		}
	}
}

// BenchmarkOpenLoadAll includes opening the database, as a CLI call does.
func BenchmarkOpenLoadAll(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		for _, d := range Drivers {
			b.Run(fmt.Sprintf("%dk/%s", n/1000, d.Name), func(b *testing.B) {
				seed, path := openSeeded(b, d, measureDir(b), n)
				_ = seed.Close()
				ctx := context.Background()
				for b.Loop() {
					db, err := d.Open(path, 5000)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := LoadAll(ctx, db); err != nil {
						b.Fatal(err)
					}
					_ = db.Close()
				}
			})
		}
	}
}
