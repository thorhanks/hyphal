# P0-W1 · Local web UI spike report

Date: 2026-10-31. CI run: https://github.com/thorhanks/hyphal/actions/runs/41231006906

## Recommendation

**Go, once the human checks at the end pass.** Browser editing met every automated editing, undo and text-integrity criterion on Chromium, Firefox and WebKit, on Linux and macOS, with no flakiness in CI. None of the stop conditions was hit, and no heavy custom text handling was needed: about 2,600 lines in total, including tests.

Three things this spike can't settle stay open: real browser-reserved keys, real IME use, and the `$EDITOR` round trip through a real terminal. Cold launch misses the §9.4 target, so that target has to change if this goes ahead.

## Results

| AC | Result | Notes |
| --- | --- | --- |
| AC1 Enter, Backspace, Delete, Tab, Move | Pass | Every row of §5.2, §5.3, §5.3a and the §5.4 Tab and Move rules, plus fast typing across Enter |
| AC2 Navigation, block selection | Pass | Stable column across wrapped lines and bullets needed custom Up/Down handling, about 40 lines |
| AC3 Keys | **Partial; human check needed** | See Keys below. Automation bypasses browser shortcuts, so it can't prove this |
| AC4 Undo | Pass | Ctrl+Z and native `historyUndo`/`historyRedo` both route to the server stack |
| AC5 Input methods | Pass, automated part | Emoji and combining marks on all three browsers. Composition events on Chromium only. **Real IME use not tested** |
| AC6 Scale | **Mostly pass; cold launch fails** | See Performance below |
| AC7 Theme | Pass | `colors.toml` recolors without reload, using a temp path. Real `omarchy-theme-set` and font switching not exercised |
| AC8 Security | Pass | Loopback only; bad token, `Host` or `Origin` refused; token leaves the URL; cookie is `HttpOnly`, `SameSite=Strict` |
| AC9 Tabs | Pass | Second tab is read-only with a banner and takes over when the first closes. Server side refuses its writes |
| AC10 Testing | Pass | Headless on Linux and macOS. 20 repeats per OS: 3,540 passed, 0 failed, 0 flaky, 120 skipped (Chromium-only tests) |

Skips: the 2 CDP composition tests and the clipboard test run on Chromium only, so those features are unverified on Firefox and WebKit.

Locally on Arch, 20 repeats on Chromium and Firefox gave 2,376 passes, no assertion failures and 4 Firefox attempts ending in "browser has been closed". That was not diagnosed. It did not occur in CI. Playwright's WebKit build can't run on Arch because it needs Ubuntu libraries, so local WebKit testing would need a container.

## Performance

100,000 nodes with 10,010 visible rows. Milliseconds, p50 / p95.

| Measure (target) | Arch, Chromium | Arch, Firefox | CI Linux: Chromium, Firefox, WebKit | CI macOS: Chromium, Firefox, WebKit |
| --- | --- | --- | --- | --- |
| Keypress to frame (< 16) | 3 / 15 | 2 / 3 | 4 / 18, 1 / 2, 8 / 15 | 14 / 65, 1 / 3, 10 / 19 |
| Enter round trip (not in §9.4) | 14 / 16 | 13 / 16 | 29 / 32, 14 / 27, 14 / 17 | 63 / 118, 27 / 38, 13 / 26 |
| Filter update (< 50) | 30 / 32 | 30 / 47 | 31 / 60, 39 / 54, 33 / 119 | 48 / 102, 38 / 125, 34 / 96 |
| Page first paint, browser running | 40 | 92 | 56, 85, 78 | 160, 105, 55 |
| Launch with a new browser (< 100) | 130 | 1,233 | 182, 1,339, 474 | 455, 2,945, 1,143 |

How to read these:

- **Typing is local.** It doesn't wait for the server, which is why it holds up. Enter does wait, and on a developer machine it stays within a frame.
- **Measurement limits.** Headless browsers, with "frame" measured as `requestAnimationFrame` plus a task, not real display output. CI runners are noisy and §9.4 only allows them for regressions. The macOS Chromium tail is the one to recheck on real hardware.
- **Fake core.** It has no SQLite and no history writes. Real commits run off the UI path per §9.4, but this hasn't been shown with the real core.
- **Filter is simplified.** It uses `strings.ToLower`, no diacritic folding and no page text, so the real number will be higher. Filter p95 misses 50 ms in places, mostly on CI.
- **Cold launch misses the target.** Headless shell is already a best case at 130 ms, and a real windowed browser will be slower. The < 100 ms target can only hold with the browser already running. It needs rewording to something like "first paint under 100 ms with a running browser" plus a measured cold figure.

## Keys

Key list from memory of Chromium and Firefox behavior. **Not verified in real windows.**

| §6.4 binding | Normal tab | `chromium --app` window |
| --- | --- | --- |
| Ctrl+W delete word | **Lost, closes the tab** | Expected to reach the app |
| Ctrl+Q quit | **Lost on Linux, quits the browser** | Check needed |
| Ctrl+P, Ctrl+K, Ctrl+F, Ctrl+S, F1, Ctrl+A, Ctrl+E, Ctrl+U | Expected to work; the page can cancel the browser's action | Expected to work |
| Alt+Left, Alt+Right word movement | Browser back and forward; check needed | Check needed |

Firefox has no app window mode, so it would stay on tab behavior.

Proposed replacements if these are confirmed: Ctrl+W keeps Ctrl+Backspace and Alt+Backspace as the working word-delete keys, and is removed from the tab-mode keymap. Quit becomes closing the window, which also suits the web model. A web UI can also drop the "keys that arrive in every terminal" limit in §6.4, and macOS users will expect Cmd, which this spike did not test.

## Findings for a real build

- **Stale replies can undo typing.** A state built before the server applied the latest typing overwrote newer text when it arrived. The test caught it during IME composition, and scrolling could trigger it too. Fixed by echoing a typing acknowledgement in each state; the real protocol needs this from day one.
- **Keys must queue during round trips.** Structural keys wait for the server's reply, and the next keys wait behind them. Fast typing across Enter stays in order without any rules in the browser.
- **`contenteditable="plaintext-only"` on only the focused row worked everywhere.** The `<textarea>` fallback wasn't needed. Concealed markers (§5.8) only apply away from the focused row, so this model should carry over, but that wasn't built.
- **Virtual scrolling is approximate.** Spacers use an average row height, so long jumps with wrapped rows may shift position. Enough for the spike, not for §4.6 stability.

## Report questions

**`$EDITOR` round trip (§6.3).** The server writes a temp file, runs a configured terminal command, waits for it to exit and applies the result; the lock is released meanwhile. Tested with fake editors, including a failing one that applies nothing. **Not tested in a real terminal.** The risk is that single-instance terminals return before the editor closes, which would apply the file too early. The real design may need a wait wrapper or a config template per terminal. Multi-line subtree parsing wasn't built.

**Clipboard (§5.5).** After a server round trip, `navigator.clipboard.writeText` copied subtree Markdown on Chromium with permission granted in the test. Loopback HTTP counts as a secure context. Firefox and WebKit weren't verified, and they restrict writes more tightly. Reading the clipboard can trigger prompts, which threatens "cut and paste is a move" (§5.5); a real `paste` event carries the text and may avoid that. Multi-line paste currently flattens to one line.

**Launch (proposed, not built).** `hyphal` opens the default database, starts a server or reuses a running one through a lock file, then opens the URL. It prefers `chromium --app` with its own window class under Hyprland and falls back to the default browser. The server exits shortly after the last tab closes. Whether `--class` sets the Hyprland window class under Wayland needs checking. The token in the launch URL is visible to the local user's process list; that needs a decision in the real design.

## Not covered

Pages, real storage, palettes, markup rendering, visual polish, Cmd bindings, and accessibility.

## Human checks

From `internal/webspike`, run `npm ci`, `npm run build`, then `go run .` and open the printed URL. It holds a small in-memory outline, reads your real theme file without writing to it, and saves nothing. `--gen 100000` loads the large outline.

1. **Feel.** Use it next to Workflowy, in both a tab and `chromium --app=<URL>`. This is the deciding check.
2. **Keys.** Open `/keyprobe.html` in the same session, in both window types, and note what's lost. A reserved key may close the window.
3. **Theme and font.** Run `omarchy theme set` and the font switcher while it's open.
4. **IME.** Only if you use one.
5. **`$EDITOR`.** Esc then `e`, with your real terminal. Check that the change applies only after Neovim closes.

## Decision needed

Go: an agent rewrites the spec and plan as listed in `plan/proposals/web-ui-spike.md`. Stop: resume the on-hold TUI cards.
