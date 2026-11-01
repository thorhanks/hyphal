# Testing

Implements §9.5 and §9.6: every behavior is checkable automatically; no manual verification is an acceptance criterion.

## 1. Commands

| Make target | Runs |
| --- | --- |
| `make test` | `go test -race -count=1 ./...` |
| `make check` | gofmt check, `go vet`, `golangci-lint run`, `make test`, `make fuzz-short`, `make web-lint` |
| `make web-lint` | Go test `TestWebAssets_*`: no external URLs, no non-relative imports, no raw theme values (§6 below), no `innerHTML` on outline data |
| `make e2e` | Playwright on Chromium, Firefox and WebKit. Needs Node from `mise.toml` |
| `make e2e-update` | `make e2e` with snapshot updates (only when a card changes structure; explain each change) |
| `make fuzz-short` | each `Fuzz*` target for 10 s (`-fuzztime=10s`) |
| `make fuzz-long` | each `Fuzz*` target for 10 min (nightly CI) |
| `make bench` | `go test -run=^$ -bench=. -benchmem ./...` with generated DBs |
| `make golden-update` | `go test ./... -update` (only when a card changes output; explain each change) |
| `make crash` | crash-injection suite (`-tags crash`, ~2 min) |

## 2. CI (GitHub Actions)

- `ci.yml` on every push to `main`: matrix `ubuntu-latest`, `macos-latest`; Go from `go.mod`; `make check`; `make e2e`; `make crash` on Linux only.
- `nightly.yml`: `make fuzz-long`, `make bench` compared to the stored baseline (`testdata/bench/baseline.txt`) with `benchstat`; fail if any tracked benchmark regresses > 20 % with p < 0.05 (§9.4). Runs on schedule and on manual dispatch.
- Release workflow (P1-50): GoReleaser on tags.

Playwright's installer hung under Node 26 on Arch during P0-W1; `mise.toml` pins Node 22. Its WebKit build needs Ubuntu libraries, so WebKit runs in CI and only in a container on Arch.

## 3. Test layers

| Layer | Where | Style |
| --- | --- | --- |
| Leaf unit + fuzz | each leaf package | Table tests; `Fuzz*` for markup, dialect, opml, fracindex (§9.5) |
| Store | `internal/store` | Real SQLite in `t.TempDir()`; migration fixtures in `internal/store/testdata/legacy/` |
| Property | `internal/store`, `internal/core` | `rapid`: JSON round trip, undo-all, revert isolation (§9.5) |
| Core rules | `internal/core` (`core_test`) | Fixture notation (CONTRACTS §11): `before`, action, `after`, `hint` |
| CLI | `internal/cli` | Run the Cobra root in-process with args; compare `--json` output to goldens in `internal/cli/testdata/golden/`. Timestamps and IDs normalized (§6 below) |
| Session | `internal/server` | `servertest.Harness`: protocol messages in, state out, no browser (§4) |
| HTTP security | `internal/server` | `httptest`: Host, Origin, ticket, cookie, CSP headers |
| Browser | `e2e/` | Playwright on three engines: real keys, focus, caret, clipboard, IME events, theming (§5) |
| Structure | `e2e/` | ARIA snapshots as text goldens |
| Crash | `internal/store/crashtest` (build tag `crash`) | Subprocess killed with SIGKILL (§7 below) |
| Bench | packages + `internal/testgen` | Generated DBs: 10k and 100k nodes |
| Arch | `internal/archtest` | Import rules (ARCHITECTURE §2), app-name placement scan (CONTRACTS §1) |

Rule logic is tested in `core`, where it is cheap and exhaustive. Browser tests prove the same tables through real keystrokes, because the caret and focus are where a web UI goes wrong.

### Core rule test shape

```go
func TestEnter(t *testing.T) {
    cases := []struct{ name, before, after, hint string }{
        {"start of bullet inserts above",
         "- ‸Alpha\n- Beta",
         "- \n- ‸Alpha\n- Beta", ""},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            f := testgen.MustParseOutline(t, c.before)
            p := core.PlanEnter(f.Ctx(), f.Cursor(), f.Selection())
            got := f.ApplyAndFormat(t, p) // applies to tree, sets cursor from p.Cursor
            testgen.Equal(t, c.after, got); testgen.Equal(t, c.hint, p.Hint)
        })
    }
}
```

Use only the stdlib `testing` package plus small helpers in `testgen` (`testgen.Equal`, `testgen.Golden`); no assertion library dependency.

## 4. `servertest.Harness`

```go
h := servertest.New(t, servertest.Options{
    Outline: "- Alpha‸\n- Beta",   // fixture notation
    Clock: servertest.FixedClock("2026-10-24T09:00:00Z"),
    Config: "",                      // TOML text
})
h.Action("enter")                    // protocol actions; "enter" uses the fixture's cursor
h.Type("hello")                      // sends text messages as the browser would
h.Paste("line one\n  line two")
h.Advance(3 * time.Second)           // fake clock; fires ticks → typing seals
h.AssertOutline("- Alpha\n  - Hi‸\n- Beta")
h.AssertMessage("Moved \"Alpha\" to Trash · Ctrl+Z undoes")
h.AssertBanner("")                   // none
h.Second()                           // a second tab's session, for read-only and takeover tests
h.DB()                               // *store.DB to assert history rows
```

- The harness uses a temp DB, a synchronous committer, a fake clock, a fake editor command (`h.Editor(func(path string) error)`) and a fake browser launcher.
- It calls the session handler directly. It needs no socket except in the tests about reconnects.

## 5. Browser tests (`e2e/`)

- Playwright, TypeScript-free: tests are plain `.mjs` files, matching the no-build rule.
- `e2e/helpers.mjs` starts `hyphal serve --dev --seed <fixture>` per test, with every path under a temp directory, and opens it with the launch ticket. `--dev` exposes `window.__hyphal` for reading state and enables `/keyprobe`; release builds exclude both with a build tag.
- Fixtures use the notation of CONTRACTS §11. Helpers: `place(page, id, offset)`, `outline(page)`, `cursorIs(page, id, offset)`, `footer(page)`, `idle(page)`.
- `internal/webspike/e2e/` is the starting point (P0-18 ports it, then deletes the spike).
- Every card that adds a key adds a browser test tagged in its title with `@command@context` (§9 below).
- Measurements use `page.evaluate` with `requestAnimationFrame` plus a task, and only report.
- Automated keys bypass browser-level shortcuts, so reserved keys are checked by hand (`/keyprobe`). Firefox and WebKit have no CDP composition events; those tests are Chromium only, and real IME use is a human check.

## 6. Web asset checks (Go, `internal/web`)

Run in `make check`, with no Node needed:

- `TestWebAssets_NoExternalURLs`: no `http(s)://` in HTML, CSS or JS outside comments and an allowlist for docs links.
- `TestWebAssets_ImportsRelative`: every `import` is relative and resolves to an embedded file; no bare specifiers.
- `TestWebAssets_TokensOnly`: outside `tokens.css` and `themes/*.css`, CSS has no color literals, no `font-family` values, and no raw `px` sizes except `0` and `1px`.
- `TestWebAssets_TokensUsed`: every token in `tokens.css` is used somewhere, and every `var(--x)` is defined.
- `TestWebAssets_NoInnerHTML`: no `innerHTML` in `internal/web/assets/` outside `vendor/`.
- `TestThemes_Contrast`: bundled `light` and `dark` meet WCAG AA for `--text` and `--muted` on `--bg`.

## 7. Crash injection (§9.5, Phase 0 gate)

- `internal/store/crashtest` builds a helper binary (`go build` in `TestMain`) that opens a DB, then loops: random structural actions via `core` (synchronous commits) and typing bursts through `Outline.TypeInto` with a fake clock that seals per the rules. After each commit it appends the change set ID and the expected durable state hash to a side file (`fsync`ed).
- The parent kills the child with SIGKILL at random times (uniform 5–500 ms) for N=200 iterations (CI) / 2,000 (nightly).
- After each kill: `PRAGMA integrity_check` = `ok`; `hyphal check` invariants pass; every change set recorded in the side file as committed is present; unsealed typing is the only possible loss (the DB state equals the state after the last committed change set).

## 8. Golden files

- Location: `testdata/golden/` next to the test; `e2e/__snapshots__/` for ARIA snapshots. Names: `<TestName>.golden` (`.json`, `.txt`).
- Normalization before comparison: IDs → `<id1>`, `<id2>`… in order of appearance; timestamps → `<ts>`; durations → `<dur>`; temp paths → `<tmp>`; version string → `<version>`.
- Update with `-update` only in cards that change the output; list every changed golden with a one-line reason in the commit message (§9.6).
- Screenshots are for human review only and never fail a test: rendering differs across machines.

## 9. Generators (`internal/testgen`)

- `testgen.Outline(r *rapid.T | *rand.Rand, o OutlineOpts) *store.Dump`: random trees (depth up to 12, fan-out skewed), random markup in text (using the markup grammar), pages (10 %), stars, collapsed state, trashed subtrees, completion, tasks, headings, unicode (CJK, emoji, combining marks, RTL).
- `testgen.Actions(...)`: random sequences of planner calls valid for the current tree (for undo/revert properties and crash tests).
- `testgen.BenchDB(tb, n int) string`: builds and caches (under `$TMPDIR/hyphal-bench/`) a DB with n nodes for benchmarks.
- `testgen.ParseOutline` / `FormatOutline`: fixture notation (CONTRACTS §11).

## 10. Interaction matrix (§9.5)

Every row of the §5.2, §5.3 and §5.3a tables and every binding in §6.4 has a named browser test. P1-55 adds a Go test in `internal/web` that enumerates `keymap.Defaults()` and fails if any binding has no test whose title carries `@<command>@<context>` in `e2e/*.mjs`.

## 11. Performance (§9.4)

| Benchmark | Where | Target (fixed hardware) |
| --- | --- | --- |
| `BenchmarkOpen10k` (open + LoadAll + first state message) | server | part of the < 100 ms launch |
| `launch` (process start → first paint, browser running) | e2e, measured | < 100 ms |
| `typing` (keydown → frame) | e2e, measured | < 16 ms |
| `structure` (Enter keydown → frame) | e2e, measured | < 33 ms p95 |
| `BenchmarkActionReply10k` (action → state message) | server | < 5 ms |
| `BenchmarkCommitAction10k` | store | < 5 ms (Linux) |
| `BenchmarkFilter100k` (one keystroke) | core | < 50 ms |
| `BenchmarkCLIChangeVisible` (CLI commit → state pushed) | server | < 1 s |
| `BenchmarkCLILoad100k` (CLI open + LoadAll) | core | measured in P0-20; target set in report |

Absolute targets are checked by the human on fixed hardware (`make bench` and `make e2e-perf` print tables); CI checks only Go regressions against the baseline (§9.4).
