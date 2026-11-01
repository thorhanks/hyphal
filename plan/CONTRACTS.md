# Contracts

Formats and names that several packages, tests or external users depend on. Changing anything here needs Thor's approval first.

## 1. App name placement (§1)

`hyphal` / `Hyphal` may appear only in: the binary name, data/config/runtime directory names, the `.desktop` file and window class (`hyphal`, capture window class `hyphal-capture`), the env vars `HYPHAL_DB` and `HYPHAL_DEBUG_LOG`, the export header (JSON `format` and `exported_by`, OPML `<head>`, Markdown first-line comment), CLI help text and docs, and UI code (page title, custom element tag names, `/keyprobe`). **Never** in table or column names, `meta` keys or values (except `exported_by` in exports), op kinds, JSON field names, the UI protocol (§12), or the sync format.

## 2. IDs (§7.2)

- Node IDs: 12 characters from the lowercase Crockford base32 alphabet `0123456789abcdefghjkmnpqrstvwxyz` (no `i l o u`), 60 bits from `crypto/rand`. Regex: `^[0-9a-hjkmnp-tv-z]{12}$`. Example: `k3j9x2m4q7tb` (D-003).
- Change-set IDs: same format and generator.
- The root also has a random ID; it is stored in `meta.root_id`.
- `ids.Valid` is the only validator.

## 3. Positions (§7.2)

- Fractional index keys per Greenspan, "Implementing Fractional Indexing" (base-62 digits `0-9A-Za-z`, variable-length integer part whose head character encodes its length, then a fractional part with no trailing `0`). Keys compare with plain byte comparison, which matches SQLite's `BINARY` collation (D-004).
- `fracindex.Between("", "")` returns `a0`. `Between(a, b)` requires `a < b` (or an open end) and returns `a < k < b`.
- Sibling order is `(position, id)`: ties (possible after concurrent inserts from two connections) break by ID.
- `NBetween(a, b, n)` returns n evenly distributed keys (used by paste, import, duplicate).

## 4. Database schema v1 (§7.2, §8.2)

Exactly the spec's DDL, with these clarifications:

```sql
PRAGMA journal_mode=WAL;  PRAGMA synchronous=FULL;  PRAGMA foreign_keys=ON;
-- macOS additionally: PRAGMA fullfsync=ON (§9.4)

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE nodes (... as §7.2 ...);
CREATE TABLE pages (... as §7.2 ...);
CREATE TABLE stars (... as §7.2 ...);
CREATE TABLE view_state (... as §7.2 ...);
CREATE TABLE ui_state (... as §7.2 ...);
CREATE TABLE change_sets (... as §8.2 ...);
CREATE TABLE ops (... as §8.2 ...);
-- indexes exactly as §7.2 and §8.2
```

`meta` keys (v1):

| Key | Value |
| --- | --- |
| `schema_version` | `"1"` |
| `format_id` | `"tree-outline"` (neutral, D-005) |
| `root_id` | root node ID |
| `inbox_id` | Inbox node ID, absent until first capture |
| `ui_opened` | `"1"` once the UI has opened this DB (§4.5) |
| `description` | One paragraph explaining the tables (text in P0-06) |
| `last_snapshot_at` | timestamp of the newest good snapshot |
| `last_trash_purge_at` | timestamp of the last automatic purge (D-016) |
| `last_export_at` | scheduled export (P2) |

`ui_state` keys: `show_completed` (bool, default true), `zoom` (node ID string), `cursor:<zoom-id>` (`{"node": "<id>", "offset": <int>}`), `palette_recent` (array of command IDs, newest first, max 20).

Row rules enforced by `store.Commit`:

- Root: `parent_id IS NULL`; text is `""` (display name "Home" is UI-only); never moved, deleted, completed, starred, or given a page.
- `updated_at = change_set.at` when an op of kind `edit_text`, `edit_page`, `set_task`, `set_heading` or `set_complete` touches the node; not for `move`, `set_star`, `delete`, `restore` (§7.2).
- `nodes.version += 1` on every op that writes the node row (`insert` sets 1; `edit_text`, `move`, `set_task`, `set_heading`, `set_complete`, `delete`, `restore`). `edit_page` bumps `pages.version` (insert = 1) and also bumps `nodes.version` (D-006). `set_star` bumps nothing.
- `delete` sets `deleted_at = change_set.at`, `deleted_by = change_set.id` on the op's node (one op per node, D-008).
- Live nodes never have a trashed or missing parent; no cycles.

## 5. Op payloads (§8.2)

`before` and `after` are JSON objects holding **complete values of the changed fields**, never diffs. The store writes `after`; it recomputes `before` from current rows. Inverse of an op = same kind with `before` and `after` swapped, except the noted pairs.

| Kind | `before` | `after` | Inverse |
| --- | --- | --- | --- |
| `insert` | `null` | `{"parent_id","position","text","is_task","is_heading","completed_at","created_at"}` | `delete` of that node |
| `delete` | `{"deleted":false,"deleted_at":null,"deleted_by":null}` | `{"deleted":true,"deleted_at":"…","deleted_by":"<cs>","merged_into":"<id>"?}` | `restore` |
| `restore` | `{"deleted":true,"deleted_at":"…","deleted_by":"…"}` | `{"deleted":false,"deleted_at":null,"deleted_by":null}` | `delete` (with the before values) |
| `edit_text` | `{"text":"…"}` | `{"text":"…"}` | swap |
| `edit_page` | `{"markdown":"…"}` or `null` (create) | `{"markdown":"…"}` or `null` (delete) | swap |
| `move` | `{"parent_id","position"}` | `{"parent_id","position"}` | swap |
| `set_task` | `{"is_task":bool}` | same | swap |
| `set_heading` | `{"is_heading":bool}` | same | swap |
| `set_complete` | `{"completed_at":"…"\|null}` | same | swap |
| `set_star` | `{"starred_at":"…"\|null}` | same | swap |

- Nullable fields are present with `null`, never omitted (except `merged_into`, present only for merges).
- Ordering inside a change set matters: ops apply in `ops.id` order. Planners emit parents before children for inserts and restores, and children before parents for deletes.
- **"Changed since"** (revert and undo, §5.6, §8.2): a field is skipped when its current value differs from the op's `after` value. Skipped fields are reported as `{"node_id","kind","field"}`.
- Undo: new change set, `undo_of = <undone id>`, ops = inverses in reverse order. Redo: inverse of the undo change set, `undo_of = <undo change set id>` (D-009).

## 6. JSON export format (§7.5)

```json
{
  "format": "hyphal-export",
  "format_version": 1,
  "schema_version": 1,
  "exported_by": "hyphal 0.3.0",
  "exported_at": "2026-10-24T18:00:00.000Z",
  "meta": { "root_id": "…", "inbox_id": "…", "ui_opened": "1", "format_id": "tree-outline" },
  "nodes":      [ { "id","parent_id","position","text","is_task","is_heading","completed_at",
                    "created_at","updated_at","version","deleted_at","deleted_by" } ],
  "pages":      [ { "node_id","markdown","updated_at","version" } ],
  "stars":      [ { "node_id","starred_at" } ],
  "view_state": [ { "node_id","collapsed" } ],
  "ui_state":   [ { "key","value" } ],
  "history":    { "change_sets": [ { "id","at","source","description","undo_of" } ],
                  "ops": [ { "id","change_set_id","node_id","kind","before","after" } ] }
}
```

- Arrays are sorted by primary key (`id`, `node_id`, `key`) for deterministic output. `history` is present only with `--history`; otherwise the key is absent.
- `meta` contains every `meta` row except `schema_version`, `description` and `last_*` keys.
- `ui_state.value` and op `before`/`after` are embedded JSON (not strings).
- Booleans are JSON booleans (the DB stores 0/1). Timestamps are strings per §7.2.
- Import validates: format/format_version known, IDs valid, invariants hold (§8 below). On failure nothing changes.
- Saved searches (Phase 3) add `"saved_searches": [...]` and bump `format_version` to 2; importers accept 1 and 2.

## 7. CLI contract (§10.2)

### 7.1 Envelope

```json
{"ok": true,  "v": 1, "data": <command-specific>}
{"ok": false, "v": 1, "error": {"code": "ambiguous", "message": "…", "candidates": [{"id":"…","path":"…"}]}}
```

`candidates` is present only for `ambiguous`. With `--json`, nothing else is written to stdout; diagnostics go to stderr.

### 7.2 Error codes and exit statuses

| Code | Exit | When |
| --- | --- | --- |
| — | 0 | success |
| `usage` | 2 | bad flags/arguments (Cobra errors) |
| `not_found` | 3 | node, change set, snapshot or deletion not found |
| `ambiguous` | 3 | a path segment matches several siblings |
| `conflict` | 4 | `--if-version` mismatch |
| `busy` | 5 | SQLite busy past 5 s |
| `read_only` | 6 | DB has a newer schema |
| `invalid` | 1 | operation violates a rule (move under own descendant, edit root, text with newline) |
| `not_empty` | 1 | JSON import into a non-empty DB without `--replace` |
| `parse_error` | 1 | import/page file cannot be parsed |
| `server_running` | 1 | `snapshot restore` while a server holds the lock |
| `integrity` | 1 | `check` found problems (data lists them) |
| `io` | 1 | filesystem errors |
| `internal` | 1 | anything else |

### 7.3 Node references (D-010)

- An argument is tried as an ID first (valid format **and** exists, live or trashed as the command allows); otherwise as a path. `id:<id>` and `path:<path>` prefixes force one interpretation.
- Path: segments separated by `/`; `\/` is a literal slash and `\\` a literal backslash; leading `/` optional; empty path or `/` is Home. Segments match `markup.Plain(text)`: exact first, then case-insensitive (Unicode simple folding). Several matches at either stage → `ambiguous` with candidates. Trashed nodes never match paths.

### 7.4 Data shapes

```json
// Node (show, add, edit, set, move, complete, …)
{"id","parent_id","path","text","plain","is_task","is_heading","completed","completed_at",
 "created_at","updated_at","version","has_page","starred","child_count","children":[Node]?}
// "children" present only with --depth/tree. "starred" is false before Phase 2.

// Write commands return {"node": Node, "change_set": "<id>"}; delete returns {"deleted": "<id>", "count": N, "change_set": "<id>"}.

// ChangeSet (history list, revert result)
{"id","at","source","description","undo_of","ops":[{"node_id","kind","before","after"}]}
// history list omits "ops" unless --ops.

// revert: {"change_set":"<new id>","skipped":[{"node_id","kind","field"}]}
// trash list: [{"id":"<top node id>","change_set":"…","text","plain","breadcrumb","count","deleted_at","hidden":bool}]
// search: [{"node": Node, "matches":[{"start","end"}], "page_line": "…"?}]
// snapshot list: [{"name","size","created_at","reason"}]
// check: {"ok":bool,"problems":[{"kind","node_id","detail"}]}
// paths: {"data_dir","config_file","db","snapshots_dir","exports_dir","runtime_dir"}
```

Human-readable output is not a contract (but has goldens).

## 8. Tree invariants (`hyphal check`, `Tree.Check`, import validation)

1. Exactly one row with `parent_id IS NULL`, and its ID equals `meta.root_id`.
2. Every non-root node's `parent_id` references an existing row.
3. No cycles.
4. A live node's parent is live.
5. `pages`, `stars`, `view_state` rows reference existing nodes; the root has no page or star.
6. Text contains no `\n` or `\r`, and `markup.ConvertLeadingMarker(text)` leaves it unchanged.
7. IDs are valid (§2); positions are valid fractional keys.
8. `deleted_by` is set iff `deleted_at` is set.
9. `meta.inbox_id`, if set, references an existing row (it may be trashed).
10. `PRAGMA quick_check` returns `ok` (store only).

## 9. Files on disk

| What | Path | Mode |
| --- | --- | --- |
| Data dir | Linux `${XDG_DATA_HOME:-~/.local/share}/hyphal`, macOS `~/Library/Application Support/hyphal` | 0700 |
| DB | `<data>/outline.db` (+ `-wal`, `-shm`) | 0600 |
| Lock | `<db path>.lock` | 0600 |
| Snapshots | `<dir of db>/snapshots/<dbname>-<YYYYMMDDTHHMMSSZ>-<reason>.db`, reason ∈ `daily`, `pre-migration`, `pre-restore`, `pre-import` (D-014) | 0600 |
| Recovery files | `<data>/recovery/<dbname>-<YYYYMMDDTHHMMSSZ>.md` (dialect with `^id`s) | 0600 |
| Server state | `${XDG_RUNTIME_DIR}/hyphal/` else `<data>/tmp/`, file `server-<sha256 of db path, 16 hex>.json`: `{"port","secret","pid","version"}` (D-041) | 0600 |
| Themes | `${XDG_CONFIG_HOME:-~/.config}/hyphal/themes/*.css`, `user.css` | user's |
| Cookie secret | `<data>/session.key` | 0600 |
| Exports | `<data>/exports/` default for scheduled export | 0600 files |
| Config | `${XDG_CONFIG_HOME:-~/.config}/hyphal/config.toml` (both OSes) | user's |

## 10. Markdown export layout (§7.5)

- Single file `outline.md` (or stdout without `--out`): line 1 `<!-- Exported by hyphal <version> at <timestamp> -->`, then the dialect.
- A bullet with a page is followed by ` [≡](pages/<slug>-<id>.md)` appended to its line (D-011). Slug: `markup.Plain(text)` → lowercase → runs of non `[a-z0-9]` become `-` → trim `-` → cut to 50 bytes → `untitled` if empty.
- `--split`: one file per top-level node of the export root, named `<slug>-<id>.md`, plus `pages/`.
- Trashed nodes are never exported to Markdown or OPML.

## 11. Test notation (outline fixtures)

Used by core rule tests (as external `core_test` packages) and by `servertest` and `e2e` (TESTING §3). The parser lives in `internal/testgen`, which may import `core` and `store` and is imported only from tests.

```text
- Projects {zoom}
  - ControllerCraft {collapsed}
    - Development
  - Fix tool‸tip
  - ⟦Selected⟧ text
  - [x] Done task
  - ## Heading {page}
  - Picked {sel}
```

- Lines use the dialect. Trailing attributes in `{…}` separated by spaces: `zoom`, `collapsed`, `page` (has a page; `page=<text>` sets its Markdown), `sel` (block-selected), `star`, `id=<id>` (fix the ID), `trashed`.
- `‸` marks the text cursor (byte offset in the stored text at that point); `⟦`…`⟧` marks a text selection, with the cursor at `⟧` unless `‸` appears.
- Root children are top-level lines. `{zoom}` on a line zooms to it; otherwise zoom is Home.
- Parsing: `testgen.ParseOutline(s) (*Fixture, error)`; rendering back for assertions: `testgen.FormatOutline(tree, state) string`. Assertions compare normalized notation strings.

## 12. UI protocol (§9.3)

JSON text messages on one WebSocket at `/ws`, one socket per tab. Field names are neutral. Offsets are UTF-16 code units, the unit the browser reports; `core` converts. The protocol is internal to one binary and carries no compatibility promise across versions: `hello` compares `version`, and a mismatch makes the page reload.

### 12.1 Client → server

```json
{"t":"hello","version":"…","zoom":"<id>|null"}
{"t":"text","seq":12,"id":"<id>","text":"…","off":5}
{"t":"action","seq":7,"name":"enter","off":5,"end":5}
{"t":"action","seq":8,"name":"move","dir":-1}
{"t":"window","start":400,"count":200}
{"t":"query","seq":9,"kind":"palette","text":"col"}
{"t":"filter","seq":10,"text":"tooltip"}
{"t":"pagetext","seq":11,"id":"<id>","text":"…","final":false}
{"t":"blur"}
{"t":"settings","seq":13}
{"t":"setting","seq":14,"key":"density","value":"compact"}
{"t":"setting","seq":15,"reset":"appearance"}
```

- `action.seq` and `text.seq` are separate counters. Every `action` gets exactly one `state` reply echoing its `seq`. `text` gets no reply of its own; the next `state` carries `textAck`.
- Action names are `keymap` command IDs, plus internal ones (`set_cursor`, `block_extend`, …) listed in `internal/server/actions.go`. Unknown names return an error message and change nothing.
- On a read-only server, writes are refused whatever the client sends.
- `blur` from a window seals its typing. Every window gets a `state` after each change.

### 12.2 Server → client

```json
{"t":"welcome","version":"…","keymap":[…],"config":{"density":"comfortable","threadGlow":false},"readOnly":false}
{"t":"state","seq":7,"textAck":12,"total":10010,"start":380,"rows":[Row],
 "cursor":"<id>","off":5,"end":5,"block":["<id>"],"anchor":"<id>",
 "zoom":"<id>","crumbs":[{"id","text"}],"filter":"…","filterCount":12,
 "message":"…","banners":[{"kind","text","action"?}],"saving":false,"readOnly":false}
{"t":"result","seq":9,"kind":"palette","items":[…]}
{"t":"clipboard","text":"…"}
{"t":"page","id":"<id>","text":"…","version":3}
{"t":"theme","href":"/theme.css?v=<hash>"}
{"t":"settings","seq":14,"values":{"density":"compact","trash_days":30},"errors":{"trash_days":"…"},
 "config":{"density":"compact","threadGlow":false},"keymap":[…],"themes":["auto","light","dark"],
 "path":"…","locked":false}
{"t":"open","url":"https://…"}
{"t":"error","code":"…","message":"…"}
```

`Row`:

```json
{"id":"<id>","depth":2,"text":"<source>","runs":[Run],"kids":true,"collapsed":false,"hidden":14,
 "done":false,"task":false,"heading":false,"page":false,"star":false,"matches":[[3,8]]}
```

`Run`: `{"text":"…","style":["bold","italic"],"color":"red","href":"…"?}`. Marker ranges for the focused row: `{"markers":[[0,2],[6,8]]}` on that row. `depth` is `-1` for the zoom heading. `hidden` is the number of descendants, present only on collapsed rows; `core.Tree` keeps subtree sizes so sending it costs no walk.

### 12.3 Rules

- `state` is the whole truth for its row window. The client patches the DOM by node ID.
- **`textAck` rule:** if `textAck` is lower than the last typing `seq` sent, the client keeps its local text for that node (§7.3).
- **Queue rule:** while any action has no reply, the client queues keys and replays them in order. Composition events are never queued.
- **During composition,** the client defers applying `state` until `compositionend`.
- On reconnect the client sends `hello`, resends unacknowledged `text`, and renders the fresh `state`.
- `theme` tells the page to swap its theme stylesheet without a reload.
- `settings` answers `settings` and `setting`, echoing `seq` to the sender. After a change or a reload of `config.toml`, every window also gets one, without `seq`, and applies `config` and `keymap`. `values` uses the §13 key names. `errors` holds a rejected value's message, and `locked` is true while the modal must stay disabled (invalid or unwritable file). `reset` names a section: `appearance`, `data`, `export`, `startup` or `keys`.

### 12.4 HTTP routes

| Route | Purpose |
| --- | --- |
| `/?ticket=<t>` | Exchanges a single-use ticket (30 s life) for the session cookie, then redirects to `/` |
| `/`, `/assets/…` | Embedded UI (`no-store` in `--dev`, content-hash `ETag` otherwise) |
| `/theme.css`, `/user.css` | Active theme and user overrides; empty when absent |
| `/capture` | Capture page (Phase 2) |
| `/ws` | The WebSocket |
| `/assets/vendor/…` | CodeMirror files, requested only when a page opens |
| `/internal/ticket` | `POST` from `hyphal` with the state file's secret; returns a new ticket |
| `/keyprobe` | Key matrix page, `--dev` only |

Every route checks `Host` against `127.0.0.1:<port>` and `localhost:<port>`; `/ws` and all non-GET routes also check `Origin`. All responses carry `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'` (inline styles only for per-row `style` attributes set by script).
