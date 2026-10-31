# Phase 2 · Full Workflowy parity

Spec: §11 row 2. Scope: tags, node links, search operators, stars; jump palette, history view, page mode sync and split view; scheduled export, capture window, Edit here takeover; Sort children and Copy link to node (§6.6).

Exit gate: **every *Must* row of the parity checklist works** (P2-23, automated).

Detail level: task cards with acceptance criteria. Cards that depend on an open question (§12.3 "Phase 2") are blocked until their decision card is resolved; after resolution the deciding agent **must** update those cards' ACs in this file (additive edit, same commit as the DECISIONS entry).

## Dependencies

```text
P2-01 tags decision ──▶ P2-04 markup tags ─▶ P2-05 core tags ─▶ P2-06 UI tags ─▶ P2-21 CLI tags
P2-02 links decision ─▶ P2-07 core links ─▶ P2-08 UI links
P2-03 screens decision ─▶ P2-11 stars · P2-12 jump palette · P2-16 history view
P2-09 query parser (after P2-04) ─▶ P2-10 operators in filter + CLI
P2-19 mode switching + split view
independent: P2-13 scheduled export · P2-14 capture window · P2-15 takeover · P2-20 sort children
P2-24 drag-to-move
P2-22 parity suite ─▶ P2-23 gate
```

---

## Decision cards

### P2-01 · Decide: tag grammar and behavior
Type: decision · Spec: §5.9, §12.3 (Phase 2 · Tags)

Write `plan/proposals/p2-01-tags.md` answering, each with options, examples, and a recommendation:
1. How `#` is distinguished from Markdown headings in pages (`# Title` vs `#tag`), issue numbers (`#123`), `C#`, URL fragments (`https://x/#a`), inline code and code blocks. Suggested rule to evaluate: a tag starts at `#` preceded by start-of-text or whitespace/punctuation other than `&`/`/`/alphanumerics, followed by a letter (any Unicode letter), then letters/digits/`-`/`_`/`/`; never inside code spans, code blocks, link URLs, or at the start of a page line followed by a space.
2. Unicode letters allowed? Numeric-only tags (`#2026`)? Trailing `/` (`#a/`)?
3. Display case vs stored/matched case (match case-insensitively, display as typed? first-seen casing in the tag list?).
4. A rename command (palette + `hyphal tag rename`), implemented as edit_text ops across bullets and pages in one change set?
5. Tag colors with only 8 palette colors: hash → which colors (exclude gray? exclude the accent blue?), collision handling, config `[tag_colors]` override format.

Stop for the human. Afterwards: write `D-1xx` entries and update P2-04…P2-06, P2-21.

### P2-02 · Decide: node link storage and behavior
Type: decision · Spec: §10.1, §12.3 (Phase 2 · Node links)

Proposal `plan/proposals/p2-02-links.md`:
1. Stored form of `[[…]]` that stays readable in a SQLite browser and survives renames (option A: `[[k3j9x2m4q7tb]]` with display text resolved at render; option B: `[[Display text|k3j9x2m4q7tb]]` with display refreshed on render and rewritten lazily; option C: `[[Display text^k3j9x2m4q7tb]]`). Consider: the markup grammar, Plain()/search (§6.2 "links shown as their display text"), CLI output, editor round trip and copy.
2. Obsidian-style Markdown export when links target nodes, not files (anchor to the page file if the target has a page? heading anchor? plain text with `^id` block reference?).
3. Links to trashed and purged nodes: rendering (struck/muted?), `o` behavior, export.
4. Format of Copy link to node (`[[…]]` text for pasting inside the app; `hyphal://`-free).
5. Retargeting on merge (§5.3 says inbound links are retargeted): rewrite linking bullets' text in the merge change set.

Stop for the human. Afterwards update P2-07, P2-08, and P1-13's `retargetLinks` hook description.

### P2-03 · Decide: Phase 2 screens
Type: decision · Spec: §6.6 (stars), §8.2 (history view), §5.9 (tag list), §6.2 (jump palette), §12.3 (Phase 2 · Screens)

Proposal `plan/proposals/p2-03-screens.md` with HTML mockups using the live tokens, and keys for: History view (two modes: a node's versions, change sets by time/source; restore a version; revert a change set), Stars view, tag list (palette command result rows), jump palette result rows (text + breadcrumb layout, starred first, saved searches in P3), and click on tag → filter. Keys must satisfy §6.4's rules (no reserved keys, single keys in block selection, palette-discoverable).

Stop for the human. Afterwards update P2-11, P2-12, P2-16 and `keymap` defaults.

---

## Implementation cards

### P2-04 · Markup: tags and node-link syntax
Type: impl · Packages: `internal/markup` · Depends on: P2-01, P2-02 · Spec: §5.9, §10.1

Deliverables: tag and node-link recognition in `Parse` (new run styles `Tag`, `NodeLink`), `Tags(src) []Tag` (normalized key + display + ranges), `NodeLinks(src) []LinkRef`, `Plain` showing link display text (resolver callback `func(NodeID) (display string, ok bool)`), page-text tag extraction helper for Markdown documents (skips code blocks and headings per P2-01).
Acceptance criteria: tables from the P2-01/P2-02 proposals (every example becomes a test case) → `TestTags_Table`, `TestNodeLinks_Table`; fuzz targets extended → `FuzzParse`.

### P2-05 · Core: tag index and colors
Type: impl · Packages: `internal/core`, `internal/config` · Depends on: P2-04 · Spec: §5.9, §7.2 (tags not stored)

Deliverables: tags parsed into the tree at load and on every edit (bullet text + page text, page tags count for the bullet); `TagCounts()`; nested tags (`#project/lighthouse` matches `#project`); stable color per tag (P2-01 rule) with `[tag_colors]` overrides; rename planner if P2-01 approved it.
Acceptance criteria:
- AC1 Tags from text and pages are indexed and update on edit → `TestTagIndex_Updates`.
- AC2 Colors are stable across runs and honor overrides → `TestTagColors`.
- AC3 Nested tag lookup → `TestTagIndex_Nested`.

### P2-06 · UI: tag chips, completion, follow, tag list
Type: impl · Packages: `internal/server`, `internal/web` · Depends on: P2-05, P2-03 · Spec: §5.9, §4.6 (chips)
Deliverables: chips as colored text by default, themeable; typing `#` opens a completion popup (Popover API, anchored under the caret) from existing tags; `o` and click on a tag sets the filter to that tag; Enter never follows a tag; palette "Tags" lists tags with counts (P2-03 rows); choosing one filters.
Acceptance criteria:
- AC1 Chips render in their color, readable on light and dark themes → `tag chips` (e2e).
- AC2 `#` completion inserts the chosen tag → `tag completion` (e2e).
- AC3 `o` on a bullet with one tag filters by it; with a tag and a link a picker appears → `tag follow` (e2e).

### P2-07 · Core: node links
Type: impl · Packages: `internal/core`, `internal/markup` (if needed) · Depends on: P2-02, P2-04 · Spec: §10.1, §5.3 (retarget), §5.4 (duplicate keeps link targets), §7.5 (export)
Deliverables: link resolution for display (current target text, markup stripped), `Backrefs` index (used by P3), retargeting in merges (replaces P1-13's no-op hook), Copy link to node formatting, Obsidian-style export per P2-02, link-aware `Plain` for search.
Acceptance criteria: per P2-02 outcomes, at minimum:
- AC1 A link displays the target's current text after the target is renamed and moved → `TestLinks_SurviveRenameMove`.
- AC2 Merging a linked-to bullet retargets inbound links in the same change set → `TestLinks_RetargetOnMerge`.
- AC3 Duplicate keeps link targets → `TestLinks_Duplicate`.
- AC4 Markdown export writes the agreed Obsidian form → `TestExportMarkdown_Links` (golden).

### P2-08 · UI: link insertion and following
Type: impl · Packages: `internal/server`, `internal/server/pageview`, `internal/web` · Depends on: P2-07, P2-12 · Spec: §10.1, §5.7 (page view links), §6.4 (`o`)
Deliverables: typing `[[` opens the jump-palette-style picker; choosing inserts the stored link form; `o` and click follow (zoom to target's parent with target selected, like the jump palette); page view Tab cycles node links too and Enter zooms to the target; palette "Copy link to node".
Acceptance criteria:
- AC1 `[[` + query + Enter inserts a link that renders as the target's text → `link insert` (e2e).
- AC2 `o` on a link zooms to the target → `link follow` (e2e).
- AC3 Page view Enter on a node link zooms there → `page node link` (e2e).

### P2-09 · Core: search query parser and operators
Type: impl · Packages: `internal/core` · Depends on: P2-04 · Spec: §6.2 (operators)
Deliverables: query grammar: terms, `"exact phrase"`, `-term`, `OR` between terms (`OR` binds tighter than the implicit AND, as in Workflowy: `a b OR c` = `a AND (b OR c)`; record it as a D-entry), operators `is:complete`, `is:open`, `is:task`, `has:page`, `is:starred`, `#tag` (with nested), `changed:Nd` (the spec names only `d`; also accept `w` and `m`, and log it as an A-entry), `created:YYYY`, `created:YYYY-MM`, `created:YYYY-MM-DD` (local time, §7.2). Unknown operators are plain terms. Highlighting only for text terms. Replaces D-021 semantics.
Acceptance criteria:
- AC1 Parser table (≥ 40 cases including the §6.2 examples) → `TestQuery_Parse`.
- AC2 Evaluation table on a fixture outline → `TestQuery_Eval`.
- AC3 `changed:7d` uses `updated_at` (not moves or stars) → `TestQuery_ChangedUsesUpdatedAt`.
- AC4 `BenchmarkFilter100k` with operators within 50 ms → benchmark.

### P2-10 · Operators in the filter and CLI search
Type: impl · Packages: `internal/server`, `internal/cli` · Depends on: P2-09
Acceptance criteria:
- AC1 Filter with `is:task is:open` shows open tasks → `filter operators` (e2e).
- AC2 `hyphal search "tooltip #bug is:open" --json` golden → `TestCLI_Search_Operators`.

### P2-11 · Stars
Type: impl · Packages: `internal/core`, `internal/cli`, `internal/server`, `internal/web` · Depends on: P2-03 · Spec: §6.6 (stars), §10.2 (`star`, `unstar`)
Deliverables: `PlanToggleStar(nodes)` (root can't be starred; to star the zoom target toggle it on the zoom heading); `s` in block selection; CLI `star N`, `unstar N`; `starred` in CLI node JSON; Stars view per P2-03; `is:starred` (P2-09).
Acceptance criteria:
- AC1 `s` toggles; undoable; star appears in history as `set_star` → `stars toggle` (e2e).
- AC2 CLI star/unstar JSON goldens → `TestCLI_Star`.
- AC3 Stars view lists starred bullets; Enter zooms → `stars view` (e2e).

### P2-12 · Jump palette and Move to…
Type: impl · Packages: `internal/server`, `internal/web`, `internal/core` · Depends on: P2-11, P2-03 · Spec: §6.2 (jump palette), §5.4 (Move to)
Deliverables: Ctrl+K fuzzy finder over all live node text with breadcrumbs, starred first (P2-03 rows); Enter zooms to the result's parent with the result selected; Move to… switches to the jump palette (starred first). Fuzzy over 100k nodes on the server (precomputed plain strings; stale results dropped).
Acceptance criteria:
- AC1 Ctrl+K, type, Enter → zoomed to the parent with the result selected → `jump zoom` (e2e).
- AC2 Starred results listed first → `jump starred first` (e2e).
- AC3 Move to uses the jump palette → `move to jump` (e2e).
- AC4 `BenchmarkJump100k` < 50 ms per keystroke (CI baseline).

### P2-13 · Scheduled export
Type: impl · Packages: `internal/core`, `internal/cli`, `internal/server`, `internal/config` · Spec: §7.5 (scheduled export), §13 (`export.*`)
Deliverables: `ScheduledExportIfDue(cfg, now)`: `export.format` markdown|json, `when` daily (due when `meta.last_export_at` > 24 h old) or quit; target `export.dir`; files named `<dbname>-<UTC stamp>.md|.json` (Markdown: a folder per export containing `outline.md` and `pages/`); keep the newest `export.keep`; checks at launch, quit and on the 60 s tick; `hyphal export --scheduled` runs it now if due (for systemd/launchd/cron); `docs/scheduled-export.md` with a systemd user timer and a launchd plist.
Acceptance criteria:
- AC1 Due logic and retention (fake clock) → `TestScheduledExport_DueAndKeep`.
- AC2 `when = "quit"` exports when the server exits → `TestScheduledExport_OnQuit`.
- AC3 `hyphal export --scheduled` exports only when due → `TestCLI_ExportScheduled`.

### P2-14 · Capture window
Type: impl · Packages: `internal/cli`, `internal/server`, `internal/web`, `docs/`, `packaging/` · Spec: §4.7 (capture window)
Deliverables: `/capture` route: `<hyphal-capture>`, one input; Enter sends `PlanAdd` to the Inbox and closes the window; Esc closes without adding. `hyphal capture --window` starts or finds the server, gets a ticket, and opens `/capture` as a small `--app` window with class `hyphal-capture`; the server's exit grace applies. Hyprland rule snippet (small, floating, centered) and a suggested Super binding checked against Omarchy's defaults in `docs/omarchy.md`. With no Chromium-family browser, and on macOS: a message, then inline capture. A capture page doesn't count as an editing tab.
Acceptance criteria:
- AC1 Typing + Enter adds to the Inbox and closes → `capture page` (e2e).
- AC2 Capture works while another tab is editing → `capture alongside editor` (e2e).
- AC3 The command has the right argv (fake exec); the fallback runs inline capture with a message → `TestCaptureWindow_Command`, `TestCaptureWindow_Fallback`.

### P2-15 · Edit here takeover
Type: impl · Packages: `internal/server`, `internal/web` · Spec: §8.6 (takeover)
Deliverables: a read-only tab's banner gets an `Edit here` action: seals the editing tab's typing, swaps roles, and shows a message in both. Nothing changes without the click or keypress.
Acceptance criteria:
- AC1 `Edit here` makes this tab editable and the other read-only, with no lost typing → `edit here` (e2e).

### P2-16 · History view
Type: impl · Packages: `internal/server`, `internal/web`, `internal/core` · Depends on: P2-03, P1-07 · Spec: §8.2 (History view), §5.6
Deliverables: per P2-03: full-page view; node versions (from ops touching the node) with restore-a-version (a new change set setting the fields to that version's values); change sets by time and source with revert (`PlanRevert`, skipped-fields message).
Acceptance criteria: per P2-03 mockups, at minimum:
- AC1 A node's past texts are listed newest first; restoring one creates a change set → `history node versions` (e2e).
- AC2 Reverting a CLI change set from the list leaves later UI changes intact → `history revert` (e2e).

### P2-19 · Mode switching with position sync; split view
Type: impl · Packages: `internal/web`, `internal/server/pageview` · Spec: §5.7 (switching, split view), §13 (`split_view_min_width`)
Deliverables: switching view↔edit keeps the same block on screen using pageview source ranges; `s` toggles split view (source left, rendered right, live update while typing). Split view uses the full window width with two equal panes, each capped at `column_width`; record this as a D-entry; `split_view_min_width` opens split by default at or above that width.
Acceptance criteria:
- AC1 Scrolled to the 5th heading in view mode, `e` puts the cursor on that heading's source line → `page position sync` (e2e).
- AC2 Split view updates the rendered side as you type → `split view live` (e2e).
- AC3 `split_view_min_width = 1600` in a 2000 px window opens split → `split view default` (e2e).

### P2-20 · Sort children
Type: impl · Packages: `internal/core`, `internal/server` · Spec: §6.6
Deliverables: palette-only "Sort children": alphabetical by folded plain text, completed last, one change set of `move` ops (only nodes whose position changes).
Acceptance criteria: `TestSortChildren` (order, completed last, undo restores order).

### P2-21 · CLI tag commands
Type: impl · Packages: `internal/cli` · Depends on: P2-05 · Spec: §10.2 ("Tag commands arrive with tags in Phase 2")
Deliverables (adjust per P2-01): `hyphal tag list --json` (tag, count, color), `hyphal tag rename OLD NEW` if approved.
Acceptance criteria: JSON goldens → `TestCLI_Tag_*`.

### P2-24 · Drag-to-move
Type: impl · Packages: `internal/web`, `internal/server`, `internal/core` · Spec: §6.5, §5.4
Deliverables: native HTML drag and drop from a bullet's glyph over `<hyphal-outline>`; a drop indicator showing parent and position (depth from the pointer's horizontal position); edge autoscroll within the row window; dragging a selected bullet carries the selection; Esc cancels; the drop calls `PlanMoveTo` with its anchor as one change set. Hovering a collapsed bullet never expands it by itself in this card. Drops outside the zoom or into the dragged subtree show no indicator and do nothing.
Acceptance criteria:
- AC1 Dragging a bullet between two others, and under a new parent, moves its subtree, undoable as one step → `drag move` (e2e, three engines).
- AC2 Dragging a selected bullet moves the whole selection in order → `drag selection` (e2e).
- AC3 Esc cancels; a drop into its own subtree changes nothing → `drag cancel and invalid` (e2e).
- AC4 Dragging past the edge of the row window scrolls and can drop there at 100,000 nodes → `drag autoscroll` (e2e).

### P2-22 · Parity suite for Phase 2 rows
Type: impl · Packages: `e2e/` · Spec: §3 (Phase 2 rows)
Deliverables: `parity tags`, `parity node links`, `parity search operators`, `parity starred locations`, `parity drag to move`; the interaction-matrix meta-test now includes Phase 2 bindings.

### P2-23 · Phase 2 gate
Type: gate · Depends on: all Phase 2 cards
Deliverables: `plan/reports/phase-2-gate.md` showing every *Must* row (Phases 1 and 2) with its passing parity test. Human sign-off line.
