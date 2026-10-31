# P0-05 · SQLite driver evaluation

Card: P0-05 · Spec: §9.1, §8.5, §9.4, §12.4.7 · Date: 2026-10-26

**Decision: `modernc.org/sqlite` v1.61.0** (SQLite 3.54.1), pinned in `go.mod`.

Both candidates pass every behavior check (items 1–3) and cross-compile cleanly without cgo. The card's rule picks modernc unless it fails 1–3 or is more than 2× slower on 4–5. It is not: commits are equal, and `LoadAll` is about 1.5× slower than ncruces. modernc also adds 1.1–1.5 MB less to the binary.

Candidates: `modernc.org/sqlite` v1.61.0 (C transpiled to Go, SQLite 3.54.1) and `github.com/ncruces/go-sqlite3` v0.35.6 (Wasm build translated to Go by wasm2go, SQLite 3.53.4).

## Results

| # | Check | modernc | ncruces |
| --- | --- | --- | --- |
| 1 | Online backup of a live WAL DB while another connection writes | pass | pass |
| 1 | Restore by backing up a snapshot *into* the live DB; an open connection sees it | pass | pass |
| 2 | `PRAGMA data_version` changes after a write from another process | pass | pass |
| 3 | WAL + `synchronous=FULL` (+ `fullfsync=ON` on macOS) on every pooled connection | pass (Linux, macOS) | pass (Linux, macOS) |
| 3 | `busy_timeout` waits for another process's write lock; gives `SQLITE_BUSY` past it | pass (busy after 202 ms with a 200 ms timeout) | pass (202 ms) |
| 4 | 10-op commit, 10k nodes, btrfs, median / p99 | 1.63 ms / 6.8 ms | 1.64 ms / 5.2 ms |
| 4 | Same on tmpfs (no real fsync) | 0.38 ms / 0.83 ms | 0.36 ms / 0.85 ms |
| 4 | `BenchmarkCommitTenOps10k` (btrfs) | 1.83 ms, 526 allocs | 1.82 ms, 434 allocs |
| 5 | `LoadAll` 10k rows → structs | 17.3 ms, 9.4 MB | 11.5 ms, 8.5 MB |
| 5 | `LoadAll` 100k rows | 166 ms, 106 MB | 112 ms, 96 MB |
| 5 | Open + `LoadAll` 10k / 100k (a CLI call) | 18.3 ms / 169 ms | 13.2 ms / 118 ms |
| 6 | `CGO_ENABLED=0` builds: linux/amd64, linux/arm64, darwin/arm64, darwin/amd64 | all build | all build |
| 7 | Stripped binary size added (linux/amd64; baseline 1.76 MB) | +4.96 MB | +6.35 MB |
| 7 | Same, darwin/arm64 (baseline 1.79 MB) | +4.76 MB | +6.00 MB |

Linux numbers: Intel Core 5 320 (6 threads), Linux 7.2, btrfs on NVMe, Go 1.27.3, medians of 3 runs (300 commits each for latency). Benchmarks run 3× with `-benchmem`.

macOS: the `driverspike` workflow (`.github/workflows/driverspike.yml`) runs the same suite and `measure.sh` on `macos-latest` and `ubuntu-latest`. On macOS both drivers pass every check, including `fullfsync=1` on every pooled connection and the cross-process `data_version` and `busy_timeout` tests ([run 40428909653](https://github.com/thorhanks/hyphal/actions/runs/40428909653)). These timings come from a shared 3-core VM, so they are indicative only (§9.4):

| macOS `macos-latest` (fullfsync on) | modernc | ncruces |
| --- | --- | --- |
| 10-op commit, median / p99 | 1.98 ms / 19.2 ms | 1.38 ms / 5.9 ms |
| `BenchmarkCommitTenOps10k` (3 runs) | 2.5–2.8 ms | 1.6–3.7 ms |
| `LoadAll` 10k | 24–27 ms | 14–17 ms |
| `LoadAll` 100k | 231–306 ms | 151–185 ms |

modernc stays under 2× of ncruces on macOS too, so the choice stands. The commit p99 on a CI VM says little about real `F_FULLFSYNC` cost on a Mac's SSD; the human benchmark on fixed hardware (§9.4) is where to check it.

The regression tests in `internal/store` also run on macOS through `make check`.

## Observations

- **Commit latency** is dominated by fsync, not the driver. Both meet the < 5 ms Linux target (§9.4) at the median. The p99 tail (5–7 ms on btrfs) is the filesystem; P0-08's `BenchmarkCommitAction10k` tracks it from here on.
- **LoadAll** at 10k rows (17 ms) leaves room in the 100 ms cold-launch budget (§9.4). At 100k (~170 ms) the CLI load is the number to watch; P0-20 sets its target. If it becomes a problem, the allocation count (13 per row with modernc vs 7 with ncruces) is the lever: scanning into `sql.RawBytes` or fewer nullable columns, before reconsidering the driver.
- **Backup API.** modernc exposes it through `sql.Conn.Raw` → `NewBackup(dstURI)` / `NewRestore(srcURI)`; ncruces through `driver.Conn.Raw()` → `Conn.Backup` / `Conn.Restore`. Both are a few lines (`internal/store/driverspike/drivers.go`).
- **Backup in one step.** `internal/store` calls `Step(-1)`: one read transaction copies the whole database, and in WAL mode it never blocks writers. Incremental steps restart whenever another connection writes, so a CLI writing in a loop could starve them.
- **Restore source is opened `mode=ro`.** modernc opens the source of `NewRestore` read-write-create by default, so a mistyped snapshot path would create an empty file and restore it over the live outline. `restoreFrom` passes a read-only URI, so a missing file is an error (tested).
- **URI escaping.** Paths are passed as escaped `file:` URIs so `?`, `#`, `%` and spaces in a path keep meaning the file (`TestFileURI`; the backup test writes to `snap #1?.db`).
- Both drivers accept the same DSN parameters (`_pragma=…`, `_txlock=immediate`), so pragmas apply to every new pooled connection.

## Kept code

- `internal/store/sqlite.go`: `driverName`, `dsn`, `fileURI`, `backupTo`, `restoreFrom`. P0-06 builds `Open` and `snapshotTo` on them.
- `internal/store/sqlite_test.go`: `TestBackupWhileWriting`, `TestRestoreIntoLiveDB`, `TestDataVersionAcrossProcesses` (spawns the test binary as a helper process), `TestFileURI`.
- `internal/store/driverspike/`: the comparison, as a separate Go module (A-001) so ncruces never enters the main `go.mod`. Reproduce with `DRIVERSPIKE_DIR=<dir on a real disk> ./internal/store/driverspike/measure.sh`.
