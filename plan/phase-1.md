# Phase 1 · Minimum lovable Workflowy clone

Spec: §11 row 1. Scope: editing rules, block selection, pages with live preview, formatting, theming, command palette, CLI, quick capture, Workflowy import. Every Phase 1 *Must* row in §3's parity checklist.

Exit gate: **a week of daily use on your own outline; side-by-side visual review passes** (human). The automated part is P1-54 (parity suite) and P1-55 (interaction matrix).

**Visual reference:** SPEC §4.3 and §4.6, and `plan/reports/design-mockup.html`, a working mockup with the agreed look (D-045).

## Waves and dependencies

```text
Wave A (leaves + store):   P1-01 config · P1-02 dialect · P1-03 opml · P1-05 platform
                           P1-06 trash store · P1-07 history store · P1-08 integrity · P1-09 snapshots
Wave B (core):             P1-10 visibility · P1-11 paths · P1-12 Enter · P1-13 Backspace/Delete
                           P1-14 indent/move · P1-15 small ops · P1-16 filter · P1-22 inbox+sample
                           P1-23 committer · P1-24 Outline in server mode
Wave C (core interchange): P1-17 paste/cut · P1-19 page edit
                           P1-20 Markdown · P1-21 OPML
Wave D (CLI):              P1-25 skeleton → P1-26 node cmds · P1-27 trash/page/history · P1-28 export/import/…
                           P1-34 capture prompt
Wave E (web base):         P1-29 theming → P1-30 shell and lifecycle → P1-31 rendering → P1-32 text editing → P1-33 structure keys
Wave F (web features):     P1-35 block selection · P1-36 collapse/zoom · P1-37 undo · P1-38 overlays+palette+help
                           P1-39 pickers + o · P1-40 filter UI · P1-41 clipboard/paste · P1-42 CodeMirror bundle
                           P1-43 page screen · P1-45 Trash view
Wave G (robustness):       P1-46 windows/read-only/reload · P1-47 durability · P1-48 settings · P1-49 first run
Wave H (ship + gate):      P1-50 packaging/docs · P1-51 benchmarks · P1-52 reference screens · P1-53 Workflowy import (human)
                           P1-54 parity suite · P1-55 interaction matrix · P1-56 gate
```

Standard messages and hints referenced below are constants in `core/messages.go` (core-generated) or `server/strings.go`. Exact texts given in quotes here are binding; others are the agent's choice (they are not contracts).

All UI cards are tested twice where it matters: rules and state with `servertest.Harness`, real keys and DOM with `e2e/`. Test names in backticks with spaces are Playwright titles.

---

## Wave A — leaves and store

### P1-01 · `config`
Type: impl · Packages: `internal/config` · Depends on: P0-01 · Spec: §7.1, §13, §4.7, §6.4 (keymap config)

Deliverables:
- `Config` struct with every key in §13 and its default; `Paths` (data dir, config file, db, snapshots, exports, runtime dir) per CONTRACTS §9 with `XDG_*` and macOS rules (detect with `runtime.GOOS`).
- `Load(path) (Config, []Problem, error)`: missing file → defaults, no problem. Invalid values → that key falls back to default, `Problem{Line, Key, Message}`. Unknown keys → `Problem{Severity: Warning}`. TOML syntax error → all defaults + one problem with the line.
- `Reload(path, previous) (Config, []Problem)`: if any error-severity problem exists, return `previous` unchanged (§13 "an invalid file is ignored entirely").
- `CommentedTemplate() string`: a full `config.toml` listing every key commented out with its default and a one-line explanation (§13). Written when the settings modal first changes a key and the file is missing (P1-48).
- `SetKey(text, key, value string) (string, error)` and `ResetKey(text, key string) (string, error)`: pure text edits per D-048. They change only the affected line and return an error on invalid TOML or a value that fails the key's validation.
- `[keys.<context>]` parsing: `command_id = "key"` or `["key1","key2"]`; returned raw as `Keys map[string]map[string][]string` (config does not import `keymap`; validation happens in `keymap.WithOverrides`, wired in P1-48).
- `browser` and `theme` per §13.

Acceptance criteria:
- AC1 Empty/missing config gives the §13 defaults → `TestLoad_Defaults` (table of every key).
- AC2 `density = "tight"` at line 3 → default used, problem with line 3 → `TestLoad_InvalidValueLine`.
- AC3 Unknown key → warning, other keys applied → `TestLoad_UnknownKey`.
- AC4 Reload with an invalid file keeps the previous config and returns the problem → `TestReload_InvalidKeepsPrevious`.
- AC5 Linux paths honor `XDG_DATA_HOME`/`XDG_CONFIG_HOME`; macOS data dir is `~/Library/Application Support/hyphal` while config stays `~/.config/hyphal` → `TestPaths_Linux`, `TestPaths_Darwin` (inject GOOS).
- AC6 `CommentedTemplate()` parses as valid TOML and, uncommented, equals the defaults → `TestTemplate_MatchesDefaults`.
- AC7 `SetKey` and `ResetKey` keep comments, blank lines, unknown keys and all other lines byte for byte, for top-level and dotted keys, and uncomment or comment out a template line → `TestSetKey_PreservesFile`.

### P1-02 · `dialect`: shared Markdown list dialect
Type: impl · Packages: `internal/dialect` · Depends on: P0-16, P0-02 · Spec: §7.5 (dialect), §5.4 (leading markers), §5.5

Deliverables: `Parse(r, ParseOptions{SkipHeader bool})`, `Write(w, items, WriteOptions{IDs bool, Header string, ExportColors bool, PageLinks func(*Item) string})`.

Parse rules:
- List markers `-`, `*`, `+`, `1.`/`1)`; indentation spaces or tabs; infer the indent unit from the first indented line (tab = one level; spaces: the smallest positive indent delta). Inconsistent deeper indents attach to the previous line (D-022).
- Lines that are not list items become bullets at the current level (§7.5); blank lines ignored.
- Leading markers converted with `markup.ConvertLeadingMarker` after the list marker (`[ ] `, `[x] `, `## `, combined per D-035).
- `^id` suffix: on import and paste, a valid trailing ` ^<id>` is **stripped and ignored** (§7.5, §5.5); any other `^` is text.
- First line `<!-- … -->` skipped when `SkipHeader`.
- Strip a trailing page link per D-011 and expose it as `Item.PageLink`.
- `_italic_` and `__bold__` converted to `*…*`/`**…**` on import (§5.8 "Importers convert"; decision: paste converts too, since pasted text comes from other apps — log as A-entry if you disagree).
- Text with control chars handled per D-020.

Write rules: two-space indent, `- ` marker, `[ ] `/`[x] ` (completed non-task also `[x]`, §7.5), `## `, text, optional ` ^id`, optional page link.

Acceptance criteria:
- AC1 The §7.5 example parses into the expected items and writes back identically (with IDs) → `TestDialect_SpecExample`.
- AC2 Indentation table: 2-space, 4-space, tabs, mixed numbered markers, non-list lines, jumps of two levels → `TestParse_Indentation`.
- AC3 `^id` handling: valid trailing ID kept only with `IDs`; `a ^b` (invalid) is text; `x ^k3j9x2m4q7tb y` is text → `TestParse_IDSuffix`.
- AC4 Write→Parse round-trip is identity for random items (text from the markup generator, flags, IDs) → `FuzzDialectRoundTrip`, `TestDialect_RoundTripProperty`.
- AC5 Parser never panics → `FuzzParse`.

### P1-03 · `opml`
Type: impl · Packages: `internal/opml` · Depends on: P0-16 · Spec: §7.5 (Workflowy mapping, OPML export)

Deliverables: `Parse(r) ([]*Outline, Stats, error)` with `Outline{Text, Note, Complete, Children}`, and conversion `Text` from Workflowy inline HTML to stored markup per the mapping table: `<b>` → `**`, `<i>` → `*`, `<s>`/`<strike>` → `~~`, `<code>` → `` ` ``, `<a href>` → `[label](url)`, `<span class="colored c-red">` etc. → nearest of 8 colors, background-color classes (`bc-*`) → `==…==`, unknown tags dropped keeping text, entities decoded; escape characters that would become markup (`*`, `~`, `` ` ``, `=`, `<`, `[`, `\`) with `\` when literal. `Stats` counts mirrors when detectable (attributes found in P1-53) and boards. `Write(w, items, head)` for export: plain text (`markup.Plain`), `_note` = page Markdown, `_complete="true"`. The export marker goes in `<head>` (§7.5) as `<title>Exported by hyphal <version></title>` and `<dateCreated>` (RFC 822 date, as OPML 2.0 specifies).

Acceptance criteria:
- AC1 A synthetic Workflowy export (`testdata/opml/workflowy-synthetic.opml`, built by the agent from public descriptions of the format) imports into the expected markup → `TestParse_WorkflowyMapping` (table per mapping row).
- AC2 Leading markers inside OPML text are converted (§5.4) → `TestParse_LeadingMarkers`.
- AC3 Export → parse keeps text (plain), notes and completion → `TestOPMLRoundTrip`.
- AC4 Never panics on malformed XML; returns a parse error → `FuzzParse`.

### P1-04 · Withdrawn
The browser's clipboard APIs replace the platform tools and OSC 52 (§5.5, P0-24, P1-41).

### P1-05 · `platform`
Type: impl · Packages: `internal/platform` · Depends on: P0-01 · Spec: §8.6, §8.1 (sync folder warning), §4.7 (launching)

Deliverables: `Flock(path) (*FileLock, error)` exclusive non-blocking (`unix.Flock LOCK_EX|LOCK_NB`), `IsLocked(path)`; `OpenBrowser(cfg, url)`: with `browser = "auto"`, the first Chromium-family browser found (`chromium`, `google-chrome`, `brave`, `microsoft-edge`) as `--app=<url> --class=hyphal`, otherwise `xdg-open` / `open`; a template with `{url}` when configured; detached, no shell; `InSyncFolder(path) (name string, ok bool)` for known roots (`~/Dropbox`, `~/Library/Mobile Documents` (iCloud Drive), `~/Library/CloudStorage/*`, `~/OneDrive`, `~/Google Drive`, any ancestor containing `.stfolder` or `.stignore` (Syncthing)); `WriteFileAtomic` (moved here from P0-12 if not already).

Acceptance criteria:
- AC1 A second process can't take the lock; it is released when the holder exits (even by SIGKILL) → `TestFlock_Exclusive`, `TestFlock_ReleasedOnKill`.
- AC2 Sync folder detection table → `TestInSyncFolder`.
- AC3 `OpenBrowser` invokes the right command with the URL as a single argument, for each of auto-with-Chromium, auto-without and a template (fake exec) → `TestOpenBrowser`. Whether `--class` sets the Hyprland class under Wayland is checked by hand in P1-52's review and recorded.

### P1-06 · Store: Trash queries and purge
Type: impl · Packages: `internal/store` · Depends on: P0-08 · Spec: §8.3, §8.4 · D-015, D-016, D-027

Deliverables: `Deletions(includeHidden)` (one row per `deleted_by` among currently trashed nodes; top node = the trashed node of that deletion whose parent is not trashed by the same change set; count; hidden per D-027 using the op rows), `Purge(deletions, source)`, `PurgeOlderThan(cutoff, source)`, `EmptyTrash(source)`. Purge deletes `nodes` rows of the deletion plus any descendants still trashed from earlier deletions (§8.3), with their `pages`, `stars`, `view_state` rows, in one transaction, and records an empty change set (D-015). Never touches `ops`.

Acceptance criteria:
- AC1 Two deletions → two rows, newest first, with counts → `TestDeletions_List`.
- AC2 Merge leftovers and single empty leaves are hidden unless `includeHidden` → `TestDeletions_Hidden`.
- AC3 Purging a deletion also purges earlier-trashed descendants; no trashed row references a purged parent → `TestPurge_Cascade`.
- AC4 History ops for purged nodes remain → `TestPurge_KeepsHistory`.
- AC5 `PurgeOlderThan` purges by `deleted_at` → `TestPurgeOlderThan`.

### P1-07 · Store: history queries and purge
Type: impl · Packages: `internal/store` · Depends on: P0-08 · Spec: §8.2, §8.4, §10.2

Deliverables: `ChangeSets(HistoryFilter{Source, Since, Until, NodeID, Limit, WithOps})` newest first; `ChangeSetByID`; `PurgeHistory(before)` deleting change sets with `at < before` and their ops (one transaction).

Acceptance criteria:
- AC1 Filter by source `agent:claude` and by node → `TestChangeSets_Filter`.
- AC2 `PurgeHistory` removes only older change sets and their ops → `TestPurgeHistory`.

### P1-08 · Store: integrity checks
Type: impl · Packages: `internal/store` · Depends on: P0-09 · Spec: §8.1, §10.2 (`check`) · CONTRACTS §8

Deliverables: `QuickCheck`, `IntegrityCheck`, `CheckInvariants(ctx) []Problem` (SQL implementation of CONTRACTS §8 items 1–10, independent of `core` so it works on any file).

Acceptance criteria:
- AC1 Each invariant violation is reported on a crafted DB (write rows with raw SQL) → `TestCheckInvariants` (table).
- AC2 A byte-corrupted DB copy fails `QuickCheck` → `TestQuickCheck_Corrupt`.

### P1-09 · Store: snapshots
Type: impl · Packages: `internal/store` · Depends on: P0-06, P1-05 · Spec: §8.5, §7.4 · CONTRACTS §9 · D-014

Deliverables: `Snapshot(reason)` (backup API to `*.tmp`, `quick_check` on the copy, rename, 0600; on failure delete and return error), `SnapshotIfDue` (due when the newest good snapshot is > 24 h old **and** the outline changed since it — compare max `change_sets.at` with `meta.last_snapshot_at`), retention (`keep` per DB name; daily snapshots count toward keep; pre-* snapshots too — keep the newest N overall, D-014), `ListSnapshots`, `RestoreSnapshot(name)` (refuse with `ErrServerRunning` if another process holds the server lock; take `pre-restore` snapshot; copy the snapshot **into** the live DB with the backup API, never rename; record a change set with source `restore` and description `Restored snapshot <name>` and no ops; set a `meta` flag `restore_banner` = name so the UI can show the informational banner, §4.5).

Acceptance criteria:
- AC1 Snapshot passes quick_check and has mode 0600; failure leaves no file → `TestSnapshot_Verified`.
- AC2 Due logic: not due within 24 h; not due if unchanged; due otherwise → `TestSnapshotIfDue` (fake clock).
- AC3 Only the newest `keep` are kept → `TestSnapshot_Retention`.
- AC4 Restore takes a pre-restore snapshot first, so restoring that one returns the previous state → `TestRestore_Reversible`.
- AC5 Restore refuses while a lock holder process exists → `TestRestore_RefusesWithServerLock`.
- AC6 Restore while `-wal` has uncheckpointed data yields a consistent DB (integrity_check ok) → `TestRestore_WithWAL`.

---

## Wave B — core

### P1-10 · Visibility, view state and cursor memory
Type: impl · Packages: `internal/core` · Depends on: P0-10 · Spec: §4.1, §5.4 (filter stickiness), §6.1, §7.2 (view/ui state) · D-026, D-036

Deliverables:
- `Tree.VisibleRows(ViewOpts)`: rows under the zoom (zoom heading first unless zoom is Home, D-036), skipping collapsed descendants, completed nodes when `ShowCompleted` is false (a completed node hides with its subtree), and nodes outside the filter result (matches + ancestors), but always including `Sticky` nodes (created/edited under the filter, §5.4) and their ancestors.
- Helpers: `PrevVisible(rows, id)`, `NextVisible(rows, id)`, `IsTopLevelOfZoom`.
- View state API on `Outline`: `SetCollapsed(ids, bool)` (persists through `store.SetCollapsed`, no history), `ExpandToLevel(ids, n)` (D-026), `CollapseAllInZoom()`, `SetShowCompleted`, `SetZoom`, `RememberCursor(zoom, node, offset)`, `CursorFor(zoom)`. Persist ui_state keys (CONTRACTS §4).
- `Breadcrumb(zoom, maxWidth) []Crumb` with middle truncation `Home › … › Development › Tooltips` (§4.1).

Acceptance criteria:
- AC1 Collapsed nodes hide descendants; zoom shows only the target's descendants with the heading first → `TestVisibleRows_CollapseZoom` (fixtures).
- AC2 `ShowCompleted=false` hides completed nodes and their subtrees → `TestVisibleRows_HideCompleted`.
- AC3 Sticky nodes stay visible under a filter even when they no longer match → `TestVisibleRows_Sticky`.
- AC4 Expand-to-level table for N = 1, 2, 9 and `0` → `TestExpandToLevel`.
- AC5 Breadcrumb truncation keeps Home, the last two crumbs and `…` within width → `TestBreadcrumb_Truncation`.
- AC6 Collapse/zoom changes create no change sets → `TestViewState_NoHistory`.

### P1-11 · Path resolution
Type: impl · Packages: `internal/core` · Depends on: P0-10 · Spec: §10.2 (paths) · CONTRACTS §7.3 · D-010

Deliverables: `Resolve(tree, ref string, opts) (NodeID, error)` with `ErrNotFound`, `ErrAmbiguous{Candidates}`; `PathOf(tree, id) string` (escaping `/` and `\`).

Acceptance criteria:
- AC1 Exact match beats case-insensitive; two exact matches → ambiguous with both candidates → `TestResolve_Matching`.
- AC2 Markup stripped for matching (`**Projects**` matches `Projects`) → `TestResolve_Plain`.
- AC3 `\/` and `\\` escapes; `/` and empty = Home; `id:` / `path:` prefixes → `TestResolve_Syntax`.
- AC4 `PathOf` → `Resolve` round-trips for random trees with slashes in text → `TestPathRoundTrip`.
- AC5 Trashed nodes never match paths; IDs of trashed nodes resolve only with `opts.AllowTrashed` → `TestResolve_Trashed`.

### P1-12 · Enter rules
Type: impl · Packages: `internal/core` · Depends on: P1-10 · Spec: §5.2, §5.4 (filter), §5.4 (leading markers) · D-029, D-036

Deliverables: `PlanEnter(ctx, pos TextPos, sel *TextRange) Plan`. `TextPos{Node, Offset}`; the zoom heading is the zoom node itself with `IsZoomHeading`. Selection: delete the selected text first (part of the same change set as the resulting action, folded into the `edit_text`), then apply the row for the resulting position. Split: first half keeps ID and everything attached (§5.2 last paragraph); second half = new node inserted directly after the first half (after its subtree, as next sibling). New nodes created under a filter are added to `Sticky`.

Notes: Enter seals pending typing first (the caller does `Outline.Seal()` before planning). The new empty node's `edit_text` typing begins after it.

Acceptance criteria → `TestEnter` table (fixture notation, CONTRACTS §11), one case per row of §5.2 plus:
- AC1 Start of non-empty: `- ‸Alpha` → `- \n- ‸Alpha`.
- AC2 Middle: `- Al‸pha\n  - Child` → `- Al\n  - Child\n- ‸pha`.
- AC3 End, expanded with children: `- Alpha‸\n  - Child` → `- Alpha\n  - ‸\n  - Child`.
- AC4 End, collapsed: `- Alpha‸ {collapsed}\n  - Child\n- Beta` → `- Alpha {collapsed}\n  - Child\n- ‸\n- Beta`.
- AC5 End of leaf: `- Alpha‸\n- Beta` → `- Alpha\n- ‸\n- Beta`.
- AC6 Empty, nested: `- Alpha\n  - ‸\n  - Gamma` → `- Alpha\n  - Gamma\n- ‸` (outdent; later siblings stay).
- AC7 Empty, top level of zoom: `- Alpha\n- ‸` → `- Alpha\n- \n- ‸`.
- AC8 Zoom heading: `- Proj‸ects {zoom}\n  - A` → `- Projects {zoom}\n  - ‸\n  - A` (never split).
- AC9 Selection: `- Al⟦ph⟧a` → `- Al\n- ‸a`; `- Al⟦pha⟧` → `- Al\n- ‸`.
- AC10 Split keeps page, star, completion and ID on the first half: `- Al‸pha {page star id=aaaaaaaaaaaa}` → `- Al {page star id=aaaaaaaaaaaa}\n- ‸pha`.
- AC11 Under a filter, the created bullet is sticky → `TestEnter_UnderFilterSticky`.
- AC12 One change set per Enter, containing the edit and insert ops → `TestEnter_SingleChangeSet`.

### P1-13 · Backspace and Delete rules (merges)
Type: impl · Packages: `internal/core` · Depends on: P1-10 · Spec: §5.3, §5.3a, §8.3 · D-027

Deliverables: `PlanBackspaceAtStart`, `PlanDeleteAtEnd`. Merge change set: `edit_text` on the survivor (concatenate, no separator), `move` ops adopting children (appended in order), page transfer (`edit_page` create on survivor + `edit_page` delete on merged-away) when only the merged-away has a page, star retarget (`set_star` on survivor if it isn't starred, unstar merged-away) — Phase 2 adds link retargeting via a hook `retargetLinks(from, to)` that is a no-op now — then `delete` with `merged_into`. Cursor at the join point. Rows checked top to bottom, first match wins. Hidden bullets under a filter are skipped when finding previous/next visible.

Hints (binding text):
- Empty with children: `"Use Esc then Delete to delete a bullet with children"`.
- Both pages: `"Both bullets have pages; move one page first"`.
- Both have children: `"Both bullets have children; move them first"`.
- Delete into own first child: `"Can't merge a bullet with its own child"`.

Acceptance criteria → `TestBackspace` and `TestDelete` tables, one case per row:
- AC1 Selection deletes only the text: `- Al⟦ph⟧a` → `- Al‸a`.
- AC2 No previous visible bullet: `- ‸Alpha` (zoom Home) → no-op, no hint.
- AC3 Empty leaf: `- Alpha\n- ‸` → `- Alpha‸`; at zoom heading: `- P {zoom}\n  - ‸` → `- P‸ {zoom}`.
- AC4 Empty with children → no-op with hint.
- AC5 Previous is the zoom heading: `- P {zoom}\n  - ‸A` → no-op.
- AC6 Both pages → no-op with hint.
- AC7 Text, no children: `- A\n  - A1\n- ‸B` → `- A\n  - A1‸B`.
- AC8 Text + children, previous without children: `- Alpha\n- ‸Beta\n  - B1` → `- Alpha‸Beta\n  - B1`.
- AC9 Text + children, previous has children: `- A {collapsed}\n  - A1\n- ‸B\n  - B1` → no-op with hint.
- AC10 Page and star move to the survivor; merged-away goes to the Trash with `merged_into` and is hidden from `Deletions(false)` → `TestMerge_Identity`.
- AC11 Delete rows mirror: no next; next is own first child; next empty leaf deleted; both pages; text merge; adopt children; both children → `TestDelete` (7 cases).
- AC12 Under a filter, hidden bullets are skipped → `TestBackspace_SkipsHidden`.

### P1-14 · Indent, outdent, move
Type: impl · Packages: `internal/core` · Depends on: P1-10 · Spec: §5.4

Deliverables: `PlanIndent(nodes)`, `PlanOutdent(nodes)`, `PlanMove(nodes, Up|Down)`. `nodes` is a contiguous run of siblings (block selection) or one node. All use the **real** tree including hidden bullets (§5.4); moved nodes become sticky under a filter.

Algorithms:
- Indent: previous real sibling P of the first node; none → no-op (silent). Nodes become P's last children, in order; if P is collapsed, `Plan.Expand = [P]`.
- Outdent: parent Q; if Q is the zoom target → no-op with hint `"Already at the top level of this zoom"`. Nodes become Q's next siblings (inserted right after Q, in order); later siblings of the nodes stay under Q.
- Move up (first node N, parent Q): if N has a previous sibling S → place the run before S. Else if Q is the zoom target → no-op. Else if Q has a previous sibling A → run becomes A's last children (`Expand=[A]` if collapsed). Else → run moves up a level, placed right before Q under Q's parent.
- Move down mirrors: next sibling after the run → place after it; else Q is zoom → no-op; else Q's next sibling B → run becomes B's first children (expand B); else → placed right after Q.

Acceptance criteria → tables:
- AC1 Indent: first sibling no-op; under collapsed sibling expands it; multi-selection keeps order → `TestIndent`.
- AC2 Outdent: becomes next sibling of old parent; later siblings not adopted; top level of zoom → hint → `TestOutdent`.
- AC3 Move up/down within siblings → `TestMove_Siblings`.
- AC4 Cross-parent: B's first child up → last child of A (A expands); first child of the first sibling up → becomes its parent's previous sibling; A's last child down → first child of B; mirror → `TestMove_CrossParent` (the four §5.4 examples).
- AC5 Zoom edges are no-ops → `TestMove_ZoomEdges`.
- AC6 Hidden siblings participate (move up past a hidden completed sibling swaps with it) → `TestMove_HiddenSiblings`.

### P1-15 · Small node operations
Type: impl · Packages: `internal/core` · Depends on: P1-10 · Spec: §5.4, §5.8 (color), §6.6, §8.3 (cursor after delete)

Deliverables:
- `PlanToggleComplete(nodes)`: any open → complete all (`completed_at = now`), else reopen all; reopening keeps `is_task` (§7.2). Cursor hint: if a newly completed bullet will hide (`ShowCompleted=false`), move to the next visible bullet; message `"Completed \"<text>\" · Ctrl+Z undoes"` (truncate text to 40 cells).
- `PlanToggleTask`, `PlanToggleHeading` (mixed selection: any off → all on).
- `PlanDuplicate(nodes)`: deep copy as next siblings (after the last selected) with new IDs, pages, completion, task, heading; no stars; links unchanged.
- `PlanSetColor(nodes, color|none)` using `markup.SetColor` (§5.8), `PlanWrapStyle(nodes, Bold|Italic)` for whole bullets from the palette (wrap whole text; if already wrapped entirely, unwrap — decision: toggle).
- `PlanMoveTo(nodes, parent)`: append as last children (D-028); refuse moving into own subtree (`ErrInvalid` → hint).
- `PlanDelete` cursor rule (§8.3): next sibling, else previous sibling, else parent; message `"Moved \"<text>\" and N descendants to Trash · Ctrl+Z undoes"` (`"Moved \"<text>\" to Trash · Ctrl+Z undoes"` when N = 0).
- `PlanEditText(node, text)` applies `ConvertLeadingMarker` (§5.4) and D-020 cleanup; used by typing seal and CLI `edit --text`.

Acceptance criteria:
- AC1 Mixed-state complete rule → `TestToggleComplete_Mixed`.
- AC2 Completing with hide-completed moves the cursor to the next visible bullet and returns the message → `TestToggleComplete_HidesAndMovesCursor`.
- AC3 Reopen keeps `is_task` → `TestReopen_KeepsTask`.
- AC4 Duplicate copies subtree with new IDs, copies pages, drops stars → `TestDuplicate`.
- AC5 Color replaces existing color tag; "No color" removes it → `TestSetColor_Bullets`.
- AC6 Delete cursor rule and message counts → `TestDelete_CursorAndMessage`.
- AC7 Typing `[ ] ` at the start converts to a task and strips the marker in the sealed edit → `TestEditText_LeadingMarker`.
- AC8 Move to own descendant is refused with a hint → `TestMoveTo_IntoSelf`.

### P1-16 · Filter search engine (Phase 1)
Type: impl · Packages: `internal/core` · Depends on: P1-10, P0-20 · Spec: §6.2 · D-021

Deliverables: `Filter(tree, query) FilterResult{Matches map[NodeID][]Range (plain-text ranges), PageLines map[NodeID]string, Count int}`; per-node cached folded plain text (invalidated on text change); page text included once `Tree.SetPages` ran (§6.2: loaded in the background after the first frame); trashed nodes never match; incremental refinement when the new query extends the previous (search only previous matches). `PageLines` holds the first matching page line, trimmed to 80 cells. Range mapping to rendered (concealed) text uses `markup.Plain` offsets.

Acceptance criteria:
- AC1 Terms AND, case/diacritic-insensitive, match on plain text (`**bold** text` matches "bold text") → `TestFilter_Matching`.
- AC2 Matches include collapsed descendants and page text (page match keeps the bullet with `PageLines`) → `TestFilter_CollapsedAndPages`.
- AC3 Trashed nodes never match → `TestFilter_Trash`.
- AC4 Highlight ranges map to the concealed render positions → `TestFilter_RangesMapToConcealed`.
- AC5 `BenchmarkFilter100k` within target or documented (P0-20).

### P1-17 · Paste and cut-move
Type: impl · Packages: `internal/core` · Depends on: P1-12, P1-02, P1-15 · Spec: §5.5 · D-029, D-030

Deliverables:
- `PlanPaste(ctx, target PasteTarget, text string) Plan`: single line (after D-020 cleanup) → text insert replacing the selection (an `edit_text`, merged into pending typing by the caller; leading markers convert only if the result starts with one). Multi-line → `dialect.Parse` (IDs ignored) → subtree inserted per §5.5: end of bullet → siblings after it; mid-text → split (§5.2) and insert between halves; empty bullet → replaces it (the empty node is deleted — use the empty node's ID for the first top-level item instead of deleting, to avoid a Trash entry: decision, log A-entry if changed); block selection → siblings after the selection; zoom heading → first children (D-029).
- Copy: `CopyMarkdown(tree, nodes) string` (dialect, no IDs, no pages, colors kept as typed).
- Cut-move: `CutRecord{Text string, ChangeSetID, Tops []NodeID}` remembered by the session. When the pasted text (from the `paste` event) equals `rec.Text` after normalizing line endings and one trailing newline, the clipboard still holds what cut wrote (§5.5). `PlanPasteCut(ctx, target, rec)`: if those nodes are still trashed by that change set → one change set with `restore` ops + `move` ops to the paste position (same IDs, pages, stars, history). Otherwise fall back to `PlanPaste`.

Acceptance criteria:
- AC1 Each §5.5 placement case (end, mid-text, empty, block selection, zoom heading) → `TestPaste_Placement` (fixtures).
- AC2 Indented Markdown with `[ ]`, `[x]`, `##`, `^id` → tasks, completed, headings, new IDs → `TestPaste_Dialect`.
- AC3 Copy output for a subtree with markup and colors → `TestCopyMarkdown` (golden).
- AC4 Cut then paste of the same text restores the same IDs and page at the new position, as one change set; the cut stays undoable separately → `TestCutPaste_IsMove`.
- AC5 Cut, then the clipboard changes (different text) → paste creates new nodes → `TestCutPaste_ClipboardChanged`.

### P1-18 · Withdrawn
The `$EDITOR` round trip was cut (SPEC §12.2 row 58).

### P1-19 · Page editing (core)
Type: impl · Packages: `internal/core` · Depends on: P0-10, P0-11 · Spec: §5.7, §7.3

Deliverables: `PlanSavePage(ctx, node, text) Plan`: empty text creates no page and deletes an existing one (§5.7); one change set (`source=ui`). The title is ordinary bullet typing through `TypeInto`. `PlanDeletePage(node)`. Page typing seals per §7.3: 2 s idle, 5 s open, leaving the page. A page visit groups its seals and title edits into one undo step.

Acceptance criteria:
- AC1 Saving empty on a new page creates nothing; clearing an existing page deletes it (one change set) → `TestPage_Empty`.
- AC2 Edits coalesce into one `edit_page` per seal → `TestPage_Seal`.
- AC3 One undo reverses a whole visit, including a title edit → `TestPage_VisitUndo`.

### P1-20 · Markdown import and export (core)
Type: impl · Packages: `internal/core` · Depends on: P1-02 · Spec: §7.5 · CONTRACTS §10 · D-011

Deliverables: `ExportMarkdown(tree, pages, root, ExportOptions{IDs, Split, OutDir, Version})` writing `outline.md` (or split files) + `pages/` atomically, headers, color export via `markup.ExportColors`, page links (D-011), trashed nodes excluded; to an `io.Writer` when no `OutDir` and not split. `PlanImportMarkdown(ctx, reader, filename, under)` → new top-level bullet named after the file (basename with extension, §7.5) unless `under` is set; pages attached from `pages/` when present (D-011); source `import`.

Acceptance criteria:
- AC1 Export of a fixture tree with pages equals golden files (outline.md + pages/*) → `TestExportMarkdown_Golden`.
- AC2 `--split` layout → `TestExportMarkdown_Split`.
- AC3 Slug rules and collision-free names → `TestPageSlug`.
- AC4 Import lands under a new top-level bullet named after the file, or directly under `--under` → `TestImportMarkdown_Placement`.
- AC5 Export → import keeps text, tasks, completion (non-task completed becomes completed task, documented loss), headings, pages, structure → `TestMarkdownRoundTrip`.
- AC6 Logseq and Obsidian sample exports (`testdata/markdown/logseq-sample.md`, `obsidian-outliner-sample.md`, built by the agent) import without loss of text or structure → `TestImportMarkdown_Samples`.

### P1-21 · OPML import and export (core)
Type: impl · Packages: `internal/core` · Depends on: P1-03 · Spec: §7.5

Deliverables: `PlanImportOPML(ctx, reader, filename, under) (Plan, ImportSummary)` (pages from `_note`, completion from `_complete`, summary counts nodes, pages, mirrors if detectable); `ExportOPML(tree, pages, root, w)`.

Acceptance criteria:
- AC1 Workflowy synthetic fixture imports with correct text/markup, pages and completion → `TestImportOPML_Workflowy`.
- AC2 Export contains plain text, `_note`, `_complete`, head elements → `TestExportOPML_Golden`.

### P1-22 · Inbox, quick add and first-run sample
Type: impl · Packages: `internal/core` · Depends on: P0-10 · Spec: §6.3 (quick capture), §4.5 (first run) · D-024

Deliverables:
- `PlanAdd(ctx, text, under, anchor)`: default target = Inbox. `EnsureInbox`: `meta.inbox_id` missing, trashed or purged → create a new top-level `Inbox` (appended at the end of Home's children), set `meta.inbox_id` (after commit), and return message `"Created a new Inbox"` (§6.3). A trashed old Inbox stays untouched.
- `SampleOutline` embedded from `core/sample.md` (dialect) teaching the core keys. Content (binding):
  ```markdown
  - Welcome to Hyphal
    - Type to edit a bullet. Enter adds a new one, Tab indents it, Shift+Tab outdents
    - Press Esc to select a bullet, then use single keys
      - z zooms in, Z zooms out, - and = collapse and expand
      - x completes, d deletes (it goes to the Trash), [ and ] move it
      - p opens the bullet's page, for long notes
    - [ ] Try completing this task: Esc, then x
    - Ctrl+P lists every command, and F1 shows every key
    - When you're done, delete this whole branch: select "Welcome to Hyphal" with Esc, then press d
  ```
- `PlanFirstRun(tree)`: if `meta.ui_opened` unset and Home has no children other than the Inbox → insert the sample (source `ui`); set `ui_opened` either way.

Acceptance criteria:
- AC1 Add without Inbox creates one and records its ID; second add reuses it → `TestAdd_CreatesInbox`.
- AC2 Renamed/moved Inbox is still the target → `TestAdd_FollowsInboxID`.
- AC3 Trashed Inbox → new Inbox, message, old stays trashed → `TestAdd_TrashedInbox`.
- AC4 First run inserts the sample once; not when the outline has other content (e.g. after import) → `TestFirstRun`.

### P1-23 · Committer, retries, reload, recovery files
Type: impl · Packages: `internal/core` · Depends on: P0-11, P0-13 · Spec: §7.3, §8.1, §9.3 · D-007, D-033

Deliverables: `Committer` (ARCHITECTURE §4.3) with jobs `Commit{cs, expect, seen}` and `Reload`; results `Committed{result}`, `CommitFailed{err, attempt}` (retry every 3 s; `ErrInvalid` is not retried: it triggers a Reload and reports `Rejected`), `Reloaded{dump}`; `Drain(timeout)`. `SyncCommitter` for tests/CLI. `WriteRecoveryFile(dataDir, dbName, tree, pending []model.ChangeSet, typing *Typing) (path, error)`: dialect with `^id`s of the nodes affected by uncommitted change sets and unsealed typing, each with a comment header describing what didn't save (CONTRACTS §9).

Acceptance criteria:
- AC1 Jobs execute in submission order; a reload after commits reflects them → `TestCommitter_FIFO`.
- AC2 A failing store (injected) retries every 3 s, later jobs wait, success resumes → `TestCommitter_Retry` (fake clock).
- AC3 `ErrInvalid` → Rejected + automatic reload → `TestCommitter_Rejected`.
- AC4 `Drain` returns after the queue empties or times out → `TestCommitter_Drain`.
- AC5 Recovery file contains the unsaved text and IDs, mode 0600 → `TestRecoveryFile`.

### P1-24 · `Outline` in server mode: undo with cursor, messages
Type: impl · Packages: `internal/core` · Depends on: P1-23, P0-13 · Spec: §5.6

Deliverables: `Outline` async mode wiring `Do` → `Tree.Apply` + `Committer.Submit` + undo push (optimistic; replaced by recorded ops on `Committed`; removed on `Rejected`); `Undo()`/`Redo()` return a `Plan` whose `Cursor` is the stored cursor-before and a `ZoomTo` when the affected nodes are outside the current zoom (§5.6), message `"Undid: <description>"` / `"Redid: <description>"` and, when fields were skipped, `"Undid: <description> · N fields changed elsewhere were kept"`; Undo seals typing first (§5.6); reload merging (`ApplyReload(dump)` keeps unsealed typing on top, §7.3).

Acceptance criteria:
- AC1 Ctrl+Z right after typing undoes the typing just done (seal first) → `TestUndo_SealsTypingFirst`.
- AC2 Undo restores the stored cursor and selection → `TestUndo_RestoresCursor`.
- AC3 Undo of a change outside the zoom returns `ZoomTo` → `TestUndo_ZoomsToShow`.
- AC4 CLI change sets are never on the stack → `TestUndo_IgnoresCLIChanges`.
- AC5 Reload with unsealed typing keeps the typed text and later commits it after the other change → `TestReload_KeepsTyping`.

---

## Wave D — CLI

### P1-25 · CLI skeleton
Type: impl · Packages: `internal/cli`, `cmd/hyphal` · Depends on: P1-01, P1-11, P0-12 · Spec: §10.2 · CONTRACTS §7

Deliverables: Cobra root `hyphal` (no subcommand → start or reuse the server and open a window; `hyphal serve` runs it in the foreground, via a function variable to avoid an import cycle: `cli.RunServer = server.Run` in `cmd/hyphal/main.go`); persistent flags `--db`, `--json`, `--source` (validate `cli` | `agent:<name>`; `ui`, `import`, `restore` are reserved), `--version`; DB resolution order (§10.2); envelope printer; error→code→exit mapping (CONTRACTS §7.2) in one place; human printers; version string injected by `-ldflags` (default `dev`).

Acceptance criteria:
- AC1 `--db` beats `HYPHAL_DB` beats config → `TestResolveDB`.
- AC2 Each error code maps to its exit status and envelope → `TestErrorMapping` (table).
- AC3 Usage errors exit 2 with `usage` code under `--json` → `TestUsageError`.
- AC4 `--source agent:claude` is recorded; `--source ui` is rejected → `TestSourceFlag`.

### P1-26 · CLI node commands
Type: impl · Packages: `internal/cli` · Depends on: P1-25, P1-15, P1-16, P1-22 · Spec: §10.2

Commands: `add "<text>" [--under N] [--after N | --before N]`, `show N [--depth K]`, `tree [--root N] [--depth K]`, `search "<query>"` (Phase 1 semantics, D-021), `edit N --text T [--if-version V]`, `set N [--task|--no-task] [--heading|--no-heading]`, `move N --under P [--after S | --before S | --index I]`, `complete N`, `reopen N`, `delete N`. Every write: one change set, then `SnapshotIfDue` and purge-due check (ARCHITECTURE §6). Data shapes per CONTRACTS §7.4.

Acceptance criteria (CLI goldens, TESTING §3):
- AC1 Each command's `--json` output matches its golden → `TestCLI_<Command>_JSON` (one per command).
- AC2 `edit --if-version` mismatch → exit 4, `conflict` → `TestCLI_Edit_Conflict`.
- AC3 Ambiguous path → exit 3 with candidates → `TestCLI_Ambiguous`.
- AC4 `add` lands in the Inbox by default; `--under`/`--after` placement → `TestCLI_Add_Placement`.
- AC5 `move --index` places at that index among live siblings; moving under own descendant → `invalid` → `TestCLI_Move`.
- AC6 Editing the root → `invalid` → `TestCLI_RootImmutable`.
- AC7 Text with a leading `[ ] ` becomes a task (§5.4) → `TestCLI_Edit_LeadingMarker`.
- AC8 A write while the DB is held by an exclusive SQLite write lock for > 5 s → exit 5 `busy` → `TestCLI_Busy`.
- AC9 A DB with a newer schema → writes exit 6 `read_only`; reads work → `TestCLI_ReadOnly`.

### P1-27 · CLI trash, page, history, revert
Type: impl · Packages: `internal/cli` · Depends on: P1-25, P1-06, P1-07, P0-13, P1-19 · Spec: §10.2, §8.2, §8.3

Commands: `trash list [--all]`, `trash restore <id>` (top node ID of the deletion; message names where it landed, D-017), `trash empty --yes` (purge is final; without `--yes` in a TTY ask `Empty the Trash? This can't be undone [y/N]`, without TTY → usage error), `page get N`, `page set N [--file F | stdin] [--if-version V]` (raw Markdown, no title line — the CLI sets the body only; decision, log A-entry if changed), `page delete N`, `history list [--source S] [--node N] [--limit K] [--ops]`, `history purge --before DATE --yes`, `revert <change-set-id>`.

Acceptance criteria:
- AC1 Goldens for every command's JSON → `TestCLI_Trash_*`, `TestCLI_Page_*`, `TestCLI_History_*`, `TestCLI_Revert_JSON`.
- AC2 `revert` reports skipped fields → `TestCLI_Revert_Skipped`.
- AC3 `trash list` hides merge leftovers; `--all` shows them → `TestCLI_TrashList_All`.
- AC4 `page set --if-version` conflict → exit 4 → `TestCLI_PageSet_Conflict`.

### P1-28 · CLI export, import, snapshot, check, paths
Type: impl · Packages: `internal/cli` · Depends on: P1-25, P1-20, P1-21, P1-09, P1-08 · Spec: §10.2, §7.5, §8.5

Commands: `export --format markdown|json|opml [--out DIR] [--ids] [--history] [--split]` (no `--out`: single-file formats to stdout; `--split` requires `--out`), `import --format json|opml|markdown FILE [--under N] [--replace]` (JSON without `--replace` into non-empty → `not_empty` naming `--replace`, §7.5; `--under` with JSON imports as new nodes with new IDs: build via planners), `snapshot list`, `snapshot restore NAME` (`server_running` if locked), `check` (`QuickCheck` + `CheckInvariants`; exit 1 `integrity` with problems), `paths`. The `--scheduled` flag is not added until P2-13.

Acceptance criteria:
- AC1 JSON export → import into an empty DB → export is identical (via CLI) → `TestCLI_JSONRoundTrip`.
- AC2 Import JSON into non-empty without `--replace` → `not_empty` message mentions `--replace` → `TestCLI_Import_NotEmpty`.
- AC3 `--replace` takes a snapshot first (listed by `snapshot list`) → `TestCLI_Import_Replace`.
- AC4 `snapshot restore` refuses while a server lock is held → `TestCLI_SnapshotRestore_Locked`.
- AC5 `check` on a corrupted DB exits 1 with problems → `TestCLI_Check`.
- AC6 `paths` JSON golden → `TestCLI_Paths`.
- AC7 Every CLI write takes a due snapshot (fake clock via env `HYPHAL_TEST_NOW`, test-only build tag) → `TestCLI_SnapshotOnWrite`.

### P1-34 · Withdrawn
The terminal capture prompt was cut (SPEC §12.2 row 60). `hyphal add` covers capture from a shell.

---

## Wave E — web base

### P1-29 · Theme system
Type: impl · Packages: `internal/server/theme`, `internal/web` · Depends on: P0-18, P1-01 · Spec: §4.4, §4.6 (tokens), §13 (`theme`)

Deliverables:
- `tokens.css` completed with the values in SPEC §4.6: every color, font, size, spacing, radius, glyph and motion token. Literata and Fraunces as bundled WOFF2 files with their license text (D-045).
- `lib/glyphs.js` and the thread-drawn icon set, redrawn cleanly from the mockup.
- Bundled `themes/light.css` and `themes/dark.css`; `auto` follows `prefers-color-scheme` through `light-dark()`.
- `/theme.css` serves the active theme: `theme = auto` → Omarchy mapping if `colors.toml` exists, else the bundled pair; `light`/`dark`; or a file from `themes/`. `/user.css` serves `user.css`. Both load into their cascade layers.
- `theme.MapOmarchy(path) (css string, ok bool)`: hex colors and `mode` only; missing or unreadable → `ok=false`, no error.
- `theme.Watch`: polls the active files every 500 ms (works through Omarchy's symlink swap) and pushes `{"t":"theme"}`; the page swaps the stylesheet without a reload.
- `docs/theming.md`: every token, the state attributes components expose, a worked example theme, how layers work. The example theme is a test fixture.

Acceptance criteria:
- AC1 A theme file overriding three tokens changes those three computed values and nothing else → `theme overrides tokens` (e2e).
- AC2 Editing the active theme file restyles the page with no reload → `theme live reload` (e2e).
- AC3 `MapOmarchy` maps the roles, rejects non-hex values, and falls back when the file is missing → `TestMapOmarchy` (table, temp paths only).
- AC4 A rule in `user.css` beats a component rule regardless of specificity → `user layer wins` (e2e).
- AC5 `light` and `dark` meet AA → `TestThemes_Contrast`.
- AC6 The docs' example theme loads and every token it names exists → `TestThemingDocs`.
- AC7 Under `prefers-reduced-motion`, motion tokens resolve to none → `reduced motion` (e2e).

### P1-30 · App shell, lifecycle and state machine
Type: impl · Packages: `internal/server`, `internal/web`, `internal/cli` · Depends on: P1-29, P1-24, P1-10, P1-05, P1-25 · Spec: §4.5, §4.6, §4.7, §5.1, §6.4 (global keys), §9.3 · D-041

Also (D-047): document title and icon per §4.5; stable port and cookie per §9.3; `hyphal open [<node>]`; clickable Undo in messages; a message clears on the next key or click. AC: `window title`, `open at node`, `message undo click` (e2e); `TestServer_StablePort`, `TestServer_PortFallback`.

Deliverables:
- `<hyphal-app>` layout (§4.5): header (breadcrumb left, muted; DB name + save indicator right), banner row, scrolling body, footer fixed at the bottom, all in the reading column; ARIA landmarks and a live region footer.
- Save indicator: nothing / `●` while typing unsealed or commits queued / red `Save failed` label on commit failure or lost connection.
- Footer priority (§4.5): message → filter query (`/ tooltip · 12 matches`) → hints. Message persists until the next keypress or click, then the previous footer returns.
- Banners: priority (failed write > lost connection > read-only > invalid config > restore info); informational ones clear on the next outline change.
- State machine: `text`, `block`, `filter`, `page`, `overlay`, `trash`. Esc chain per §4.7. Key routing via `lib/keys.js`: overlay → state context → global.
- Lifecycle: `hyphal` starts or finds the server, gets a ticket, opens a window; exit 10 s after the last tab closes; reconnect with resend; `version` mismatch reloads the page.
- Global commands: Ctrl+S message `"Saved automatically"`, F1 and Ctrl+P (stubs until P1-38), Ctrl+Z/Y (wired in P1-37).
- Footer hint strings (binding): text editing `Enter new · Tab indent · Esc select · Ctrl+P commands · F1 help`; block selection `Enter edit · x complete · d delete · z zoom · ? help · Ctrl+P commands`; page `Ctrl+Enter follow link · Esc back`; trash `Enter preview · r restore · d purge · Esc back`. `Ctrl` renders `⌘` on macOS.

Acceptance criteria:
- AC1 Layout stays correct at 1000×700 and 2400×1200, with the column centered → `shell layout` (e2e, ARIA snapshot + geometry).
- AC2 Message shows until the next key, then hints return → `message until next key` (e2e).
- AC3 Footer priority order → `footer priority` (e2e).
- AC4 Save indicator states → `save indicator` (e2e with a fake failing store).
- AC5 Running `hyphal` twice uses one server and a new ticket; the old ticket is dead → `TestLifecycle_Reuse`.
- AC6 After the last tab closes, the server seals typing, exits after the grace period, and the text is in the DB; a reload inside the grace period keeps it alive → `TestLifecycle_Exit`.
- AC7 Killing the socket mid-typing: banner shows, the page reconnects, and no text is lost → `reconnect resends` (e2e).
- AC8 Banner priority and informational clearing → `TestBanners`.

### P1-31 · Outline rendering
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-30 · Spec: §4.1–§4.3, §4.6, §4.9, §5.7 (page marker), §9.3

Also (D-047): `aria-setsize` and `aria-posinset`; `spellcheck` on the focused bullet; `data-tag` on tag spans. AC: `tree aria sizes` (e2e).

Deliverables:
- `tree`/`treeitem` ARIA. Zoom heading row (not at Home, D-036); rows from `VisibleRows`; spore and Lit strands glyphs as inline SVG, with `data-state` and `data-strands` on the row; hidden count after the text of a collapsed bullet; task checkboxes after the glyph; completed: dim + strikethrough, glyph unchanged; heading-style bullets; density via `data-density`.
- Indent per the `--indent` token, half past 8 levels below the zoom; on entering such a row, once per zoom, message `"Deep outline · Esc z zooms in"`.
- Threads per §4.3: ruled paths in one overlay SVG, recomputed only when row geometry or focus changes, never on a plain keystroke that doesn't change row height. Lit path to the focused bullet; `thread_glow` adds the glow and pulse. Draw-in on expand.
- Page marker uses the page icon. Header icon strip and focused-row icons per §4.6, each with an accessible name and the same action as its key.
- Runs rendered as spans; links as anchors; focused row per D-042.
- Stable virtualization: measured row heights cached by ID so long jumps don't shift the view; scroll margin (§4.6); the caret's row stays fixed when collapsing or expanding; Page Up/Down.
- Filter highlights through the Custom Highlight API and page-match lines beneath the bullet; filter state arrives in P1-40.
- Virtual empty bullet in empty zooms (D-036).

Acceptance criteria:
- AC1 The §4.2 structure renders as specified → `outline structure` (ARIA snapshot).
- AC2 Density rules; heading spacing never doubles → `density` (e2e geometry).
- AC3 Beyond 8 levels the indent step halves → `deep indent` (e2e).
- AC4 A completed parent keeps its collapse glyph; text dim + struck → `completed style` (e2e computed style).
- AC5 Moving down never leaves the caret within the scroll margin unless at the end → `scroll margin` (e2e).
- AC6 Collapse keeps the caret's row at the same screen position → `collapse stable` (e2e).
- AC7 Unchanged rows keep their DOM node across updates → `rows reused` (e2e).
- AC8 An empty zoom shows the virtual bullet; typing creates a real node → `virtual bullet` (e2e).
- AC9 Bullet text with `<script>` or HTML stays literal text → `no HTML from data` (e2e), `TestWebAssets_NoInnerHTML`.
- AC10 Threads connect each visible child to its parent, and only the focus path is lit, after moving, collapsing and scrolling → `threads` (e2e geometry).
- AC11 Strand count follows the hidden count at 4, 5, 9, 10, 24 and 25 → `strand levels` (e2e).
- AC12 With `thread_glow` on and reduced motion set, the glow shows and no animation runs → `thread glow` (e2e).

### P1-32 · Text editing
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-31, P1-15 · Spec: §5.1, §5.8 (wrap keys), §6.1, §6.4 (text-editing table), §7.3

Deliverables:
- Movement from §6.4: arrows (visual lines, across bullets), word moves left to the platform, Home/End (visual line, then bullet on second press via `Selection.modify`), first/last bullet in the zoom, Page Up/Down.
- Selection stays within one bullet; Shift+Up/Down clamps at the bullet's edges.
- Word delete through the native command. Paste, drag-and-drop and rich input are cleaned to plain text (D-020).
- Typing via `text` messages; leading markers convert per §5.4 when the server answers that the source changed: the sealed `edit_text` plus `set_task`/`set_heading` are one change set.
- Wrap keys `*`, `~`, `=`, `` ` `` with a selection → `markup.WrapSelection` on the server, selection kept (§5.8). Without a selection they type.
- Ctrl+C / Ctrl+X with a text selection stay native (D-030).
- Seal typing on leaving the node and on structural commands.

Acceptance criteria:
- AC1 Each movement binding in §6.4's table moves as specified, including Home twice → `text nav @<command>@text` (e2e, one per binding).
- AC2 Up/Down through wrapped lines and across bullets keep the column → `up down wrapped` (e2e).
- AC3 Typing coalesces into one change set per node sealed after 2 s idle → `TestTyping_OneChangeSet` (servertest).
- AC4 Select "word", press `*` twice → `**word**`, selection still on "word" → `wrap keys` (e2e).
- AC5 Typing `## ` at the start makes a heading and removes the marker, with the caret correct → `leading marker typed` (e2e).
- AC6 Pasting styled HTML inserts plain text → `paste plain` (e2e).

### P1-33 · Structural keys in text editing
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-32, P1-12, P1-13, P1-14 · Spec: §5.2–§5.4, §6.4

Deliverables: wire Enter, Backspace at start, Delete at end, Tab/Shift+Tab, Ctrl+Shift+Up/Down, Ctrl+Enter, Ctrl+F (wired in P1-40) to the planners; seal typing before each; apply `Plan.Cursor`, `Plan.Expand`, `Plan.Hint`/`Message`.

Acceptance criteria:
- AC1 Every §5.2 row through real keys → `enter <row> @new_bullet@text` (e2e, three engines).
- AC2 Every §5.3 and §5.3a row → `backspace <row>`, `delete <row>` (e2e).
- AC3 Tab/Shift+Tab and Ctrl+Shift+Up/Down, including hints → `indent and move` (e2e).
- AC4 Ctrl+Enter toggles complete → `ctrl enter` (e2e).
- AC5 Keys typed during a pending reply replay in order → `fast typing across enter` (e2e).

---

## Wave F — web features

### P1-35 · Block selection
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-33 · Spec: §5.1, §6.4 (block table), §4.3 (selected row) · D-036

Deliverables: Esc from text editing selects the current bullet (no caret, `aria-selected` rows); Up/Down move; Shift+Up/Down extend across siblings only; every key in the §6.4 block table dispatched to its command (commands from later cards show `"Not available yet"` until wired — remove when wired); Enter → text editing at end of the (first) selected bullet; Backspace → hint `"Press Delete or d to delete"`; other printable keys → hint `"Press Enter to edit, or ? for help"`; Esc → clear filter if active else hint `"Press Enter to edit"`. Chords that act on bullets apply to the whole selection.

Acceptance criteria:
- AC1 Esc selects; Enter returns with the caret at the end → `block enter exit` (e2e).
- AC2 Shift+Down extends across siblings only and stops at the last sibling → `block extend` (e2e).
- AC3 Printable keys without bindings never change the outline and show the hint → `TestBlock_TypingDoesNothing` (servertest property).
- AC4 `d`/Delete delete the selection with the undo message; Backspace shows the hint → `block delete` (e2e).
- AC5 `x`, `t`, `#`, `D`, `[`, `]`, Tab, Shift+Tab act on the whole selection → `block commands @<command>@block` (e2e).

### P1-36 · Collapse, expand, zoom, breadcrumbs
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-35, P1-10 · Spec: §4.1, §6.1, §6.5, §6.6 · D-026

Also (D-047): zoom through View Transitions when available, within the motion token, off under reduced motion. AC: `zoom transition` (e2e): typing during it isn't lost, and an engine without the API zooms instantly.

Deliverables: `-`/`=` collapse/expand; `1`–`9`, `0` levels (D-026); palette "Collapse all"; `z` zoom in (cursor to its first child or the heading), `Z` zoom out one level (cursor returns to where it was in that zoom, §6.1), `~` zoom to root; `h` show/hide completed (persisted, message `"Completed bullets hidden"` / `"…shown"`); breadcrumb links with middle truncation; cursor memory per zoom (persisted); zoom in the URL via the History API, with Back and Forward; click on glyphs, checkboxes and breadcrumbs (§6.5).

Acceptance criteria:
- AC1 Collapse/expand and levels change visibility and persist across a restart → `TestCollapse_Persist` (servertest).
- AC2 Zoom in/out/root and per-zoom cursor memory → `zoom cursor memory` (e2e).
- AC3 Esc never zooms out → `esc never zooms` (e2e).
- AC4 Browser Back and Forward move through zooms; loading `/#/<id>` opens that zoom; an unknown ID goes to Home with a message → `zoom in URL` (e2e).
- AC5 A deep path truncates in the middle at a narrow width → `breadcrumb truncation` (e2e).
- AC6 `h` hides completed and persists → `show completed` (e2e).
- AC7 Clicking a glyph, a checkbox and a breadcrumb each act → `mouse basics` (e2e).

### P1-37 · Undo and redo
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-24, P1-35 · Spec: §5.6, §6.4 (browser rules)

Acceptance criteria:
- AC1 Ctrl+Z after typing undoes that typing and restores the caret; Ctrl+Y redoes → `undo typing` (e2e).
- AC2 Undo after a subtree delete restores it and the cursor/selection from before → `undo delete` (e2e).
- AC3 Undo of a change outside the zoom zooms there with a message → `undo zooms` (e2e).
- AC4 Undo with nothing to undo → message `"Nothing to undo"` → `undo empty` (e2e).
- AC5 `historyUndo` and `historyRedo` from outside the keymap route to the app's stack; the browser's own undo never changes text → `native undo disabled` (e2e).

### P1-38 · Overlay framework, command palette, help overlay
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-30, P0-19 · Spec: §4.6 (overlays), §4.8 (help), §6.4 (overlay keys), §6.6 (palette)

Deliverables:
- `<hyphal-overlay>` on native `<dialog>`: title, filter input, list with highlight, width from a token; typing filters (fuzzy on the server through `query`, stale results dropped), Up/Down move, Enter chooses, Esc closes, click chooses, focus returns where it was.
- Command palette (Ctrl+P): every command available in the current context with its binding right-aligned, recently used first (`palette_recent`). Running a command executes it in the context it was opened from.
- Palette-only commands in Phase 1: Collapse all, Delete page, Bold (bullets), Italic (bullets), Color…, Empty Trash, Open Trash.
- Help overlay (F1, `?` in block selection): all commands in the current context grouped by context with their keys, generated from the keymap, filterable.

Acceptance criteria:
- AC1 Typing `col` matches Color…, Collapse all, Expand to level… → `palette filter` (e2e).
- AC2 Recent commands come first and persist → `palette recent` (e2e).
- AC3 Ctrl+P inside the overlay moves up; Esc closes and restores focus and caret → `overlay keys` (e2e).
- AC4 Help lists every binding of the current context and filters → `help from keymap` (e2e, compared to the keymap JSON).
- AC5 The overlay meets dialog accessibility: focus trapped, named, Esc closes → `overlay a11y` (e2e).

### P1-39 · Color picker, Move to…, link picker and `o`
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-38, P1-15 · Spec: §4.8 (color picker), §5.4 (Move to), §5.8, §6.4 (`o`, `c`, `m`) · D-025, D-028

Deliverables: color picker overlay (8 swatches with names + "No color"; `1`–`8`, `0`, arrows+Enter, Esc); Move to… overlay (D-028); `o` opens the bullet's URL in a new tab with `noopener`; several links → picker overlay; no link → hint (D-025).

Acceptance criteria:
- AC1 `c` then `3` colors the selected bullets yellow; `0` removes color → `color picker` (e2e).
- AC2 `m`, type a parent name, Enter → moved to the end of that parent with an undo message → `move to` (e2e).
- AC3 `o` opens the URL; with two links a picker appears → `follow link` (e2e).
- AC4 `javascript:` and other disallowed schemes never become links → `TestMarkup_LinkSchemes`, `link schemes` (e2e).

### P1-40 · Filter UI
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-16, P1-35 · Spec: §6.2, §6.4 (filter input), §5.4 (editing under a filter)

Deliverables: Ctrl+F / `/` opens the filter input in the footer row (`/ query · N matches`); outline filters as you type; stale replies dropped; Enter or Down moves focus to the outline on the selected match with the filter active; Esc in input clears and keeps the cursor on the selected match; with the filter active, Ctrl+F or `/` returns to the input; Esc in block selection clears it; sticky nodes per §5.4; page matches show the line beneath and highlight the page icon.

Acceptance criteria:
- AC1 Typing filters; matches highlighted; ancestors shown → `filter typing` (e2e).
- AC2 Enter moves to the outline on the first match; Esc clears and keeps the cursor there → `filter enter esc` (e2e).
- AC3 Editing a bullet so it no longer matches keeps it visible until the query changes → `filter sticky` (e2e).
- AC4 A page match shows the matching line → `filter page match` (e2e).

### P1-41 · Clipboard and paste
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-17, P1-35, P0-24 · Spec: §5.5

Also (D-047): pasting a URL over selected text makes a link, in bullets and on pages; block copy writes `text/plain` and `text/html` (nested `<ul>`, escaped, built by the server). AC: `paste url as link`, `copy html` (e2e); `TestCopyHTML_Escapes`. Falls back to plain text where the engine refuses.

Deliverables: `paste` event routed to `PlanPaste`/`PlanPasteCut`; block-selection Ctrl+C copies Markdown per P0-24's transport (message `"Copied N bullets"`), Ctrl+X cuts (copy then delete; remember `CutRecord`); clipboard failures → message `"Couldn't copy: <error>"`.

Acceptance criteria:
- AC1 Multi-line paste in each position creates the expected subtree → `paste <position>` (e2e).
- AC2 Copy writes Markdown to the clipboard → `copy markdown` (e2e, per P0-24's engine notes).
- AC3 Cut + paste elsewhere moves the nodes (same IDs) → `cut paste moves` (e2e).
- AC4 A rejected clipboard write shows the message → `copy failure` (e2e, stubbed API).

### P1-42 · CodeMirror vendor bundle
Type: impl · Packages: `internal/web`, `web-vendor/` · Depends on: P0-18 · Spec: §9.1 · D-046

Deliverables: `web-vendor/` with exact-pinned CodeMirror packages (including `@codemirror/search`), a small entry file and `make vendor-js`, which uses esbuild to write `internal/web/assets/vendor/codemirror.js` and one on-demand file per code language (JavaScript, Python, Go, CSS, HTML). Output is committed, with license text. `make check` needs no Node. Asset checks (raw colors, `innerHTML`) skip `assets/vendor/`.

Acceptance criteria:
- AC1 `make vendor-js` reproduces the committed files byte for byte → CI step on changes to `web-vendor/`.
- AC2 The core file works under the CONTRACTS CSP → `vendor loads under CSP` (e2e).
- AC3 A code language loads only when a page uses it → `language on demand` (e2e).

### P1-43 · Page screen with live preview
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-42, P1-19, P1-29 · Spec: §5.7, §6.4 (page keys), §9.3 · D-046

Deliverables: `<hyphal-page>` around CodeMirror; `lib/livepreview.js` from `internal/pagespike/src/livepreview.js`, cleaned up, including `==highlight==` and `<color>` extensions matching `markup`; `p` opens a page at `/#/<id>/page`, with the title field above it (§5.7), built from the outline's focused-bullet code; load the vendor file with a dynamic `import()` on first open; find bar from `@codemirror/search`, styled with tokens; `spellcheck` on; breadcrumb ending in the page icon; token adapter and `page.css` (tokens only); keys per §6.4, including arrow handling around collapsed blocks with and without Shift; text sent as `pagetext` about 300 ms after a pause and at once on blur and `pagehide`; `hyphal page set` applied to an open page outside its history, per §7.3; a read-only server gets a read-only editor; palette "Delete page". Delete `internal/pagespike/` and `.github/workflows/pagespike.yml`.

Acceptance criteria:
- AC1 Port the spike's suite: reveal, click accuracy, typing, arrows, block splits, cross-block selection, drag, copy, undo, tables, code, lists, unsafe HTML and links, text integrity, composition, tokens → `page *` (e2e).
- AC2 `p` on a bullet without a page opens it empty with the cursor in the page; leaving it empty creates nothing → `page create` (e2e).
- AC3 Editing the title renames the bullet; one Ctrl+Z in the outline reverses the whole visit → `page rename undo` (e2e).
- AC4 Esc saves and returns to block selection on the page's bullet → `page esc` (e2e).
- AC5 Ctrl+click and Ctrl+Enter follow a safe link; a plain click only places the cursor → `page links` (e2e).
- AC6 Typing survives a `hyphal page set` during editing → `TestPage_Concurrent`, `page concurrent` (e2e).
- AC7 `![alt](url)` shows its alt text and makes no request → `page image` (e2e).
- AC8 Tables and fences nested in lists and quotes render and open correctly → `page nested blocks` (e2e).
- AC10 Closing the tab right after typing loses nothing beyond in-flight keys → `page close saves` (e2e).
- AC11 The vendor file isn't requested until a page opens → `editor loads on demand` (e2e).
- AC12 Ctrl+F finds text not yet drawn; Esc closes the bar, then leaves → `page find` (e2e).
- AC13 Back and Forward move between outline and page → `page address` (e2e).
- AC9 Typing on a 2,000-line page stays within a frame at p95; report 20,000 lines → `measure page` (e2e, reports only).

### P1-44 · Withdrawn
The `$EDITOR` round trip was cut (SPEC §12.2 row 58).

### P1-45 · Trash view
Type: impl · Packages: `internal/web`, `internal/server` · Depends on: P1-06, P1-38 · Spec: §4.8, §8.3, §6.4 (Trash table)

Deliverables: full-page view opened from the palette (Open Trash): rows `text · former breadcrumb · N bullets · 3d ago`, newest first, hidden entries excluded; Enter → read-only preview, Esc back to the list; `r` restores (message names where it landed, D-017; the view stays so several entries can be restored); `d` purges after a confirmation in a `<dialog>`: `Purge "<text>" for good?`; palette Empty Trash with `Empty the Trash for good?`; Esc returns to the outline.

Acceptance criteria:
- AC1 List rows for two deletions with breadcrumbs and counts → `trash list` (e2e).
- AC2 `r` restores to the original position and shows the message → `trash restore` (e2e).
- AC3 `d` then confirm purges; cancel keeps it → `trash purge confirm` (e2e).
- AC4 Preview is read-only → `trash preview` (e2e).

---

## Wave G — robustness

### P1-46 · Windows, read-only mode, live reload
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-30, P1-05, P1-23 · Spec: §4.8 (read-only), §7.3, §8.6 · D-044

Deliverables: the server lock at startup (`store.AcquireServerLock`); any number of windows, each with its own zoom, cursor, selection, filter and row window; `blur` seals that window's typing; state pushed to every window after each change. Newer schema → banner `"This outline was created by a newer version · read-only"`; integrity failure → P1-47. In read-only, editing keys and commands show hint `"Read-only: <reason>"`; navigation, filter, pages, palette and help work; writes are refused server-side. `data_version` poll every 500 ms → Reload job → swap tree, keep each window's zoom, cursor and selection by ID (fallback to nearest surviving ancestor), keep unsealed typing. Stale/rejected messages per D-033.

Acceptance criteria:
- AC1 Two windows zoomed differently both edit; each sees the other's change; each keeps its own cursor → `two windows` (e2e).
- AC2 Focusing another window seals typing in the first → `blur seals` (e2e).
- AC3 One page open in two windows: typing in one appears in the other without entering its history → `page two windows` (e2e).
- AC4 Undo from either window reverses the latest step and restores the cursor in that window → `TestSession_SharedUndo`.
- AC5 A CLI `add` appears in an open window within 1 s → `cli change visible` (e2e).
- AC6 Reload keeps zoom and cursor; when the cursor's node was deleted elsewhere, the cursor goes to its nearest surviving ancestor → `TestReload_KeepsPosition`.
- AC7 Unsealed typing on a node changed by the CLI commits after the CLI change; message shown; both change sets in history → `TestReload_ConcurrentTyping`.
- AC8 A newer-schema database shows the banner and refuses a forged write → `TestSession_ReadOnlyRefused`.

### P1-47 · Durability wiring
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P1-46, P1-09, P1-08, P1-23 · Spec: §7.3, §8.1, §8.5, §8.3 · D-016

Deliverables: async committer in the session; commit failure → red save label + banner `"Couldn't save: <error> · retrying"`, retry every 3 s, typing continues; exit with unsaved edits → recovery file, and the next launch shows `"Unsaved edits were written to <path>"`; SIGHUP/SIGTERM handling (ARCHITECTURE §5); `quick_check` after the first paint → failure switches to read-only immediately, writes a recovery file for unsealed typing, banner `"This outline failed its integrity check · read-only"` with a `Restore latest snapshot` action that closes connections, runs `RestoreSnapshot(latest good)` and reopens; snapshot due check at start, exit and every 60 s; trash purge due (D-016); sync-folder warning banner if `platform.InSyncFolder(db)`: `"outline.db is inside <name>; syncing a live database can corrupt it"`.

Acceptance criteria:
- AC1 Injected commit failure: red label, banner, typing continues, success after recovery clears both → `TestDurability_FailedWrite`.
- AC2 Exit while the store fails → recovery file with the unsaved text, shown on next launch → `TestDurability_RecoveryOnExit`.
- AC3 SIGTERM during typing → typing sealed and committed before exit → `TestDurability_SIGTERM` (subprocess).
- AC4 Corrupt DB → read-only banner; the action restores the latest good snapshot and reopens editable → `TestDurability_IntegrityRestore`.
- AC5 Snapshot taken when due at start and on the 60 s tick (fake clock) → `TestDurability_SnapshotTimer`.
- AC6 The first state is sent before `quick_check` completes (check blocked by a test hook) → `TestStartup_QuickCheckAfterFirstState`.

### P1-48 · Settings modal and config in the UI
Type: impl · Packages: `internal/server`, `internal/web`, `internal/keymap` · Depends on: P1-01, P1-05, P1-29, P1-38 · Spec: §4.7, §4.8 (settings modal), §13, §6.4 (keymap config), CONTRACTS §12 · D-048

Deliverables: apply config at startup (theme, density, thread glow, trash days, snapshot keep, keys); invalid config banner `"config.toml line N: <message> · using defaults for those keys"`; `Reload` when the file changes, polled like themes (invalid → keep previous + banner); keymap overrides validated (`keymap.WithOverrides`): conflicts keep the default for that command + banner, reserved keys warn (§6.4).

Settings modal: `<hyphal-settings>` on native `<dialog>`, with the sections in §4.8, CSS from tokens only. Opened by a thread-drawn settings icon at the end of the header strip (same style as the mockup's set, `lib/glyphs.js`), Ctrl+, / Cmd+, (command `settings`, added to the keymap so `/keyprobe` covers it) and palette "Settings". The server handles the `settings` and `setting` messages: validate → `config.SetKey` or `ResetKey` (from `CommentedTemplate()` if the file is missing) → `platform.WriteFileAtomic` → reload → push to all windows. The theme list is the bundled themes plus `themes/*.css`, refreshed when the modal opens.

Acceptance criteria:
- AC1 `density = "compact"` applies → `config applied` (e2e).
- AC2 Invalid value → banner naming the line; valid keys still applied → `TestConfig_InvalidBanner`.
- AC3 Changing the file to something invalid keeps the previous config and shows the banner → `TestConfig_ReloadInvalid`.
- AC4 Key override `toggle_complete = "y"` in `[keys.block]` works; a duplicate keeps the default and shows the banner → `key overrides` (e2e).
- AC5 The icon, Ctrl+, and palette "Settings" each open the modal; Esc, the close button and Ctrl+, close it, and focus and caret return → `settings open close` (e2e).
- AC6 Changing theme, density or thread glow restyles this window and a second one with no reload and no Save → `settings apply live` (e2e).
- AC7 A change writes `config.toml` and keeps a hand-written comment; a missing file is created from the template; the value survives a restart → `TestSettings_WritesFile`.
- AC8 An invalid `trash_days` shows an error beside the field and leaves the file unchanged → `settings invalid value` (e2e), `TestSettings_RejectsInvalid`.
- AC9 While the file is invalid or unwritable, the controls are disabled, the modal says why, and the file isn't written; fixing the file re-enables them without a reload → `settings locked` (e2e).
- AC10 "Reset to defaults" restores only its section → `TestSettings_ResetSection`.
- AC11 Every §13 key available in Phase 1 has a control, except `[keys.*]`, which is listed read-only → `TestSettings_CoversEveryKey`.
- AC12 Controls are labeled and reachable by Tab, and the dialog is named → `settings a11y` (e2e). A database opened read-only still allows settings → `settings on read-only` (e2e).

### P1-49 · First run and Inbox refresh
Type: impl · Packages: `internal/server` · Depends on: P1-22, P1-46 · Spec: §4.5 (first run), §6.3

Acceptance criteria:
- AC1 First UI open of a CLI-created empty DB shows the sample outline; the second open does not re-add it → `TestFirstRun_UI`.
- AC2 After `hyphal import` of OPML into a fresh DB, the first UI open adds no sample → `TestFirstRun_AfterImport`.
- AC3 `hyphal add` while a tab shows the Inbox → the new bullet appears → `inbox live refresh` (e2e).

---

## Wave H — ship and gate

### P1-50 · Packaging and docs
Type: impl · Packages: `packaging/`, `docs/`, `.goreleaser.yaml`, `.github/workflows/release.yml` · Depends on: P1-28, P1-34, P1-30 · Spec: §4.7, §9.1 (distribution)

Deliverables: `packaging/hyphal.desktop` (`Exec=hyphal`; `StartupWMClass=hyphal`; categories Office;Utility), Hyprland window rule examples for `hyphal` and `hyphal-capture` in `docs/omarchy.md`, `.goreleaser.yaml` (linux amd64/arm64, darwin amd64/arm64, `CGO_ENABLED=0`, version ldflags, Homebrew tap formula, AUR `PKGBUILD` via goreleaser's `aurs`), `docs/` pages: install, keymap (generated from `keymap.Defaults()` by `go generate`), browsers (supported list, app window, reserved keys), theming (P1-29), backups (off-machine backup is the user's job, §8), never sync the live DB (§8.1), scheduled export via systemd timer/launchd/cron (stub for P2-13), CLI reference (generated by Cobra), `README.md` update.

Acceptance criteria:
- AC1 `goreleaser check` and `goreleaser build --snapshot --clean` succeed in CI → workflow job.
- AC2 `docs/keymap.md` is up to date with `keymap.Defaults()` (CI fails on drift) → `TestDocs_KeymapUpToDate`.
- AC3 `desktop-file-validate packaging/hyphal.desktop` passes (CI on Linux) → workflow step.
- AC4 A release build has no `/keyprobe`, no `--seed` and no `window.__hyphal` → `TestRelease_NoDevSurface`.

### P1-51 · Performance benchmarks
Type: impl · Packages: `internal/server`, `internal/store`, `e2e/` · Depends on: P1-47 · Spec: §9.4 · TESTING §11

Acceptance criteria:
- AC1 Benchmarks in TESTING §11 exist; Go ones run in nightly CI against the baseline → workflow + `testdata/bench/baseline.txt` updated.
- AC2 `plan/reports/p1-51-perf.md` lists the numbers on the CI runner and instructions for the human's fixed-hardware run (`make bench`, `make e2e-perf`), including a real windowed browser.

### P1-52 · Reference screens
Type: impl · Packages: `e2e/`, `plan/reports/` · Depends on: P1-48, P1-40 · Spec: §4.6 (reference screens)

Deliverables: five reference states seeded and screenshotted by `make reference-screens`: laptop window (1440×900), full-screen ultrawide (3440×1440), deep zoom (breadcrumb truncation, deep indent), active filter, light theme. `plan/reports/p1-52-review.md` explains the visual review: side by side with Workflowy, under three themes, one light, in a real `--app` window, plus the window class check from P1-05.

Acceptance criteria:
- AC1 The five states have stable ARIA snapshots → `reference <name>` (e2e).

### P1-53 · Real Workflowy import check
Type: human · Depends on: P1-21, P1-28 · Spec: §7.5

Agent part: `hyphal import --format opml FILE --json` prints an `ImportSummary` (nodes, pages, completed, colors mapped, unknown HTML tags seen, mirrors detected). Write `plan/reports/p1-53-workflowy.md` with instructions: export from Workflowy as OPML, place at `testdata/private/workflowy.opml` (gitignored), run the import into a temp DB, review the summary and the outline. Then stop. A follow-up agent turns the human's findings into fixes and a redacted fixture in `testdata/opml/`.

### P1-54 · Parity checklist suite
Type: impl · Packages: `e2e/` · Depends on: all Phase 1 UI cards · Spec: §3 parity checklist (Phase 1 rows)

Deliverables: `parity.spec.mjs` with one end-to-end test per Phase 1 *Must* row, titled `parity <row>` (one tree zoom breadcrumbs, enter tab backspace, block selection multi bulk, collapse expand levels, complete show hide, pages, inline formatting color, duplicate move to, import workflowy opml, undo trash).

Acceptance criteria:
- AC1 All ten parity tests pass on all three engines → `parity *`.

### P1-55 · Interaction matrix meta-test
Type: impl · Packages: `internal/web` (tests) · Depends on: P1-54 · Spec: §9.5 (interaction matrix) · TESTING §10

Acceptance criteria:
- AC1 Every binding in `keymap.Defaults()` for Phase 1 commands has at least one e2e test titled with `@<command>@<context>` → `TestMatrix_EveryBindingCovered`.
- AC2 Every row of §5.2, §5.3, §5.3a has a key-driven test (list of row IDs in the meta-test) → `TestMatrix_EditingTablesCovered`.

### P1-56 · Phase 1 gate
Type: gate · Depends on: P1-01…P1-55 · Spec: §11 row 1, §9.6

Deliverables: `plan/reports/phase-1-gate.md`: automated evidence (parity suite, matrix, CLI goldens, crash suite, benchmarks), the release build tag, the dogfooding template `plan/reports/phase-1-dogfood.md` (daily log: what felt wrong, bugs, missing), the visual review checklist, and the comparison-set evaluation checklist (§3). Then stop for the human's week of daily use and sign-off. Bugs found during dogfooding become new cards `P1-57+` appended to this file.
