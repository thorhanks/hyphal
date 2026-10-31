# Decisions

`D-xxx`: made by the planner while turning SPEC.md into this plan (2026-10-24). They fill gaps; none of them changes a spec *Must* row. The human may overrule any of them before the card that uses it starts.
`A-xxx`: added by building agents under the gap protocol (README §5). Append at the end.

## Planner decisions

### D-001 · Leaf packages under the four layers
Context: §9.2 says "four Go packages". Decision: the four are architectural layers; small dependency-free leaf packages (ARCHITECTURE §1) hold shared code. The human approved this on 2026-10-24.

### D-002 · Markdown and OPML interchange live in `core`
Context: §9.2 puts import/export in `store`. Decision: `store` owns the lossless JSON format (row-level). Markdown/OPML export reads the tree, and Markdown/OPML import creates nodes through planners (source `import`), so both live in `core`.

### D-003 · ID alphabet
Context: §7.2 "12 chars, lowercase base32"; the spec's example `k3j9x2m4q7tb` contains `9`, which RFC 4648 base32 lacks. Decision: lowercase Crockford base32 (CONTRACTS §2). Change-set IDs use the same format.

### D-004 · Fractional index algorithm
Decision: Greenspan base-62 fractional indexing (CONTRACTS §3); ties are broken by ID.

### D-005 · `format_id`
Decision: `meta.format_id = "tree-outline"` (neutral per §1, since it lives in the database). The JSON export `format` is `"hyphal-export"`: it marks an export, like `exported_by`, so it may carry the app name. Renaming the app later keeps accepting `"hyphal-export"` on import.

### D-006 · Version bumps
Context: §7.3 "every write bumps the node's version". Decision: every op that writes the `nodes` row bumps `nodes.version`. `edit_page` bumps `pages.version` and also `nodes.version`, so one `--if-version` on a node covers its page. `set_star` bumps nothing. `hyphal page set --if-version` compares `pages.version`.

### D-007 · One FIFO worker for TUI commits and reloads
Context: §7.3, §8.1, §9.3. Decision: a single worker goroutine runs commit and reload jobs in order. A failed commit blocks later jobs and is retried every 3 s, which keeps history order equal to action order.

### D-008 · One `delete` op per node
Context: §8.3 sets `deleted_at` on every node in the subtree. Decision: a subtree delete emits one `delete` op per node, children before parents, so revert and "changed since" work per node.

### D-009 · Redo is the inverse of the undo
Context: §5.6. Decision: redo commits the inverse of the undo change set, with `undo_of` set to the undo change set's ID.

### D-010 · Node references in the CLI
Context: §10.2. Decision: an argument is tried as an existing ID first, then as a path. `id:` and `path:` prefixes force one. Empty or `/` is Home. `\/` escapes a slash and `\\` a backslash (CONTRACTS §7.3).

### D-011 · Page links in Markdown export
Context: §5.7 and §7.5 say the outline links to each page file, without a form. Decision: append ` [≡](pages/<slug>-<id>.md)` to the bullet's line. Markdown import strips a trailing link whose target matches `pages/*.md` and, when the page file exists next to the import file, attaches it as the page.

### D-012 · TOML library
Decision: `github.com/pelletier/go-toml/v2`, using its `unstable` parser to find key line numbers for value errors (§13 "a banner names the file and line").

### D-013 · Undo of an insert soft-deletes
Context: §5.6 says undo applies the inverse. Decision: the inverse of `insert` is `delete` (soft). Empty-leaf undos are hidden from the Trash view by §8.3's rule. A larger undone paste shows in the Trash, which is acceptable because the Trash is never wrong about what existed.

### D-014 · Snapshot location and names
Context: §7.1 puts `snapshots/` in the data directory. Decision: snapshots go in `<dir of db>/snapshots/`, which for the default DB is the data directory. Names follow `<dbname>-<UTC stamp>-<reason>.db`, and retention (`snapshots.keep`) is counted per DB name.

### D-015 · Purges record an empty change set
Context: principle 4 says every change is recorded. Decision: purge (manual, Empty Trash, automatic by age) records a change set with no ops and a description such as `Purged 3 deletions (41 bullets) from Trash`. Source: `tui`, `cli`, or `cli` for automatic purges run by the CLI. Purges are not undoable.

### D-016 · When the automatic Trash purge runs
Context: §8.3 says purge after 30 days, without saying when. Decision: run at TUI launch (after the first frame), on the TUI's 60 s tick if more than 24 h have passed since `meta.last_trash_purge_at`, and on CLI writes under the same 24 h rule.

### D-017 · Restore placement
Context: §8.3. Decision: if the original parent is live, reinsert at the original position key; if a live sibling has the same key, use `Between(that sibling, next)`. If the parent is not live, walk up `parent_id` through existing rows to the first live ancestor and append at its end; if the chain hits a purged row, append under Home. The message names the parent it landed under when that is not the original parent.

### D-018 · Debug logging
Decision: no terminal logging. When `HYPHAL_DEBUG_LOG=<path>` is set, write a structured `log/slog` text log there.

### D-019 · Alternate screen
Decision: the TUI uses the alternate screen and hides the terminal cursor except at the text-editing cursor (§4.3).

### D-020 · Tabs and control characters in bullet text
Decision: bullet text is one line. On typing or single-line paste, `\t` becomes one space, other C0/C1 control characters except ZWJ-related ones are dropped, and `\r\n`/`\n` split lines (multi-line paste rules, §5.5).

### D-021 · Phase 1 filter semantics
Context: operators arrive in Phase 2 (§6.2). Decision: the Phase 1 query is split on whitespace into terms that are ANDed, each a folded substring match. Quotes, `-`, `OR` and operators are literal text until P2.

### D-022 · Editor round-trip scope
Context: §6.3. Decision:
- A focused bullet: the file holds that bullet and its descendants. Top-level lines are re-parented under the bullet's parent at its position.
- A block selection: the selected bullets, same rule.
- A focused zoom heading: the zoom target's children. The target itself is not in the file, and top-level lines become its children.
- An `^id` of a node outside the edited set, or of a trashed node, starts a new node; the original is never moved from elsewhere.
- Lines indented more than one level deeper than their predecessor attach as that predecessor's children.

### D-023 · Plain `hyphal capture` ships in Phase 1
Context: §4.7 puts the capture window in Phase 2; quick capture is Phase 1. Decision: the one-line `hyphal capture` prompt ships in Phase 1 (P1-34); `--window` ships in Phase 2.

### D-024 · Root text
Decision: the root row's `text` is `""`. "Home" is a display label in breadcrumbs, the zoom heading and paths, never stored.

### D-025 · `o` in Phase 1
Decision: in Phase 1, `o` follows URL links (`[label](url)`) only. Node links and tags join in Phase 2. With no link the hint is `No link on this bullet`.

### D-026 · Expand to level N
Context: §6.6. Decision: for each selected node S, S is expanded, and each descendant at relative depth k (children have k = 1) is expanded iff k < N. Leaves are unaffected. `0` expands all descendants. The palette-only "Collapse all" collapses every node in the current zoom.

### D-027 · Hidden Trash entries
Context: §8.3. Decision: a deletion is hidden when (a) its top node's `delete` op has `merged_into`, or (b) it deleted exactly one node, that node's text is empty, and it had no page at deletion time.

### D-028 · Move to… in Phase 1
Context: §5.4. Decision: an overlay built on the command-palette list component that lists every live node (excluding the selection and its descendants, and including Home) with its breadcrumb, fuzzy-matched on plain text. Enter moves the selection to the end of the chosen parent's children and shows a message with Ctrl+Z.

### D-029 · Paste at the zoom heading
Decision: a single-line paste inserts into the heading's text. A multi-line paste inserts the subtree as the zoom target's first children.

### D-030 · Copy and cut of a text selection
Decision: in text editing with a text selection, Ctrl+C copies the selected source text, markers included, and Ctrl+X cuts it as a text edit. Neither is a node operation.

### D-031 · CLI envelope version
Decision: the envelope carries `"v": 1` (CONTRACTS §7.1).

### D-032 · Text-editing state on startup
Decision: the TUI starts in text editing on the remembered cursor for the remembered zoom, or on the first bullet. An empty zoom shows the virtual bullet (D-036).

### D-033 · Messages for stale writes
Decision: when a commit returns `Stale` nodes (another connection changed them first), the footer message is `"<text>" was also changed elsewhere; both changes are in history`. For `ErrInvalid`: `The outline changed elsewhere; nothing was changed`.

### D-034 · Phase 1 page view follows URLs only
Decision: in Phase 1, Tab cycles through URL links in page view and Enter opens them with the URL opener. `[[node links]]` join in Phase 2 with node links.

### D-035 · Task and heading markers combine
Context: §5.4 and §7.5 list the markers one at a time. Decision: a bullet can be both a task and a heading. `ConvertLeadingMarker` takes a task marker first, then a heading marker (`[ ] ## Title`). The dialect writes them in the same order.

### D-036 · No zoom heading at Home; a virtual bullet in empty zooms
Context: §4.1 shows the zoom target as an editable heading, but Home can't be renamed. Decision:
- At Home there is no heading row; the breadcrumb alone shows `Home`. The palette command "Edit zoom in $EDITOR" opens the whole zoom (D-022) at any level, including Home.
- When the current zoom has no visible children (or none at all), the outline shows one **virtual empty bullet**. It is not in the database. The first typed character, Enter or paste turns it into a real node (one insert, then normal typing). Esc on it enters block selection on nothing: the footer shows the hint `Nothing to select`. This replaces D-032's "empty zoom shows the heading focused".

## Agent decisions

<!-- Append A-001, A-002, … below using the format in README §5. -->

### A-001 · Driver spike as a nested module (P0-05, 2026-10-26)
Context: P0-05 compares two SQLite drivers, but `go.mod` must pin only the chosen one; the card allows deleting the spike or keeping it as a benchmark.
Decision: keep `internal/store/driverspike/` as its own Go module (own `go.mod`), run by `.github/workflows/driverspike.yml` on changes and on demand. `make check` and `./...` in the main module do not see it.
Alternatives: delete the spike after measuring (numbers not reproducible); build tags in the main module (the losing driver would still be in `go.mod`).
