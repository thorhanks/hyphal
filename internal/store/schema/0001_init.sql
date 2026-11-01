-- Schema v1 (SPEC §7.2, §8.2, CONTRACTS §4). Pragmas are set per connection.

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);

CREATE TABLE nodes (
  id           TEXT PRIMARY KEY,     -- random, stable forever: 12 chars, lowercase base32
  parent_id    TEXT REFERENCES nodes(id),
  position     TEXT NOT NULL,        -- fractional index among siblings
  text         TEXT NOT NULL,        -- one line, inline markup as typed
  is_task      INTEGER NOT NULL DEFAULT 0,
  is_heading   INTEGER NOT NULL DEFAULT 0,  -- heading-style bullet
  completed_at TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  version      INTEGER NOT NULL DEFAULT 1,  -- bumped on every write
  deleted_at   TEXT,                 -- soft delete; non-null = in Trash
  deleted_by   TEXT                  -- change set that deleted it
);

CREATE TABLE pages (                 -- at most one page per bullet
  node_id    TEXT PRIMARY KEY REFERENCES nodes(id),
  markdown   TEXT NOT NULL,          -- full Markdown document
  updated_at TEXT NOT NULL,
  version    INTEGER NOT NULL DEFAULT 1   -- bumped on every write
);

CREATE TABLE stars (
  node_id    TEXT PRIMARY KEY REFERENCES nodes(id),
  starred_at TEXT NOT NULL
);

CREATE TABLE view_state (            -- per device; not in history or sync
  node_id   TEXT PRIMARY KEY REFERENCES nodes(id),
  collapsed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE ui_state (              -- per device; not in history or sync
  key   TEXT PRIMARY KEY,            -- show_completed, zoom, cursor:<zoom-id>, palette_recent
  value TEXT NOT NULL                -- JSON
);

CREATE INDEX nodes_by_parent  ON nodes(parent_id, position);
CREATE INDEX nodes_in_trash   ON nodes(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE change_sets (
  id          TEXT PRIMARY KEY,  -- random
  at          TEXT NOT NULL,     -- timestamp
  source      TEXT NOT NULL,     -- 'ui', 'cli', 'import', 'restore', 'agent:<name>'
  description TEXT,              -- e.g. 'Moved "Design notes"'
  undo_of     TEXT               -- change set this one undoes
);
CREATE INDEX change_sets_by_at ON change_sets(at);

CREATE TABLE ops (
  id            INTEGER PRIMARY KEY,
  change_set_id TEXT NOT NULL REFERENCES change_sets(id),
  node_id       TEXT NOT NULL,   -- no foreign key: purging a node keeps its history
  kind          TEXT NOT NULL,   -- insert, delete, restore, edit_text, edit_page, move,
                                 -- set_task, set_heading, set_complete, set_star
  before        TEXT,            -- JSON: prior values of the changed fields
  after         TEXT             -- JSON: complete new values, never diffs
);
CREATE INDEX ops_by_node ON ops(node_id);
CREATE INDEX ops_by_change_set ON ops(change_set_id);
