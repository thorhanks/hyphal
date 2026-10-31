# Proposal: replace the TUI with a local web UI

Status: spike proposed, 2026-10-31. Nothing in `SPEC.md` or the phase files changes until the human accepts the outcome.

## Direction

One Go binary serves a browser UI on loopback, using the same SQLite file and CLI. No hosted version, accounts or remote access; §2.6 stays in force, with its wording to be adjusted for a loopback listener. Thor chose this over a native app and over staying with the TUI.

## Card impact while the spike runs

| Cards | State |
| --- | --- |
| P0-05 to P0-15, P0-16, P0-20, P0-21 (store, core, markup, performance) | Unaffected; may continue |
| P0-17 `layout`, P0-18 TUI skeleton, P0-19 keyprobe, P0-23 keymap matrix | On hold until the outcome below |

P0-22 (gate) and Phase 1 wait for the outcome.

## P0-W1 · Local web UI spike

Type: spike
Packages: `internal/webspike/` only, as its own Go module (same pattern as A-001), so the main `go.mod` stays unchanged
Depends on: none. Uses an in-memory tree with a small fake core, not `internal/core`
Spec: §5.1–5.4, §5.6, §6.3, §6.4, §4.4, §9.4
Human review: yes. Stop after the report

Goal: find out whether browser editing can meet Workflowy-grade behavior and the §9.4 latency targets, with agent-runnable tests, before any rewrite of the spec.

Suggested approach:

- Go server with `go:embed` assets and one WebSocket. TypeScript with no UI framework, bundled by esbuild.
- Virtualized rows, with `contenteditable="plaintext-only"` on the focused row. Compare it with a `<textarea>` swapped into the focused row if it causes trouble.
- Go owns structure rules. The browser applies typing immediately and sends structural actions to the server.

Acceptance criteria (Playwright on Chromium, Firefox and WebKit unless noted):

- AC1 Enter and Backspace: every row of the §5.2 and §5.3 tables behaves as written, including the cursor position afterward.
- AC2 Navigation: Up and Down cross wrapped lines and bullets at a stable column. Shift+arrows select text within a bullet, and block selection works per §5.1.
- AC3 Keys: every §6.4 binding that browsers reserve (such as Ctrl+T, Ctrl+W, Ctrl+N) is listed in the report, in a normal tab and in a `chromium --app` window. Propose a replacement for each one lost.
- AC4 Undo: Ctrl+Z follows §5.6, with the browser's native undo never interfering.
- AC5 Input methods: composition (Chinese or Japanese IME events), emoji and combining marks do not corrupt text or the cursor. Automated with CDP events on Chromium; real IME use is a human check.
- AC6 Scale: at 100,000 nodes, with 10,000 expanded, report keypress-to-paint (target < 16 ms, §9.4), Enter round trip, cold launch to first paint (target < 100 ms) and filter update (target < 50 ms).
- AC7 Theme: a change to `~/.local/state/omarchy/current/theme/colors.toml` recolors the page without a reload, and the mode, palette and font are applied. Tests use a temp path, never the real directory.
- AC8 Security: the listener binds to loopback only; a request without the per-launch token, or with a wrong `Host` or `Origin`, is refused (DNS rebinding and cross-site WebSocket cases).
- AC9 Tabs: a second tab on the same database is read-only with a banner, or the report proposes a better rule.
- AC10 Testing: the whole suite runs headless in CI on Linux and macOS, and flakiness over 20 consecutive runs is reported.

Report in `plan/reports/p0-w1-web-ui.md`, also covering:

- `$EDITOR` round trip from a browser (§6.3): spawning a terminal through `xdg-terminal-exec` and waiting for it, or an alternative.
- Clipboard: Markdown copy (§5.5) and the permission prompts it triggers.
- Launch: what `hyphal` does (start or reuse a server, open which browser, shutdown after the last tab) and the window class under Hyprland.

Human checks, prepared by the agent: a daily-driver feel test against Workflowy, and real IME use if relevant.

Out of scope: pages, real storage, palettes, visual polish, and any change to `SPEC.md`.

## Outcome rules

Thor decides after reading the report. The recommendation must be one of:

- **Go:** AC1–AC10 pass, or their failures have workable fixes. An agent then writes the spec and plan changes: §1–2, §4, §6.4–6.5, §9.1–9.5, `ARCHITECTURE.md`, `TESTING.md`, and new `DECISIONS.md` entries. The on-hold cards are retired or replaced.
- **Stop:** AC1, AC4, AC5 or AC6 cannot be met without heavy custom text handling. Return to the TUI and resume the on-hold cards, or reconsider a native app.

The spike does not use `internal/core`, so nothing from it needs unpicking in the main module either way.
