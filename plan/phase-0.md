# Phase 0 · Storage and browser foundations

Spec: §11 row 0, §12.4. Exit gate: **export and import are identical; a crash loses at most the unsealed typing; the outline skeleton passes in all three browser engines.**

Phase 0 code is production code, not throwaway: the leaf packages, the store, the core tree and the web skeleton built here are the foundations of Phase 1. "Spike" cards also produce measurement reports in `plan/reports/`.

**Done:** P0-01 to P0-07, plus spike P0-W1 (`plan/reports/p0-w1-web-ui.md`). **Withdrawn:** P0-17 (wrap engine; the browser does layout). P0-18, P0-19 and P0-23 were rewritten for the web UI.

## Dependency graph

```text
Done: P0-01 · P0-02 ids · P0-03 fracindex · P0-04 textfold · P0-05 driver · P0-06 store open/schema · P0-07 model · P0-W1 web spike

                                        P0-08 commit+history ─┬─ P0-12 JSON export/import ─ P0-14 round-trip props
                                                              ├─ P0-09 load+tree ─ P0-10 basic planners ─┬─ P0-11 typing seal
                                                              │                                         ├─ P0-13 undo/revert + props
                                                              │                                         └─ P0-15 crash injection
                                                              └─ P0-20 perf spike (after P0-10)
P0-16 markup ───────────────────────────┐
P0-10, P0-11 ─ P0-18 web skeleton ──────┴─ P0-19 keymap + keyprobe ─ P0-23 (human) browser and IME checks
P0-21 growth spike (after P0-11) ─ P0-22 gate
```

---

### P0-01 · Repository bootstrap · Done
Type: impl · Packages: repo root, `internal/archtest`, `.github/` · Depends on: — · Spec: §9.1, §9.6

Goal: a buildable Go module with CI, lint and the architecture test, so later cards only add code.

Deliverables:
- `go.mod` with module `github.com/thorhanks/hyphal`, Go ≥ 1.26, toolchain line. Add only the deps needed now (`pgregory.net/rapid`, `golang.org/x/tools` for archtest if used).
- `cmd/hyphal/main.go` printing `hyphal dev` for `--version` (replaced in P1-25).
- Empty package directories with `doc.go` for every package in ARCHITECTURE §1 (as it stood then; P0-18 reconciles the layout) (doc comment: purpose + spec §).
- `Makefile` with the targets in TESTING §1.
- `.golangci.yml` (linters in ARCHITECTURE §7).
- `.github/workflows/ci.yml` and `nightly.yml` per TESTING §2 (benchmark comparison step can be a no-op until P0-20 creates a baseline).
- `internal/archtest`: `TestImportRules` (ARCHITECTURE §2) and `TestAppNamePlacement` (scans `internal/store`, `internal/model` and SQL/JSON field strings for `hyphal` case-insensitively; allowlist per CONTRACTS §1).
- `AGENTS.md` and `CLAUDE.md` at the repo root (identical short text): "Read plan/README.md first. SPEC.md is the product spec. Follow the card workflow."
- `.gitignore`: `/dist`, `testdata/private/`, coverage files.

Acceptance criteria:
- AC1 `make check` passes on a clean checkout → CI run green on Linux and macOS.
- AC2 An import from `internal/store` to `internal/core` makes `TestImportRules` fail → test `TestImportRules_DetectsViolation` (uses a synthetic package graph).
- AC3 A string literal `"hyphal_version"` in `internal/store` makes `TestAppNamePlacement` fail → `TestAppNamePlacement_DetectsViolation`.

---

### P0-02 · `ids` · Done
Type: impl · Packages: `internal/ids` · Depends on: P0-01 · Spec: §7.2 · CONTRACTS §2

Deliverables: `NewNode`, `NewChangeSet`, `Valid`, and `NewNodeFrom(r io.Reader)` for deterministic tests.

Acceptance criteria:
- AC1 Generated IDs are 12 chars from the Crockford alphabet → `TestNewNode_Format` (10,000 samples).
- AC2 `Valid` rejects uppercase, `i l o u`, wrong length, and empty → `TestValid`.
- AC3 No collisions in 1,000,000 generated IDs → `TestNewNode_NoCollisions` (skipped with `-short`).

---

### P0-03 · `fracindex` · Done
Type: impl · Packages: `internal/fracindex` · Depends on: P0-01 · Spec: §7.2 · CONTRACTS §3

Deliverables: `Between`, `NBetween`, `Valid`, `FuzzBetween`.

Notes: port the published algorithm carefully (integer part head chars `a`–`z` for positive lengths, `A`–`Z` for negative; base-62 digits). Do not use floats.

Acceptance criteria:
- AC1 `Between("", "") == "a0"`; `Between(a, "")` > a; `Between("", b)` < b → `TestBetween_OpenEnds`.
- AC2 For random valid a < b, `a < Between(a,b) < b`, and the result is valid → `FuzzBetween`, `TestBetween_Property` (rapid).
- AC3 Repeatedly inserting at the front 10,000 times, then at the back 10,000 times, keeps keys ≤ 20 bytes → `TestBetween_LengthGrowth` (report actual max in the test log).
- AC4 `NBetween(a, b, n)` returns n strictly increasing keys in (a, b) → `TestNBetween`.
- AC5 Byte order equals SQLite `ORDER BY position` order (BINARY collation) → `TestOrderMatchesSQLite` (in-memory SQLite, after P0-05; until then compare with `bytes.Compare` and add the SQLite check in P0-06).

---

### P0-04 · `textfold` · Done
Type: impl · Packages: `internal/textfold` · Depends on: P0-01 · Spec: §6.2

Goal: case-insensitive, diacritic-insensitive substring matching with offsets mapped back to the original string.

Notes: NFD-normalize, drop `unicode.Mn`, apply `cases.Fold()`; build the byte map while transforming rune by rune. `ß` folds to `ss` (map both bytes to the `ß` start). Matches must map to grapheme-cluster boundaries of the original (expand ranges with `uniseg` if needed).

Acceptance criteria:
- AC1 `Index(Fold("Crème Brûlée"), "creme brulee")` finds one match covering the whole original → `TestIndex_Diacritics`.
- AC2 `Fold("STRASSE")` matches `straße` and vice versa → `TestIndex_SharpS`.
- AC3 Turkish dotted/dotless I, Greek final sigma, and CJK behave per `cases.Fold` → `TestFold_Unicode` (table).
- AC4 Returned ranges are valid byte ranges at grapheme boundaries of the original for random input → `FuzzIndex`.
- AC5 Folding 100,000 strings of 40 runes takes < 50 ms total on CI hardware (recorded) → `BenchmarkFold100k`.

---

### P0-05 · SQLite driver evaluation · Done
Type: spike · Packages: `internal/store/driverspike` (deleted at the end of the card or kept as a benchmark) · Depends on: P0-01 · Spec: §9.1, §8.5, §9.4, §12.4.7

Goal: choose between `modernc.org/sqlite` and `github.com/ncruces/go-sqlite3`.

Evaluate for each, on Linux (CI) and macOS (CI):
1. Online backup API works from Go: back up a live WAL DB while another connection writes; restore by backing up a snapshot *into* the live DB (§8.5). Code sample for each.
2. `PRAGMA data_version` changes on a second connection after a write from another process.
3. WAL + `synchronous=FULL` + `fullfsync` on macOS; `busy_timeout` honored across processes.
4. Commit latency of a 10-op transaction on a 10k-node DB (median, p99).
5. `LoadAll` of 10k and 100k node rows into Go structs.
6. Cross-compilation: `GOOS=darwin GOARCH=arm64` and `linux/amd64`, `linux/arm64` with `CGO_ENABLED=0`.
7. Binary size impact.

Deliverables: `plan/reports/p0-05-sqlite-driver.md` with a table and the decision; `go.mod` pins the chosen driver. Default to `modernc.org/sqlite` unless it fails 1–3 or is > 2× slower on 4–5.

Acceptance criteria:
- AC1 The report exists and names the chosen driver, with numbers for items 4–5 → reviewed by Thor.
- AC2 `TestBackupWhileWriting` and `TestRestoreIntoLiveDB` pass with the chosen driver (kept in `internal/store` as regression tests).
- AC3 `TestDataVersionAcrossProcesses` passes (spawns a helper process).

---

### P0-06 · Store: open, schema v1, migrations, permissions · Done
Type: impl · Packages: `internal/store` · Depends on: P0-05, P0-02, P0-07 · Spec: §7.1, §7.2, §7.4, §8.1 · CONTRACTS §4, §9

Deliverables:
- `Open(path, OpenOptions)`: create the parent dir 0700 if missing, the DB 0600, and set pragmas (WAL, `synchronous=FULL`, `foreign_keys=ON`, `busy_timeout`, macOS `fullfsync=ON`).
- Schema v1 DDL in `schema/0001_init.sql` (embedded). On create: insert root row (random ID, text `""`, position `a0`) and `meta` rows (CONTRACTS §4).
- Migration framework: `migrations []Migration{From, To int; SQL string; Go func(tx) error}`. Before running any migration on an existing DB: take a `pre-migration` snapshot (call an internal `snapshotTo(path)` using the backup API; full snapshot management arrives in P1-09).
- Newer `schema_version` than known → open read-only (`ReadOnlyReason=NewerSchema`); every write returns `ErrReadOnly`.
- `meta.description` paragraph (neutral, explains tables).

Acceptance criteria:
- AC1 New DB has exactly the tables/indexes of CONTRACTS §4, root row, and meta keys → `TestOpen_CreatesSchema` (compares `sqlite_master` to a golden).
- AC2 Data dir created 0700, DB file 0600 → `TestOpen_Permissions`.
- AC3 `PRAGMA journal_mode` = wal, `foreign_keys` = 1, `synchronous` = 2 (FULL) on every connection in the pool → `TestOpen_Pragmas`.
- AC4 A DB with `schema_version` = 99 opens read-only; `Commit` returns `ErrReadOnly` → `TestOpen_NewerSchemaReadOnly`.
- AC5 A fake migration 1→2 registered in the test runs once, takes a snapshot first, and bumps `schema_version` → `TestMigrate_SnapshotFirst`.
- AC6 `fracindex` ordering equals SQLite `ORDER BY position` on 10,000 random keys → `TestOrderMatchesSQLite` (moved here from P0-03 AC5 if not done).

---

### P0-07 · `model` types and payloads · Done
Type: impl · Packages: `internal/model` · Depends on: P0-01 · Spec: §7.2, §8.2 · CONTRACTS §5

Deliverables: types from ARCHITECTURE §4.1; `model.Time` with RFC 3339 millisecond UTC marshalling and parsing; typed payload structs per op kind with `EncodeBefore/After`, `Decode`; `Inverse(op) Op` (CONTRACTS §5 table); field comparison helper `ChangedFields(op, current) []string` used by revert.

Acceptance criteria:
- AC1 `model.Time` marshals as `2026-10-17T09:30:00.123Z`, always UTC, always 3 fractional digits → `TestTime_Marshal`.
- AC2 Each op kind round-trips through JSON with nulls present (never omitted) → `TestPayload_RoundTrip` (table over kinds).
- AC3 `Inverse(Inverse(op)) == op` for every kind; `insert`↔`delete` and `delete`↔`restore` per CONTRACTS §5 → `TestInverse`.

---

### P0-08 · Store: commit with history
Type: impl · Packages: `internal/store` · Depends on: P0-06 · Spec: §7.3, §8.2 · CONTRACTS §4, §5

Deliverables: `Commit(ctx, cs, expect, seen)` per ARCHITECTURE §4.2: one transaction; insert `change_sets` row; for each op in order: read current row(s), recompute `before`, validate, apply `after`, bump versions and `updated_at` per CONTRACTS §4, insert `ops` row. Return `CommitResult` with recorded change set and `Stale`.

Validation (`ErrInvalid`): insert with an existing ID; parent missing or trashed (for insert, move, restore); a move creating a cycle (recursive CTE ancestor check); **any** op on the root (it can't be renamed, deleted, moved, completed, starred, or given a page, §4.1); text containing `\n`/`\r` or a leading marker; an invalid ID or position.

Acceptance criteria:
- AC1 An insert + edit_text + move change set writes nodes and exactly 3 ops rows with recomputed `before` → `TestCommit_WritesHistory`.
- AC2 `before` reflects the DB, not the caller: commit an edit_text with a wrong `before`; the recorded op has the true prior text → `TestCommit_RecomputesBefore`.
- AC3 `updated_at` changes for edit_text/set_complete and not for move/set_star; `version` bumps per CONTRACTS §4 → `TestCommit_TimestampsAndVersions` (table).
- AC4 `Expect{Version: 3}` on a node at version 4 returns `ErrConflict` and writes nothing → `TestCommit_ExpectConflict`.
- AC5 `seen` with an older version returns the node in `Stale` but commits → `TestCommit_ReportsStale`.
- AC6 Moving a node under its own descendant, inserting under a trashed parent, and editing the root each return `ErrInvalid` and roll back → `TestCommit_Invariants` (table).
- AC7 Any SQL error mid-change-set leaves no partial rows (inject failure via a test hook) → `TestCommit_Atomic`.
- AC8 Two processes committing concurrently both succeed (busy timeout) → `TestCommit_ConcurrentProcesses`.

---

### P0-09 · Store `LoadAll` and core `Tree`
Type: impl · Packages: `internal/store`, `internal/core` · Depends on: P0-08 · Spec: §7.3, §9.3 · CONTRACTS §8

Deliverables: `store.LoadAll`, `store.LoadPages`, `store.Page`; `core.NewTree`, accessors (ARCHITECTURE §4.3), `Tree.Apply(cs)` for every op kind (mirror of store semantics on the in-memory tree, including version bumps and `updated_at` so the tree equals a reload), `Tree.Check()` implementing CONTRACTS §8 items 1–9.

Acceptance criteria:
- AC1 For random change sets, `Tree.Apply` then compare with `NewTree(LoadAll())` after `Commit` → identical (all fields) → `TestTreeApply_MatchesStore` (rapid, uses `testgen.Actions` once P0-10 exists; initially hand-written change sets).
- AC2 `Children` ordering is `(position, id)` and excludes trashed nodes → `TestTree_ChildrenOrder`.
- AC3 `Check` reports each invariant violation kind on crafted dumps → `TestTreeCheck` (table, one case per invariant).
- AC4 `LoadAll` of 10k nodes < 30 ms on CI (logged, not enforced) → `BenchmarkLoadAll10k`.

---

### P0-10 · Basic planners and `Outline` (CLI mode)
Type: impl · Packages: `internal/core`, `internal/testgen` · Depends on: P0-09, P0-03, P0-02 · Spec: §5.4, §7.3, §8.2, §8.3

Goal: the minimal set of planners needed for history, round-trip and crash tests, plus the synchronous `Outline`.

Deliverables:
- Planners: `PlanInsert(parent, after|before anchor, text, flags)`, `PlanEditText(node, text)`, `PlanMoveTo(nodes, parent, anchor)`, `PlanDelete(nodes)` (subtree, D-008), `PlanRestore(deletion)` (D-017), `PlanSetTask/Heading/Complete`, `PlanSetPage(node, md)` (empty → delete page / not create, §5.7), `PlanSetStar`.
- `Outline` with synchronous commit (`OutlineOptions{Source, Clock, NewID, Sync: true}`), `Do(plan)`.
- `testgen`: fixture notation parser/formatter (CONTRACTS §11), `Actions` generator, `Outline` generator, `TempDB` helper.

Acceptance criteria:
- AC1 `PlanDelete` on a node with 3 descendants emits 4 delete ops, children first, all with the same `deleted_by` → `TestPlanDelete_Subtree`.
- AC2 `PlanRestore` restores only nodes whose `deleted_by` matches (a child deleted earlier stays trashed) → `TestPlanRestore_OneDeletion`.
- AC3 Restore placement follows D-017 for: parent live; parent trashed (goes under nearest live ancestor); chain broken by a purged row (goes under Home) → `TestPlanRestore_Placement` (table).
- AC4 `PlanSetPage(node, "")` on a node without a page is a no-op; on a node with a page emits `edit_page` with `after = null` → `TestPlanSetPage_Empty`.
- AC5 Fixture notation round-trips: `FormatOutline(ParseOutline(s)) == s` for all examples in CONTRACTS §11 → `TestFixture_RoundTrip` (in testgen).

---

### P0-11 · Typing coalescing and sealing
Type: impl · Packages: `internal/core` · Depends on: P0-10 · Spec: §7.3, §8.2, §5.6

Deliverables: `Outline.TypeInto`, `SealIfDue`, `Seal`, `PendingTyping` (ARCHITECTURE §4.3). The tree shows typed text immediately; the `edit_text` op is created only on seal, with `before` = text when typing opened. Seal triggers: 2 s since last keystroke, 5 s since opened, `Seal()` called (leaving the node, structural action, quit, Ctrl+Z). Typing into a different node seals the previous one first. A seal where text equals `before` commits nothing.

Acceptance criteria:
- AC1 Typing "abc" over 1 s then waiting 2 s yields one change set with one `edit_text` op `"" → "abc"` → `TestTyping_SealsAfterIdle`.
- AC2 Continuous typing for 6 s seals at 5 s and opens a new operation → `TestTyping_SealsAfterFiveSeconds`.
- AC3 Typing in A then in B seals A first; two change sets in order → `TestTyping_SealsOnNodeChange`.
- AC4 Type then delete back to the original text: no change set → `TestTyping_NoOpSeal`.
- AC5 `PendingTyping()` is non-nil exactly while unsealed (drives `●`, §4.5) → `TestTyping_Pending`.

---

### P0-12 · JSON export and import (store)
Type: impl · Packages: `internal/store` · Depends on: P0-08 · Spec: §7.5 · CONTRACTS §6

Deliverables: `ExportJSON(w, withHistory, exportedBy)`, `ImportJSON(r, mode)`; modes `ImportEmpty` (error `ErrNotEmpty` if the DB holds anything besides Home — Inbox included, §7.5 mentions the first-run sample) and `ImportReplace` (takes a `pre-import` snapshot, then replaces all tables in one transaction). Validate before writing (CONTRACTS §6, §8). Writing to a file is atomic (temp + rename, 0600) — provide `WriteFileAtomic` in `platform`.

Acceptance criteria:
- AC1 Export of a fixed fixture equals a golden JSON (normalized) → `TestExportJSON_Golden`.
- AC2 `history` key absent without `withHistory`, present with it → `TestExportJSON_History`.
- AC3 Import into a non-empty DB returns `ErrNotEmpty`; with Replace, a snapshot exists and the content is replaced → `TestImportJSON_Modes`.
- AC4 Import of a file violating an invariant changes nothing and returns a parse/validation error naming the problem → `TestImportJSON_Invalid`.
- AC5 Export writes atomically: an injected failure mid-write leaves no partial target file → `TestWriteFileAtomic`.

---

### P0-13 · Undo stack, revert, and history properties
Type: impl · Packages: `internal/core` · Depends on: P0-10 · Spec: §5.6, §8.2 · CONTRACTS §5 · D-009, D-013

Deliverables: `UndoStack` (session, in memory) with entries `{ChangeSetID, Ops, CursorBefore any}`; `Outline.Undo/Redo` (UI source only; CLI outlines have no stack); `PlanRevert(changeSetID)` producing the inverse for fields not changed since, plus `Skipped`. Redo cleared on any new TUI change.

Acceptance criteria:
- AC1 Random action sequences (rapid, 1–50 actions) followed by undo-all restore the original tree exactly (excluding history and versions/updated_at) → `TestUndoAll_RestoresOriginal`.
- AC2 Reverting any single change set from a random history leaves every field touched only by other change sets unchanged → `TestRevert_Isolation`.
- AC3 Revert skips fields changed later and reports them → `TestRevert_SkipsChangedFields`.
- AC4 Undo writes a change set with `undo_of`; redo writes one whose `undo_of` is the undo's ID → `TestUndoRedo_Linkage`.
- AC5 A new change after undo clears redo → `TestUndo_NewChangeClearsRedo`.
- AC6 Reverting a change set that touched a purged node reports it as gone and applies the rest → `TestRevert_PurgedNode`.

---

### P0-14 · Round-trip property tests (phase gate part 1)
Type: impl · Packages: `internal/store`, `internal/testgen` · Depends on: P0-12, P0-10 · Spec: §7.5 "CI checks", §9.5

Acceptance criteria:
- AC1 For random outlines with history (rapid, 200 cases in CI): DB → JSON (`--history`) → fresh DB → JSON is byte-identical, and every table row is equal → `TestJSONRoundTrip_Property`.
- AC2 Same without history: all non-history tables equal → `TestJSONRoundTrip_NoHistory`.
- AC3 A hand-made "real-looking" outline fixture (`testdata/outlines/real-1.json`, ~2,000 nodes with pages, stars, trash, unicode) round-trips → `TestJSONRoundTrip_RealFixture`.

---

### P0-15 · Crash injection (phase gate part 2)
Type: impl · Packages: `internal/store/crashtest` · Depends on: P0-11, P0-13 · Spec: §7.3, §9.5 · TESTING §5

Acceptance criteria:
- AC1 200 SIGKILL iterations: integrity_check ok, invariants hold, every recorded committed change set present, no partial change set (ops count matches) → `TestCrash_Typing` (build tag `crash`).
- AC2 Kill during `ImportJSON(Replace)` leaves either the old or the new content, never a mix → `TestCrash_ImportReplace`.
- AC3 Kill during a backup-API snapshot leaves the live DB intact; the partial snapshot file is not listed as a good snapshot (write to `*.tmp`, rename after `quick_check`) → `TestCrash_Snapshot`.

---

### P0-16 · `markup`: inline grammar
Type: impl · Packages: `internal/markup` · Depends on: P0-04 · Spec: §5.8, §5.4 (leading markers), §6.2 (strip)

Grammar (§5.8):
- Markers: `**` bold, `*` italic, `~~` strike, `` ` `` code, `==` highlight, `[label](url)` link (url must start with `http://`, `https://`, `mailto:` or `file://`), `<color>…</color>` for the 8 colors.
- Openers can't be followed by whitespace; closers can't be preceded by whitespace. Unmatched markers are literal. Styles nest but don't overlap (an overlapping close makes the inner opener literal). Code spans are literal inside. `\` escapes a marker char (`\*`, `\~`, `` \` ``, `\=`, `\<`, `\[`, `\\`). Color tags don't nest in other color tags (inner is literal). Unknown tags are literal. `_` and `__` are literal.
- Delimiter resolution: use a CommonMark-style delimiter stack restricted to these markers; `***x***` → bold+italic.

Deliverables: `Parse`, `Doc.Concealed()`, `Doc.Revealed()` (markers present with `Marker` style; offsets 1:1 with source), `Plain` with offset map, `ConvertLeadingMarker` (§5.4: `[ ] `, `[x] ` (also `[X] `), `## `), `WrapSelection` (§5.8 wrap behavior: wrap with the char, keep selection on the inner text so a second press nests), `SetColor` (wraps whole text in one color tag, replacing an outer color tag that spans the whole text), `ExportColors` (`<span style="color: …">` using the CSS color names `red`, `orange`, `yellow`, `green`, `cyan`, `blue`, `purple`, `gray`), `Links`.

Acceptance criteria:
- AC1 Table of ≥ 60 cases (from §5.8 examples and edge cases: `2 * 3 * 4` literal, `**bold *and italic***`, `\*`, `` `**not bold**` ``, `<red>a <blue>b</blue></red>`, `<pink>x</pink>`, `snake_case`) → `TestParse_Table` comparing `Concealed()` runs.
- AC2 `Revealed()` text equals the source byte-for-byte for random input → `FuzzRevealedIdentity`.
- AC3 `Plain("**bold** text")` = `bold text` with a correct offset map; `[label](url)` → `label` → `TestPlain`.
- AC4 Parser never panics and terminates in linear-ish time (input up to 10 KB under 10 ms) → `FuzzParse`.
- AC5 Leading marker conversion table: `[ ] a` → task; `[x] a` / `[X] a` → completed task; `## a` → heading; `[ ] ## a` → task + heading (D-035); `[ ]a` and `##a` unchanged; `### a` unchanged → `TestConvertLeadingMarker`.
- AC6 `WrapSelection` twice with `*` makes bold and keeps the selection on the inner text → `TestWrapSelection`.
- AC7 `SetColor` replaces an existing outer color and leaves inner styles → `TestSetColor`.

---

### P0-18 · Web skeleton: server, protocol, outline component
Type: impl · Packages: `internal/server`, `internal/web`, `e2e/`, `cmd/hyphal` · Depends on: P0-10, P0-11, P0-16 · Spec: §4.2, §4.3, §4.4, §9.1, §9.3 · CONTRACTS §12 · D-039, D-040, D-042

Goal: the real foundation replacing `internal/webspike`: a server over the real `core` and an outline web component that navigates and types, with the spike's lessons built in. Use the spike as a reference and delete it at the end, with `.github/workflows/webspike.yml`.

Deliverables:
- Add `github.com/coder/websocket`. Reconcile the package directories with ARCHITECTURE §1 (remove `internal/tui`, `internal/layout`, `internal/clipboard`; add `server`, `web`).
- `server.Run`: loopback listener on the database's stable port (§9.3), Host and Origin checks, ticket → cookie exchange (D-041), CSP header, embedded assets, `/ws`, session goroutine (ARCHITECTURE §5), `hyphal serve [--dev] [--seed FILE]`. The `dev` build tag gates `--seed`, `window.__hyphal` and `/keyprobe`.
- Protocol per CONTRACTS §12: `hello`, `welcome`, `text`, `action`, `window`, `state` with `textAck`.
- `web/assets`: `index.html`, `tokens.css` with the `dark` values from SPEC §4.6, `base.css` with the `@layer` order, `<hyphal-app>` and `<hyphal-outline>`: row window with reuse by ID, one `contenteditable="plaintext-only"` focused row, Up/Down across wrapped lines and bullets at a stable column, Left/Right across edges, click to place the caret, the key queue, composition handling, runs rendered without `innerHTML`, marker highlights (D-042).
- `servertest.Harness` (TESTING §4), `e2e/` with `package.json` pinned, `helpers.mjs`, ported spike tests. `Makefile` targets `e2e`, `web-lint`; `mise.toml` pins Node 22. CI runs `make e2e`.
- `internal/web` asset checks (TESTING §6).

Acceptance criteria:
- AC1 Loopback only; bad `Host`, `Origin`, ticket or cookie is refused; a ticket works once; the cookie is `HttpOnly` and `SameSite=Strict`; CSP is set → `TestHTTP_Security` (table).
- AC2 Typing shows at once and reaches the tree; a stale `state` never overwrites newer text → `typing keeps order`, `stale state keeps local text` (e2e), `TestSession_TextAck`.
- AC3 Down from the last visual line of a wrapped bullet moves to the first visual line of the next, keeping the column → `navigation at stable column` (e2e, three engines).
- AC4 Emoji and combining marks survive edits and offsets; composition updates only on commit (Chromium) → `text integrity` (e2e).
- AC5 At 100,000 nodes with 10,000 visible, scrolling away and pressing a key returns to the cursor → `scroll away and return` (e2e).
- AC6 Unfocused `**bold**` renders bold without markers; focused shows source with marker highlight ranges → `concealment` (e2e).
- AC7 Asset checks pass, and each fails on a crafted violation → `TestWebAssets_*`.
- AC8 The full suite runs headless in CI on Linux and macOS.

---

### P0-19 · `keymap` tables and validation; key probe
Type: impl · Packages: `internal/keymap`, `internal/web`, `internal/server` · Depends on: P0-18 · Spec: §6.4

Deliverables:
- `CommandID` constants for **every** command in §6.4 and §6.6 (snake_case, e.g. `help`, `command_palette`, `jump_palette`, `undo`, `redo`, `save_hint`, `new_bullet`, `indent`, `outdent`, `block_select`, `move_up`, `move_down`, `toggle_complete`, `filter`, `collapse`, `expand`, `expand_level_1`…`expand_level_9`, `expand_all`, `zoom_in`, `zoom_out`, `zoom_root`, `edit_text`, `toggle_task`, `toggle_heading`, `delete_subtree`, `copy`, `cut`, `open_page`, `follow_link`, `open_in_editor`, `duplicate`, `toggle_show_completed`, `color_picker`, `toggle_star`, `move_to`, …). Each command has metadata: display name (palette), phase, contexts, `PaletteOnly bool`, `BlockSelectionCommand bool`, and `Native bool` for keys the browser performs itself.
- `Defaults()` with the exact tables of §6.4, in `event.key` notation with `Mod` resolved per platform at the server. Key strings: `mod+shift+arrowup`, `escape`, `#`.
- `Validate()` implementing §6.4's rules: no key bound twice in one context; every `BlockSelectionCommand` has a single-key binding in `block`; no default binding uses a reserved key (`mod+w`, `mod+t`, `mod+n`, `ctrl+tab`, `mod+q`).
- `lib/keys.js` dispatches from the keymap JSON sent in `welcome`, calling `preventDefault` only for bound keys.
- `/keyprobe` (`--dev`): lists every binding, shows which reach the page, and downloads a JSON result. Instructions in `docs/keyprobe.md`.

Acceptance criteria:
- AC1 `Defaults().Validate()` returns no problems → `TestDefaults_Valid`.
- AC2 Each validation rule detects a crafted violation → `TestValidate_Rules` (table).
- AC3 Every block-selection key in §6.4's table is present exactly as specified → `TestDefaults_BlockTableMatchesSpec`.
- AC4 On macOS `mod` resolves to Meta, and native keys stay unbound → `keymap platform` (e2e with a forced platform).
- AC5 The probe records keys and produces valid JSON → `keyprobe records` (e2e).

---

### P0-20 · Performance spike: filter, fuzzy, CLI load
Type: spike · Packages: `internal/core`, `internal/testgen` · Depends on: P0-10, P0-04, P0-16 · Spec: §6.2, §9.4, §12.4.7

Deliverables: `testgen.BenchDB`; benchmarks `BenchmarkFilter100k` (folded substring over plain text of 100k nodes; precompute and cache folded plain text per node in the tree, invalidated on text change), `BenchmarkFuzzy100k` (`sahilm/fuzzy` over 100k plain strings; if > 50 ms, evaluate alternatives or pre-filter by substring), `BenchmarkCLILoad100k`, `BenchmarkActionReply10k` (action → state message over the session), `BenchmarkQuickCheck` (10k, 100k nodes + 1 year of simulated history from P0-21 if available). Report `plan/reports/p0-20-perf.md` with numbers and the stored CI baseline `testdata/bench/baseline.txt`.

Acceptance criteria:
- AC1 Filter over 100k nodes < 50 ms per keystroke on the CI runner, or the report explains the gap and the plan (incremental refinement: a longer query filters the previous result set) → report + benchmark.
- AC2 The report gives CLI load time for 10k/100k and recommends whether path lookups need a narrower query (load only ancestors by SQL) → report.
- AC3 Baseline file committed and nightly comparison wired → CI config.

---

### P0-21 · History growth simulation
Type: spike · Packages: `internal/testgen`, `cmd/hyphal-sim` (optional, or a test with `-tags sim`) · Depends on: P0-11, P0-13 · Spec: §8.4, §12.3 (history growth), §12.4.1

Deliverables: a simulator that replays a "heavy day" (≈ 2,000 typing seals, 300 structural actions, 50 deletes, 20 page saves of 2 KB) for 7 and 365 simulated days on a 5,000-node outline. Report `plan/reports/p0-21-growth.md`: DB size per day/year, history share, `quick_check` duration at 1 year, `LoadAll` time at 1 year, recommendation on a retention default (§12.3).

Acceptance criteria:
- AC1 Report exists with measured numbers and a recommendation → Thor review.

---

### P0-22 · Phase 0 gate
Type: gate · Depends on: P0-01…P0-21, P0-24 · Spec: §11 row 0

Deliverables: `plan/reports/phase-0-gate.md` listing: gate criteria with the tests that prove them (P0-14 AC1–3, P0-15 AC1–3), all reports, open risks, and any proposed spec changes from measurements (browser checks pending human). Leave `Signed off:` for the human.

Acceptance criteria:
- AC1 `go test -tags crash ./...`, the round-trip property tests and `make e2e` pass on main → CI link in the report.

---

### P0-23 · Browser and IME checks
Type: human · Depends on: P0-19 · Spec: §6.4, §9.6

These close what spike P0-W1 couldn't automate.

Agent part: write `docs/keyprobe.md` covering two checks, and a command that turns the results into `plan/reports/p0-23-browser-checks.md`. Then stop.

1. **Keys.** `/keyprobe` in Chromium as a tab and as `--app`, Firefox, and on a Mac in Safari. Save each JSON result to `testdata/keyprobe/`.
2. **IME,** if Thor uses one: compose text in a bullet, including across Enter.

Afterwards a follow-up agent runs the report and asks Thor about any §6.4 change.

---

### P0-24 · Clipboard across engines
Type: spike · Packages: `e2e/`, `internal/web` · Depends on: P0-18 · Spec: §5.5

Goal: settle §5.5's transport, which the spike verified only on Chromium.

Deliverables: a small test page exercising `navigator.clipboard.write` with a promise-valued `ClipboardItem` resolved by a server reply, and `writeText` right after the reply, in Chromium, Firefox and WebKit; `paste` event text access. Report `plan/reports/p0-24-clipboard.md` with the chosen approach per engine and any fallback.

Acceptance criteria:
- AC1 The report names a working transport for each engine, with the test that proves it, or says plainly which engine fails and proposes a fix → report + `clipboard transport` (e2e).
