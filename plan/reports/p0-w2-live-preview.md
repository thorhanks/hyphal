# P0-W2 · Live preview spike report

Date: 2026-11-01. CI run: https://github.com/thorhanks/hyphal/actions/runs/41272941863. Code: `internal/pagespike/`.

## Outcome

Accepted 2026-11-01. Thor tried it by hand and it matches Obsidian. SPEC §5.7 and the plan now carry this, and the Go Markdown renderer and code highlighter are removed.

## Recommendation

**Go, once Thor has tried it by hand.** CodeMirror 6 delivers Obsidian-style live preview with about 360 lines of our own code. Selection across blocks, undo, arrow keys and typing all work as one document, and no stop condition was hit.

## What was built

A page renders everywhere except the lines the selection touches, which show their Markdown with dimmed syntax. It covers headings, bold, italic, strikethrough, inline code, `==highlight==`, links, images, bullet and numbered lists, task boxes, quotes, rules, tables and fenced code with highlighting. Tables and code blocks open as a whole; everything else opens per line.

## Results

17 tests, each repeated 20 times on Chromium, Firefox and WebKit.

| Environment | Passed | Failed | Skipped |
| --- | --- | --- | --- |
| CI Linux | 940 | 0 | 80 (Chromium-only tests on other browsers) |
| Arch, Chromium and Firefox (10 repeats) | 320 | 0 | 20 |
| CI macOS (run before Thor dropped macOS from this workflow) | 880 | 60 | 80 |

All 60 macOS failures are one test on all three browsers: it presses Mod+Y for redo, while CodeMirror's macOS redo key is Cmd+Shift+Z. That is a test mistake, not a product bug, though §6.4 must define the redo keys for pages.

Proven: clicking rendered text lands on the same word in the source; dragging across blocks doesn't shift text; deleting across blocks is exact and undoable; copy gives raw Markdown (Chromium only); a blank line splits and a typed fence swallows what follows without errors; raw HTML stays literal text and `javascript:` links are inert; emoji and combining marks are intact; the strict CSP from CONTRACTS holds.

## Performance

Keypress and movement to next frame, milliseconds, p50 / p95. These include waiting for the next frame, so about 17 is the floor. CI runners are noisy.

| Measure | Arch, Chromium | Arch, Firefox | CI Linux: Chromium, Firefox, WebKit |
| --- | --- | --- | --- |
| Typing, 2,000 lines (38 KB) | 17 / 19 | 17 / 18 | 17 / 22, 17 / 19, 18 / 32 |
| Typing, 20,000 lines (390 KB) | 17 / 66 | 24 / 57 | 32 / 95, 54 / 98, 64 / 85 |
| Next line, 20,000 lines | 17 / 26 | 17 / 77 | 17 / 35, 31 / 71, 20 / 67 |
| Jump, 20,000 lines | 18 / 25 | 24 / 36 | 36 / 66, 47 / 264, 62 / 78 |

A normal page is instant. A 20,000-line page stays usable, but typing has occasional slow frames, mostly from rescanning every top-level block after each edit. That can be made incremental if real pages ever get that long.

**Bundle:** 575 KB minified, 207 KB gzipped. About 210 KB of the minified size is the five code languages tested (JavaScript, Python, Go, CSS, HTML). Loading languages on demand would trim first load.

## Findings for a real build

- **Theming needs one small adapter.** CodeMirror's base styles sit outside cascade layers, so they beat our `@layer` rules. Font and column width silently failed until a small theme object set those properties from tokens. Everything else is plain CSS using tokens, and a test proves tokens restyle the page. Tokens always work; a theme overriding CodeMirror's own selectors may need `!important`.
- **Arrow keys need help around hidden lines.** Vertical motion works from screen coordinates, so it skipped collapsed tables and fences. A short key handler steps into them. Shift+arrows don't have this handling yet.
- **Reveal has to freeze during a mouse drag,** or text moves under the pointer. This is done.
- **Go's page renderer may no longer be needed.** With view mode gone, CodeMirror draws pages, and export writes the Markdown as stored. goldmark and chroma (P1-42) could be dropped, which also removes the two-parser concern. Check any other user first.
- **A small build step returns.** CodeMirror needs bundling. The plan would commit one built file and embed it, with `esbuild` run only when upgrading it. D-039 needs this exception.

## Not covered

- Saving to the server, sealing, and `hyphal page set` changing an open page.
- How page undo joins the outline's undo stack. Suggested: CodeMirror's history inside a page, and one step in the outline for a page editing session.
- Tables and fences nested in lists or quotes stay as source for tables, and aren't collapsed for fences.
- `[[links]]`, `#tags`, color tags, the title line and read-only tabs.
- Real IME use (only Chromium's simulated composition), screen readers, images beyond the basic case.

## Human check

From `internal/pagespike`:

```bash
npm ci && npm run build && npm run serve
```

Open http://127.0.0.1:47900/ and edit the sample page. Nothing is saved. Add `?long=20000` for a long page. The deciding question is whether it feels like Obsidian. Try clicking into formatted text, arrowing through the table and code block, dragging a selection across several blocks, and undo. The fonts here are placeholders.

## If this is accepted

- SPEC §5.7: live preview replaces view and edit modes; remove split view and position sync. Update §6.4, §9.1, §12.2 and the "Live preview is not planned" line.
- Plan: rewrite P1-42 and P1-43; remove the Phase 2 mode sync and split cards; add the D-039 exception and the page undo decision.
