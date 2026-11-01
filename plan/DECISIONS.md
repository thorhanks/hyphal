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

### D-007 · One FIFO worker for commits and reloads
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
Context: principle 4 says every change is recorded. Decision: purge (manual, Empty Trash, automatic by age) records a change set with no ops and a description such as `Purged 3 deletions (41 bullets) from Trash`. Source: `ui`, `cli`, or `cli` for automatic purges run by the CLI. Purges are not undoable.

### D-016 · When the automatic Trash purge runs
Context: §8.3 says purge after 30 days, without saying when. Decision: run at server start (after the first paint), on the server's 60 s tick if more than 24 h have passed since `meta.last_trash_purge_at`, and on CLI writes under the same 24 h rule.

### D-017 · Restore placement
Context: §8.3. Decision: if the original parent is live, reinsert at the original position key; if a live sibling has the same key, use `Between(that sibling, next)`. If the parent is not live, walk up `parent_id` through existing rows to the first live ancestor and append at its end; if the chain hits a purged row, append under Home. The message names the parent it landed under when that is not the original parent.

### D-018 · Debug logging
Decision: no logging to stdout from a detached server. When `HYPHAL_DEBUG_LOG=<path>` is set, write a structured `log/slog` text log there.

### D-019 · Withdrawn
Alternate screen; the terminal UI is gone (D-037).

### D-020 · Tabs and control characters in bullet text
Decision: bullet text is one line. On typing or single-line paste, `\t` becomes one space, other C0/C1 control characters except ZWJ-related ones are dropped, and `\r\n`/`\n` split lines (multi-line paste rules, §5.5).

### D-021 · Phase 1 filter semantics
Context: operators arrive in Phase 2 (§6.2). Decision: the Phase 1 query is split on whitespace into terms that are ANDed, each a folded substring match. Quotes, `-`, `OR` and operators are literal text until P2.

### D-022 · Indent parsing
The rest of this decision covered the `$EDITOR` round trip, cut in SPEC §12.2 row 58. What remains:
- Lines indented more than one level deeper than their predecessor attach as that predecessor's children.

### D-023 · Withdrawn
The terminal capture prompt was cut (SPEC §12.2 row 60). `hyphal add` ships in Phase 1 and the `hyphal capture` window in Phase 2.

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
Decision: the UI starts in text editing on the remembered cursor for the remembered zoom, or on the first bullet. An empty zoom shows the virtual bullet (D-036).

### D-033 · Messages for stale writes
Decision: when a commit returns `Stale` nodes (another connection changed them first), the footer message is `"<text>" was also changed elsewhere; both changes are in history`. For `ErrInvalid`: `The outline changed elsewhere; nothing was changed`.

### D-034 · Phase 1 pages follow URLs only
Decision: in Phase 1, Ctrl+click and Ctrl+Enter open URL links on a page in a new tab. `[[node links]]` join in Phase 2.

### D-035 · Task and heading markers combine
Context: §5.4 and §7.5 list the markers one at a time. Decision: a bullet can be both a task and a heading. `ConvertLeadingMarker` takes a task marker first, then a heading marker (`[ ] ## Title`). The dialect writes them in the same order.

### D-036 · No zoom heading at Home; a virtual bullet in empty zooms
Context: §4.1 shows the zoom target as an editable heading, but Home can't be renamed. Decision:
- At Home there is no heading row; the breadcrumb alone shows `Home`.
- When the current zoom has no visible children (or none at all), the outline shows one **virtual empty bullet**. It is not in the database. The first typed character, Enter or paste turns it into a real node (one insert, then normal typing). Esc on it enters block selection on nothing: the footer shows the hint `Nothing to select`. This replaces D-032's "empty zoom shows the heading focused".

### D-037 · Web UI replaces the terminal UI
Context: Thor's decision of 2026-10-31 after spike P0-W1. Decision: SPEC v4. Cards P0-17 (layout), P1-04 (clipboard) and the Bubble Tea stack are withdrawn. `core`, `store` and the leaf packages keep their designs. Finished cards P0-01 to P0-05 stand. `source` value `tui` becomes `ui`, and `meta.tui_opened` becomes `ui_opened`; no database exists yet, so no migration is needed.

### D-038 · No PRs or branches
Context: Thor's instruction of 2026-10-31. Decision: agents commit and push directly to `main` (README §4). The old "Human review: yes" card flag is gone; the things it protected now fall under "stop and ask" in README §5.

### D-039 · Plain JavaScript and no build step
Context: Thor's "HTML, CSS, JS". The spike's TypeScript and esbuild are not carried over. Decision: ES modules served as written. Types are JSDoc comments. A type checker would be an extra dev tool, and isn't used unless Thor approves one. Browser tests are `.mjs`.

### D-040 · Light DOM for all components
Context: theming (§4.4) and text selection. Decision: no Shadow DOM. Selection APIs don't pierce shadow roots consistently across engines, and shadow styles would make themes harder. The cost is no style encapsulation, handled by cascade layers and element-name-scoped nesting.

### D-041 · Launch tickets
Context: the spike put its secret in the launch URL, which other local processes can read from the process list (P0-W1 report). Decision: `hyphal` reads a `0600` state file holding a server secret, `POST`s it to get a single-use ticket with a 30 s life, and opens a URL with that ticket. The session cookie is the real credential.

### D-042 · Markers on the focused bullet use the Custom Highlight API
Context: §4.6, §5.8. Concealed markers must reappear on the focused bullet, dimmed, without touching the text nodes being edited. Decision: focused rows hold only source text; the server sends marker ranges and `CSS.highlights` paints them. Other rows render runs. No markup parser in JavaScript. Where the API is missing, markers show undimmed.

### D-043 · Readline keys dropped on Linux
Context: §6.4. Web convention and the browser's own behavior beat terminal convention. Decision: Ctrl+A selects all in the bullet on Linux, and Ctrl+E, Ctrl+U and Ctrl+W are unbound (Ctrl+W is reserved anyway). macOS keeps its native Ctrl+A and Ctrl+E. Alt+b and Alt+f are gone. Thor may overrule at review.

### D-044 · One session per server
Context: §8.6. Decision: one `core.Outline` and one undo stack per server, shared by all windows of that database; zoom, cursor, block selection, filter and row window are per window. Any window may write; there are no roles.

### D-045 · Bundled fonts and drawn glyphs
Context: SPEC §4.6, row 57. The agreed look depends on Literata, Fraunces and custom glyphs, and D-039 allows no build step. Decision: self-host both fonts as WOFF2 in `internal/web/assets` with their SIL Open Font License text; the CSP already serves same-origin assets. Fonts are assets rather than frontend libraries, so they carry no code. Glyphs and icons are hand-written SVG strings in `lib/glyphs.js`, with no sprite tool. Threads are one overlay SVG computed from row geometry. `plan/reports/design-mockup.html` is the reference for shapes and values; its code is not carried over.

### D-046 · CodeMirror as one vendored file
Context: SPEC §5.7, §9.1, spike P0-W2. CodeMirror needs bundling, and D-039 keeps our own code unbuilt. Decision: CodeMirror packages are bundled once into committed files under `internal/web/assets/vendor/`, which re-export the APIs we use. Our modules import from them as written, so D-039 still holds for our code. esbuild runs only through `make vendor-js`. CodeMirror's base styles are unlayered, so a small token adapter sets the properties they fix (font, line height, column, padding) to `var()` values; everything else is `page.css`. Page undo: CodeMirror's history within a visit; the outline's stack holds one step per visit, whose inverse restores the page and title to their state at opening. Seals still write change sets to history.

### D-047 · Web review of 2026-11-01
Context: Thor approved the web UI review (SPEC §12.2 rows 60, 61). Decision: the port is the first free one from `40000 + (FNV-1a of the absolute database path mod 10000)`, trying 16 in order. The cookie is `hyphal_<port>`, signed with `<data>/session.key`, valid for a year; a request without it gets a page saying to run `hyphal`. Cards marked "Also (D-047)" carry the smaller additions. `rivo/uniseg` stays: `textfold` uses it for grapheme-safe match ranges.

### D-048 · Settings modal over config.toml
Context: Thor chose a settings modal (SPEC §4.8, §12.2 row 62). go-toml's marshaler drops comments, and `config` may not import `platform` (ARCHITECTURE §2). Decision: `config.SetKey` and `config.ResetKey` are pure text edits that use parser positions to change only the affected line: a commented-out key is uncommented, a reset comments the line out again, and everything else stays byte for byte. `server` does the atomic write through `platform.WriteFileAtomic`, then runs the normal reload path, so hand edits and modal edits follow one route. A folder for `export.dir` is a typed path, since a browser can't return one. The modal is its own `<hyphal-settings>` component sharing the overlay's dialog look, because `<hyphal-overlay>` is a filtered list.

### D-049 · Inverse payloads in CONTRACTS §5
Context: review of P0-07, approved by Thor on 2026-11-01. `Inverse` produced two payloads the table didn't allow, and A-002 recorded only part of this. Decision: CONTRACTS §5 now allows `merged_into` on a restore's `before`, so a merge's delete inverts twice exactly, and says `store.Commit` stamps the null `deleted_at` and `deleted_by` of a delete made from an insert. P0-08 AC9 tests both.

## Agent decisions

<!-- Append A-001, A-002, … below using the format in README §5. -->

### A-001 · Driver spike as a nested module (P0-05, 2026-10-26)
Context: P0-05 compares two SQLite drivers, but `go.mod` must pin only the chosen one; the card allows deleting the spike or keeping it as a benchmark.
Decision: keep `internal/store/driverspike/` as its own Go module (own `go.mod`), run by `.github/workflows/driverspike.yml` on changes and on demand. `make check` and `./...` in the main module do not see it.
Alternatives: delete the spike after measuring (numbers not reproducible); build tags in the main module (the losing driver would still be in `go.mod`).

### A-002 · Inverting an insert twice (P0-07, 2026-11-01)
Context: P0-07 AC3 asks for `Inverse(Inverse(op)) == op` for every kind, but CONTRACTS §5 makes insert invert to delete and delete to restore, so an insert inverted twice is a restore. D-009 and D-013 need this: redoing an undone insert restores the node.
Decision: follow CONTRACTS §5. The double inverse is exact for every kind except insert, and `TestInverse` checks that insert ends at restore. The delete made from an insert has `deleted_at` and `deleted_by` null, since `Inverse` knows neither; `store.Commit` stamps them (D-049). `ChangedFields` takes a `Current` struct and also returns an error for a malformed payload.
Alternatives: make `Inverse` of a delete return an insert when it came from one (the op doesn't record that, and a re-insert would conflict with the node's row).
