# Phase 4 · Agent integration

Spec: §11 row 4, §10.2, §8.4 (Forget). Scope: batch `apply`, `forget`, the agent usage guide, richer query output. No MCP server (§10.2).

Exit gate (P4-06): **an agent completes a scripted multi-step task with `apply` and the usage guide, and one `hyphal revert` undoes it as a unit.**

## Dependencies

```text
P4-01 richer-queries decision ─▶ P4-04 richer queries
P4-02 apply ─┐
P4-03 forget ├─▶ P4-05 agent guide ─▶ P4-06 gate
P4-04 ───────┘
```

### P4-01 · Decide: "richer query output"
Type: decision · Spec: §10.2 ("richer query output"), §11 row 4
The spec doesn't define it. Proposal `plan/proposals/p4-01-queries.md` with options, for example: `show`/`tree`/`search` flags `--fields`, `--with-pages`, `--with-breadcrumbs`, `--format ndjson`, `--since <change-set>` (changes since a point, for agents that poll), `hyphal diff <cs1> <cs2>`, `--limit/--cursor` pagination. Recommend a minimal set. Stop for the human.

### P4-02 · `hyphal apply`
Type: impl · Packages: `internal/core`, `internal/cli` · Spec: §10.2 (`ops.json`, `apply`)
Deliverables:
- `ops.json`: JSON array of `{"op": "<name>", ...args}` where names and args mirror the CLI commands (`add`, `edit`, `set`, `move`, `complete`, `reopen`, `delete`, `star`, `unstar`, `page_set`, `page_delete`, `restore`), node references as in CONTRACTS §7.3, plus `"ref": "<temp name>"` on `add` and `"$ref:<temp name>"` usable wherever a node reference is accepted, plus optional `"if_version"`.
- All ops are planned against an evolving in-memory tree and committed as **one change set** (source from `--source`), atomically: any error → nothing applied, error envelope names the failing op index.
- `hyphal apply --schema` prints a JSON Schema (draft 2020-12) generated from Go types (keep the schema in `internal/cli/schema/ops.schema.json`, with a test that it matches the types).
- `--dry-run` prints the planned ops without committing (ask Thor; useful for agents).
Acceptance criteria:
- AC1 A 10-op batch with refs applies as one change set → `TestApply_Batch`.
- AC2 Any failing op → nothing applied, error has `index` → `TestApply_Atomic`.
- AC3 `--schema` validates every example in `testdata/apply/*.json` and rejects the invalid ones → `TestApply_Schema`.
- AC4 `if_version` mismatch → `conflict`, exit 4 → `TestApply_Conflict`.

### P4-03 · Forget
Type: impl · Packages: `internal/store`, `internal/core`, `internal/cli`, `internal/server`, `internal/web` · Spec: §8.4 (Forget)
Deliverables: `store.Forget(deletion)`: in one transaction erase the deletion's subtree (plus earlier-trashed descendants, §8.3) from `nodes`, `pages`, `stars`, `view_state`, **and** every `ops` row mentioning those node IDs (change sets left with no ops stay, with their descriptions), then record a change set with no ops whose description says what was forgotten without its content (§8.4): `Forgot a deletion of N bullets`. CLI `hyphal forget <id> --yes` (without `--yes`: usage error naming it); output notes that snapshots still contain the data until they rotate out. UI: palette command "Forget…" in the Trash view on the selected entry, with a confirmation naming the snapshot caveat.
Acceptance criteria:
- AC1 After forget, no row in any table contains the node IDs or texts → `TestForget_Erases` (full-DB text scan).
- AC2 The forget change set has no ops and no content → `TestForget_ChangeSet`.
- AC3 CLI requires `--yes` and mentions snapshots → `TestCLI_Forget`.

### P4-04 · Richer query output
Type: impl · Packages: `internal/cli`, `internal/core` · Depends on: P4-01
Deliverables and ACs per P4-01; every new flag has a JSON golden.

### P4-05 · Agent usage guide
Type: impl · Packages: `docs/agents.md`, `internal/cli` · Depends on: P4-02, P4-03, P4-04 · Spec: §10.2 ("a shipped usage guide for agents")
Deliverables: `docs/agents.md`, also embedded and printed by `hyphal help agents`: principles (never touch the DB, always `--json`, `--source agent:<name>`, `--if-version` for safe edits, `apply` for multi-step changes, `revert` for rollback, paths vs IDs, exit codes), recipes (capture to Inbox, find open tasks under a project, reorganize a subtree with `apply`, review and revert an agent's changes with `history list --source`), the envelope and error table, and the `ops.json` schema pointer.
Acceptance criteria:
- AC1 Every command example in the guide runs successfully against a fixture DB in a test (extracted from fenced blocks tagged `bash agent-example`) → `TestAgentGuide_ExamplesRun`.

### P4-06 · Phase 4 gate
Type: gate · Depends on: P4-02…P4-05
Deliverables: `internal/cli/agent_e2e_test.go`: a scripted "agent" (a Go test following only the guide's documented commands) performs a multi-step task — find a project by path, add 5 tasks with `apply`, move 2 existing bullets under it, complete one, set a page — then `hyphal revert <cs>` restores the original state exactly (compare JSON exports without history). Report `plan/reports/phase-4-gate.md`. Optionally, the human runs a real coding agent against the guide; record the transcript summary.
Acceptance criteria:
- AC1 `TestAgentE2E_ApplyAndRevert` passes.
