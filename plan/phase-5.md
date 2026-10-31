# Phase 5 · Multi-device sync (outline only)

Spec: §11 row 5, §7.6, §12.3 (sync details, Phase 5 sync view). Exit gate: **two machines edit offline and converge with nothing lost.**

This phase is deliberately not broken into implementation cards. The design depends on open questions (§12.3) and on what Phases 0–4 teach. Nothing sync-specific may be built before this phase (§7.6), so no earlier card adds device IDs, clocks, or log formats.

## First card

### P5-01 · Write the Phase 5 plan
Type: decision · Spec: §7.6, §12.3

Produce `plan/proposals/p5-01-sync-design.md` and, after human approval, replace this file with full task cards in the format of `plan/README.md §3`. The design must cover:

1. **Device identity**: device ID generation and storage (a new `meta`/table entry; per-device, never exported in JSON? decide), human-readable device names.
2. **Clock**: hybrid logical clocks on change sets; drift handling (§12.3); persistence of the HLC; ordering ties by device ID.
3. **Schema migration** (v3 or next): `change_sets.hlc`, `change_sets.device_id`, sibling-position device tie-breaker (§7.6), tombstones for Forget (§7.6) so a forgotten node isn't resurrected by another device's log.
4. **Log format**: `<sync-folder>/hyphal/<device-id>.log` (§7.1): append-only, one change set per record, self-contained ops (they already are, §7.6), versioned header, checksum per record, documented precisely enough for the separate iPhone project (§11). Neutral naming (CONTRACTS §1): no app name inside the format.
5. **Merge**: per-field last-writer-wins by HLC for node fields and pages (whole-page LWW in V1; line-by-line merge is an open question, §12.3); tree moves with Kleppmann et al.'s replicated tree move algorithm (undo-do-redo by HLC order) so concurrent moves never create cycles; deletes vs edits (edit to a trashed node keeps it trashed but records the edit?); restore vs purge.
6. **Undo/revert under sync**: inverse ops are ordinary new ops; "changed since" uses merged state.
7. **Compaction**: when and how a device compacts its own log (thresholds, §12.3), and how new devices bootstrap (snapshot + log tail).
8. **Sync view** layout and keys (§12.3 Phase 5): devices, last seen, pending, errors; `hyphal sync status|now|init <folder>` CLI noun (§10.2 mentions `sync`).
9. **Safety**: the live DB never lives in the sync folder (§8.1); partial files from the sync tool (Syncthing temp files, iCloud placeholders) are ignored until complete; corrupted records are skipped and reported, never applied.
10. **Testing**: deterministic multi-device simulation (N in-process devices with independent DBs and a fake shared folder with delays, reordering, duplication, partial writes), convergence property tests (rapid): after all logs are exchanged, all devices' JSON exports (without per-device state) are identical; crash injection during log append; the exit-gate test with two real machines (human) plus an automated two-process test.

## Expected card areas (for orientation only)

Device ID + HLC (store/core) · schema migration · log writer · log reader/watcher · merge engine (fields) · tree-move CRDT · tombstones/Forget · compaction + bootstrap · sync CLI · sync view (UI) · simulation harness · convergence properties · docs (iCloud Drive, Syncthing setup) · gate.
