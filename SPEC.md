# Hyphal — Project Specification v4

Oct 31, 2026 · @Thor

## 1. Overview

We are building a local-first infinite outliner, inspired by Workflowy, for Linux and macOS. It is a single Go binary that serves a native web UI to your browser on the loopback interface. It is one continuous tree of bullets that you can capture into, indent, reorder, collapse and zoom. The outline is stored in a single SQLite file with full edit history and automatic snapshots, and can be exported to Markdown, JSON or OPML at any time.

**Goal:** a faithful Workflowy experience in a fast, beautiful, easily themed local web app. Obsidian-inspired knowledge features (links, backlinks, saved searches) are the bonus.

**Pitch:** An infinite outline that lives on your machine: one tree, unlimited depth, stored in one open, durable file you can always export.

**Status:** Concept / pre-implementation; the storage foundations are in progress. Version 4 replaces the terminal UI of earlier versions with a web UI, following the spike in `plan/reports/p0-w1-web-ui.md`. The app is named **Hyphal**; the binary, directories and window class use `hyphal`. The name appears only in user-facing places: the binary, data and config directory names, the window class, the page title, and the comment or header that marks an export. It never appears in the schema, operations, the sync log format or JSON field names, so renaming the app later only means moving directories.

## 2. Product principles

1. **One tree, no mandatory pages.** Any node can have children and can be zoomed into.
2. **Thought-first editing.** Capturing and rearranging bullets needs no mode switch. Commands that need single keys use a lightweight block-selection state, not Vim modes. Block selection is a mode in the strict sense; modes are kept few and shallow: Esc always backs out one level, and Enter always returns to typing.
3. **Your data is never locked in.** One SQLite file with a plain, documented schema, readable by standard tools. Export to Markdown, JSON or OPML at any time; a JSON export restores the whole outline, and its history too when exported with it.
4. **Nothing is lost silently.** Every change is recorded in history, deletes go to the Trash, and snapshots protect against a damaged file.
5. **Every change is attributable and reversible.** History records whether a change came from the UI, the CLI or an agent, and any change set can be reverted.
6. **Local-first and private.** No account, cloud dependency or remote connection. The only network use is one server bound to the loopback interface, and the only client is your own browser. The page loads nothing from anywhere else.
7. **Native web, no framework.** The UI is native web components, HTML, CSS and JavaScript using modern browser features only. No external frontend library enters the UI without Thor's explicit approval, and there is no required build step.
8. **Easily themed.** Every visual value is a CSS custom property, and a theme is one small CSS file (§4.4). Keep every action available from the keyboard.
9. **Composable.** Users, scripts and agents work through a robust CLI with JSON output, never by touching the database directly.
10. **Simple core first.** The outlining experience comes before knowledge-management features.

## 3. Users, scenarios and comparison set

**Primary users** are Workflowy-style outliners who want local ownership, keyboard-first editing and a look they control. The data lives in SQLite; scheduled Markdown export (§7.5) is the bridge to plaintext and Git. Later, users who want scripts or coding agents to read and update their outline.

**Core scenarios:**

1. **Session.** Launch from the app launcher or a shell and type immediately. Enter adds bullets, Tab nests them. Collapse a project, zoom into a task, filter for a word, close the window.
2. **Quick capture.** From any shell (`hyphal add "Call the dentist"`) or a Hyprland keybind (`hyphal capture`), text lands in the Inbox node without opening the full outline.
3. **Write at length.** Press a key to open a bullet's page and write Markdown right in the app. Everything is edited in Hyphal; no external editor is involved.
4. **Recover.** A week later, restore a deleted branch from the Trash, or revert one bad change made by a script without touching anything else.
5. **Leave.** `hyphal export --format markdown` produces a readable outline that works anywhere, at any time.

**Comparison set** (evaluate hands-on before Phase 1 ends): Workflowy, for the editing feel we are matching (zoom, Esc selection, Enter and Backspace rules), and Obsidian, for live preview and the look of Markdown pages.

Our claimed difference: Workflowy-grade editing with local data in an open format, complete and reversible history, and a UI you can restyle with one CSS file.

### Workflowy parity checklist

"Clone" means every *Must* row works the way Workflowy users expect. Phase numbers refer to §11.

| Workflowy feature | Verdict | Phase | Spec |
| --- | --- | --- | --- |
| One tree, zoom, breadcrumbs | Must | 1 | §4.1 |
| Enter / Tab / Backspace behavior | Must | 1 | §5.2–5.4 |
| Esc block selection, multi-select, bulk move | Must | 1 | §5.1 |
| Collapse, expand all, expand to level N | Must | 1 | §6.6 |
| Complete, show / hide completed | Must | 1 | §5.4 |
| Pages on bullets (replace Workflowy notes) | Must | 1 | §5.7 |
| Inline formatting and text color | Must | 1 | §5.8 |
| Duplicate node, Move to… | Must | 1 | §5.4, §6.6 |
| Import from Workflowy (OPML) | Must | 1 | §7.5 |
| Undo, Trash | Must | 1 | §5.6, §8.3 |
| `#tags`, click to filter | Must | 2 | §5.9 |
| Links to other bullets (survive moves and renames) | Must | 2 | §10.1 |
| Search operators (`is:complete`, `has:page`, `changed:7d`) | Must | 2 | §6.2 |
| Starred locations | Must | 2 | §6.6 |
| Drag to move | Must | 2 | §6.5 |
| Templates | Later | 3 | §10.1 |
| Dates and a Today node | Never | — | §12.1 |
| Mirrors (one bullet in two places) | Never | — | §12.1 |
| `@person` mentions | Never | — | §12.1 |
| Board (kanban) view | Never in V1 | — | §12.1 |
| Sharing and collaboration | Never | — | §12.1 |

## 4. Information model and UI

**The look was agreed on 2026-10-31** (§12.2 row 57). `plan/reports/design-mockup.html` is the visual reference. Changing the look later should touch only theme tokens and component CSS, not behavior.

### 4.1 Global tree

- Exactly one root per database ("workspace" and "outline" both mean one database file). The root is a real row with `parent_id` NULL, its ID stored in `meta`, and is shown as **Home**. It can't be renamed, deleted, moved, completed or starred, and has no page.
- Every other bullet is a node with editable text, ordered children, exactly one parent, and a permanent random ID (§7.2).
- No distinction between bullet, task or project. Any bullet can also carry a Markdown page (§5.7). Completion is an attribute any node can have.
- **Zoom** makes any node the temporary root. The zoomed node is shown as a heading, with only its descendants below it. The heading can be focused and edited like a bullet.
- **Zoom is in the URL.** Each zoom has its own address (`/#/<node-id>`, Home is `/`), so browser Back and Forward move through zooms and a zoom can be bookmarked. The address holds only the node ID. Each database keeps a stable port (§9.3), so these addresses last. A bookmark needs the server running; `hyphal open <node>` starts it and goes there.
- **Breadcrumbs** show the path to the zoom target. They always start at Home, use `›` as the separator, and each crumb is a link. Long paths are truncated in the middle: `Home › … › Development › Tooltips`.
- Collapse/expand is independent of zoom and persists across sessions (§7.2).
- No artificial depth limit.

### 4.2 Screen structure (zoomed into Projects)

Structure only; the look comes from the theme.

```text
Home › Projects                     [icons]    outline  ●

Projects

● ControllerCraft
  ● Development
    · Inventory navigation        (completed: dimmed, struck)
    · Improve controller mappings
    · Fix tooltip positioning
  ✶ Design notes   14             (collapsed: Lit strands, hidden count)

✶ Lighthouse   6

Enter new · Tab indent · Esc select · Ctrl+P commands · F1 help
```

### 4.3 Visual language

| State | Default glyph | Rule |
| --- | --- | --- |
| Collapsed, has children | Lit strands: a spore with strands reaching right, each ending in an accent-colored tip | Clicking the glyph expands. Strands show the hidden bullets (all descendants): 2 up to 4, 3 up to 9, 4 up to 24, 5 from 25 |
| Expanded, has children | Large spore (a slightly irregular dot) | Clicking the glyph collapses |
| Leaf | Small spore (a slightly irregular dot) | |
| Task, open / done | A drawn checkbox after the bullet glyph | Native-feeling checkbox behavior; clicking toggles completion |
| Heading style | Bold text, extra space above | |
| Completed | Dimmed + strikethrough text; glyph unchanged | |
| Selected (block selection) | `selection` token background on the whole row | |

- Glyphs and icons are inline SVG in the light DOM, with classes on their parts, and each row exposes its state and strand count as attributes. A theme can recolor them or hide them and draw its own. They are never read as text by the editor.
- A collapsed bullet also shows the hidden count as a dim number after its text.
- Completion never replaces the bullet glyph, so a completed parent still shows whether it is collapsed.
- No information is carried by color alone.
- **Threads.** Ruled lines with a small rounded elbow run from each parent's bullet to its children, in the `--thread` color. The path from the zoom heading to the focused bullet is drawn in the accent color, so position is visible at a glance. Threads carry no information that isn't also in the indent and breadcrumbs. They draw in briefly when a bullet expands (motion token, off under `prefers-reduced-motion`).
- **Glowing thread.** The `thread_glow` setting (§13, off by default) adds a soft glow to the lit path and a brief pulse along it when focus moves. The pulse is off under `prefers-reduced-motion`.
- Each level indents by the `--indent` token. Past 8 levels of the current zoom, each further level indents by half as much, and the footer suggests zooming.
- **Wrapped lines** continue under the start of the text, not under the glyph, so each bullet reads as one block.
- **Focus:** in text editing, the focused bullet shows the caret, its glyph and the lit thread path in the accent color; the focused row may also show its row icons (§4.6). Concealed markers follow this focus. In block selection no caret is shown.

### 4.4 Theming

**Must have:** restyling the app takes one CSS file and no code changes. On Omarchy, the app matches the desktop theme and follows every theme change while running, with no reload and no configuration.

- **Tokens are the contract.** All colors, fonts, sizes, spacing, radii, glyphs and motion are CSS custom properties on `:root`, listed in `internal/web/assets/tokens.css` and documented in `docs/theming.md`. Component CSS uses tokens only, never raw values; a test fails on a raw color or font outside `tokens.css` and the bundled themes.
- **Cascade layers set the order:** `@layer reset, tokens, components, theme, user`. A theme can't be beaten by our components' specificity, and a user stylesheet wins over them all. CodeMirror's own base styles sit outside the layers, so a small adapter sets the properties they fix from tokens; a theme changes pages through tokens or `.lp-*` classes.
- **Light DOM.** Web components render into the light DOM, so any selector in a theme can reach any part of the UI. Shadow DOM is not used: it would hide parts from themes and breaks text selection across browsers.
- **Themes are files.** A theme is one `.css` file that overrides tokens in `@layer theme`, and may restyle components too. The app ships `light` and `dark` plus `auto` (follows `prefers-color-scheme`). Users add themes by dropping a file in `~/.config/hyphal/themes/`. The app reloads a theme when its file changes, without a page reload.
- **`omarchy` theme.** When `~/.local/state/omarchy/current/theme/colors.toml` exists and `theme` is `auto`, the app maps that file's roles onto tokens (`background`, `foreground`, `accent`, `muted`, `selection`, the named colors and `mode`) and follows changes while running. Only hex colors and `mode` are accepted, so a theme file can't inject CSS. The file's path and format belong to Omarchy and may change, so a missing or unreadable file falls back to `auto` without an error.
- **Fonts.** The default font tokens are Literata for text and chrome, Fraunces for zoom and page headings, and `ui-monospace, monospace` for code. Literata and Fraunces are bundled with the app under the SIL Open Font License, so they load offline and are never fetched. Themes, including `omarchy`, change colors and keep these fonts unless they override the font tokens. `monospace` resolves through fontconfig on Linux, so Omarchy's font choice still applies to code on the next load.
- **User overrides.** `~/.config/hyphal/user.css`, if present, loads in `@layer user` after any theme.
- **Theme CSS is trusted as your own config.** It comes only from your config directory, never from the outline. The Content Security Policy (§9.3) still blocks remote loads, so a theme can't reach the network.
- **Text colors** in bullets (§5.8) use eight named tokens (`--color-red`, …), so colored text follows the theme.

### 4.5 Chrome

- **Save indicator:** nothing when saved; `●` while typing is not yet committed (until it seals, §7.3); a red label if a write fails or the local server can't be reached.
- **Screen layout,** top to bottom, all inside the reading column:
  1. **Header row:** breadcrumb left; icon strip (§4.6), database name (the file name without `.db`) and save indicator right.
  2. **Banner row,** only while a banner is active (below).
  3. The outline (or page, or full-screen view), which scrolls.
  4. **Footer row,** fixed to the bottom of the window. It shows one of three things, in priority order: a message, the filter query while a filter is active (`/ tooltip · 12 matches`), or contextual key hints.
- **Window title and icon:** the title is the zoomed bullet's text, or the database name at Home, so windows and tabs are easy to tell apart. The icon is a spore and thread, in the bundled assets.
- **Footer hints:** contextual keys for the current state. `user.css` can hide them; messages and the filter still use the row. F1 (or `?` in block selection) opens the full help overlay (§4.8).
- **Messages** are the app's only transient feedback; nothing floats and nothing times out. A message replaces the footer's content and stays until the next keypress or click, then the footer returns to what it showed before. Messages confirm undoable actions (`Moved "Design notes" and 214 descendants to Trash · Undo`, where Undo is clickable and Ctrl+Z works too), say where a restore landed, report a clipboard failure, and explain a no-op key ("hints" elsewhere in this spec are messages). A newer message replaces an older one. The footer is an ARIA live region.
- **Banners:** persistent, for failed writes, a lost connection to the local server, a database opened read-only, a restore that just ran, or an invalid config. A banner stays until its cause is gone; an informational one (a restore that just ran) clears on the next change to the outline.
- **Views on demand:** Trash (§4.8) and History open as temporary full-page views, not panels. No sidebar, no daily journal.
- **First run:** the first time the UI opens a database (a flag in `meta`), it adds a short sample outline that teaches the core keys, unless the outline already holds anything besides an Inbox (for example after an import). Databases created by the CLI start with only Home, so scripts and agents get a clean outline.

### 4.6 Visual design

The direction is **Mycelium**: minimal and professional, with a unique identity drawn from the fungal network. The outline reads like a calm page in a field notebook, not a dashboard. The bundled `dark` theme is the primary look, mid-dark rather than near-black, tuned for long reading:

| Token | `dark` value |
| --- | --- |
| `--bg` | `#2c2824` (peat) |
| `--raise` | `#37322c` |
| `--text` | `#eee6d8` |
| `--muted` | `#b3a896` |
| `--thread` | `#75695a` |
| `--accent` | `#e8b25c` (chanterelle) |

Body text is about 16.5 px at line height 1.5. The bundled `light` theme uses the same shapes and fonts, with colors chosen in P1-29.

The identity lives in shapes, so it survives any theme: threads, spore bullets and icons use only theme colors.

- **Icons.** A custom hand-drawn set in `internal/web/assets`: each icon is a recognizable shape drawn as one thread that ends in a small accent-colored tip. The set covers home, search, star, trash, history, page and commands, and grows with new features. No icon font or icon library.

- **Centered reading column.** Text width is capped by the `--column` token (default 80 characters) and centered in wide windows. Breadcrumbs and status sit inside the same column.
- **Inline formatting rendered, markers concealed.** `**bold**` shows as bold; the markers appear only on the bullet being edited, as in Obsidian's live preview (§5.8).
- **Typographic hierarchy:**
  - Optional heading bullets: bold, with extra space above, for section-like nodes.
  - A bullet with a page shows a dim page icon after its text. There is no page preview in the outline; zoom into the page to read it.
  - Muted breadcrumbs and footer; full-strength color only for text you wrote.
  - Tags as colored text ("chips"), not filled backgrounds by default (§5.9).
- **Stability and motion:**
  - The caret never sits within `--scroll-margin` (default 3 rows) of the viewport's edge.
  - The caret's row stays fixed on screen when collapsing or expanding.
  - Scrolling keeps its position through virtualization (§9.3); content doesn't jump.
  - **Zooming animates:** the bullet grows into the heading through the browser's View Transitions API. An engine without it zooms instantly, and the transition never delays typing.
  - Motion comes only from tokens, is brief, and is off under `prefers-reduced-motion`.
- **Density modes:** *comfortable* (the default: a blank line between the top-level siblings of the current zoom) and *compact*, as `data-density` on the root element for CSS to act on. A heading-style bullet always gets space above it, except as the first row under the zoom heading, and this space is not added to the density space.
- **Concealed markers** appear only on the focused bullet. When they reflow its line, the bullet's first row stays fixed on screen.
- **Narrow windows:** below the column cap, the reading column fills the width with a 16 px gutter. There is no "too small" state; phones aren't a target but nothing breaks.
- **Semantic tokens.** UI CSS uses roles, never raw colors:

  | Token | Used for |
  | --- | --- |
  | `--text` | Bullet and page text |
  | `--bg` | Page background |
  | `--muted` | Breadcrumbs, footer, page marker, completed text |
  | `--accent` | Focused glyph, overlay borders, focus rings, links on pages |
  | `--selection`, `--selection-text` | Block selection, palette highlight |
  | `--match` | Filter and search matches |
  | `--highlight` | `==highlight==` text |
  | `--error`, `--warning` | Failed-write label and banners; read-only and restore banners |
  | `--thread` | Threads between bullets |
  | `--raise` | Focused row background, hover states |
  | `--color-*` | The eight text colors (§5.8) and tag chips |

  Size, spacing, font, radius, glyph and motion tokens are listed in `tokens.css`.
- **Contrast.** Both bundled themes meet WCAG AA for text and muted text. A test checks this for `light` and `dark`.
- **Overlays** are native `<dialog>` elements (palettes, help, backlinks), floating and centered, with a title and the accent border.
- **Restraint.** No splash screen, formatting toolbar or busy status bar. The chrome is breadcrumbs, the save dot, one footer row, a quiet icon strip in the header (search, stars, history, Trash, commands) and icons on the focused row (page, star, Trash). Icons appear once their feature exists. Every icon action also has a key, and the mouse is never required. Instant response is part of the look (§9.4).
- **Reference screens** are part of the spec before Phase 1 ends: a laptop window, a full-screen ultrawide, a deep zoom, an active filter, and a light theme. Phase 1's exit gate includes a side-by-side review against Workflowy.

### 4.7 At home on the desktop

Omarchy is *omakase*: one beautiful, opinionated default, keyboard first, themed everywhere, no bloat, with plain config files for anyone who disagrees. The app follows the same rules.

**Omakase defaults**

- One carefully tuned default look and keymap. Settings exist for real needs (density, keymap), not for every pixel; everything else is a theme or a line in `user.css`.
- No settings screen. "Edit settings" in the command palette opens the commented `config.toml` in the built-in plain-text editor and reloads it on save.

**Shared keyboard conventions**

The full keymap is in §6.4. The conventions it follows:

- `?` in block selection, or F1 anywhere, shows every command with its key.
- Esc backs out one level and never zooms out: overlay → where you were; page → outline; text editing → block selection → clear filter → nothing (a hint). Zooming out is always explicit.
- There are no side panes. Backlinks is a floating overlay like the palettes; Esc closes it.
- Closing the window or tab quits. Typing already reached the server, so there is never anything to save (§7.3).

**Launching**

- `hyphal` starts the server if needed and opens the UI (§9.3). It prefers an app window with no browser chrome (`--app=` on a Chromium-family browser, with its own window class under Hyprland), and otherwise opens a tab in the default browser. The `browser` setting overrides this.
- On Linux, ships a `.desktop` entry and its own window class, so it launches from the app launcher and can be targeted by Hyprland window rules. On macOS it is launched from a terminal; there is no app bundle in V1.
- **Capture window** (Phase 2): `hyphal capture` opens a small app window on the `/capture` route with its own class, which a window rule makes small and floating. Type one line and press Enter: it lands in the Inbox, and the window closes. `hyphal add` covers capture from a shell. The docs suggest a Super binding, checked against Omarchy's defaults.

### 4.8 Phase 1 screens

Phase 1 needs these screens besides the outline and the page. Later-phase screens are listed in §12.3.

- **Trash view** (full page, §8.3): one row per deletion, newest first: the deleted bullet's text, its former breadcrumb, the descendant count and the age. Enter previews the subtree read-only; `r` restores; `d` purges that deletion after a confirmation (purging is final, so purge and Empty Trash are the only actions in the app that ask for confirmation); Esc returns to the outline.
- **Help overlay** (F1, `?`): every command in the current context with its key, generated from the keymap (§6.4), filterable by typing.
- **Color picker** (`c` in block selection, or the command palette): an overlay listing the eight colors as swatches with their names, plus "No color". `1`–`8` (and `0` for No color) or arrows and Enter choose; Esc cancels.
- **Read-only mode** (newer schema or failed integrity check): a persistent banner says why; everything works except changes, and any editing key shows a hint instead.

### 4.9 Accessibility

The outline is a WAI-ARIA `tree` with `treeitem` rows carrying level, expanded and selected state, plus `aria-setsize` and `aria-posinset`, because only a window of rows is in the DOM. The focused bullet is a real editable element, overlays are `<dialog>`, and every control is keyboard reachable. Tab is indent inside the outline, as in Workflowy; F6 moves focus out to the page's other regions. Native browser zoom works at any level. This is a floor, not a separate feature, and each screen's tests check it.

## 5. Editing behavior

### 5.1 Two states, no modes

- **Text editing (default).** Printable keys type. Commands use modifiers.
- **Block selection.** Esc selects the current bullet as a whole. Up/Down move the selection; Shift+Up/Down extend it across siblings only. Single keys act on the selection (§6.4). Enter returns to text editing with the cursor at the end of the bullet. Other printable keys do nothing except show a hint, so text typed after Esc never runs commands by surprise.

### 5.2 Enter

| Cursor position | Result |
| --- | --- |
| Start of a non-empty bullet | Insert an empty sibling above; cursor stays on the current text |
| Middle of text | Split: text after the cursor becomes a new sibling below; children stay with the first half |
| End of an expanded bullet with children | Create a new first child |
| End of a collapsed bullet, or a leaf | Create the next sibling |
| Empty bullet that is not at the top level of the current zoom | Outdent it (Workflowy behavior) |
| Empty bullet at the top level of the current zoom | Create the next sibling |
| Anywhere in the zoom heading | Create a new first child of the zoom target; the heading is never split |
| With a text selection | Delete the selected text, then apply the rule for the resulting cursor position |

On a split, the first half keeps the node's identity and everything attached to it: ID, children, page, star, completion, inbound links and history. The second half is a new node.

### 5.3 Backspace at the start of a bullet

Rows are checked top to bottom; the first match applies.

| Situation | Result |
| --- | --- |
| With a text selection | Delete the selected text only |
| No previous visible bullet, or the cursor is at the start of the zoom heading | No-op |
| Bullet is empty, no children | Delete it; cursor to the end of the previous visible bullet (or the zoom heading) |
| Bullet is empty, has children | No-op; a message hints to use Esc then Delete |
| The previous visible bullet is the zoom heading | No-op. Nothing is ever merged into the zoom heading |
| Both bullets have a page | No-op with a hint. Merging would have to combine two pages |
| Bullet has text, no children | Merge text into the end of the previous visible bullet |
| Bullet has text and children, previous bullet has no children | Merge text; children are adopted by the previous bullet |
| Bullet has text and children, previous bullet has children | No-op with a hint. Merging would interleave two child lists unpredictably |

"Previous visible bullet" means the row above on screen, which may be deeper than the current bullet (the last visible descendant of the previous sibling). Under an active filter, hidden bullets are skipped.

On a merge, the previous bullet survives with its ID, completion state and history. The merged-away bullet goes to the Trash in the same change set; its page moves to the survivor (if the survivor has none), and its star and inbound links are retargeted to the survivor.

### 5.3a Delete at the end of a bullet

Delete pulls the next visible bullet into the current one, mirroring Backspace: the current bullet survives and the next one is merged away.

Rows are checked top to bottom; the first match applies.

| Situation | Result |
| --- | --- |
| With a text selection | Delete the selected text only |
| No next visible bullet | No-op |
| Next visible bullet is the current bullet's own first child | No-op with a hint |
| Next visible bullet is empty, no children | Delete it |
| Both bullets have a page | No-op with a hint |
| Next visible bullet has text, no children | Merge its text onto the end of the current bullet |
| Next has children, current has no children | Merge its text; the current bullet adopts its children |
| Next has children, current has children | No-op with a hint |

Identity follows the merge rule above: the current bullet keeps its ID, and the merged-away bullet goes to the Trash.

### 5.4 Structure

- **Tab / Shift+Tab** indent or outdent the current bullet (or selection) with its descendants. Indent is a no-op on the first sibling.
  - Indenting under a collapsed sibling expands it, so the bullet stays in view.
  - Outdenting makes the bullet the next sibling of its old parent. Later siblings stay where they are; they are never adopted (Workflowy behavior).
  - Outdenting a top-level bullet of the current zoom is a no-op with a hint.
- **Move up/down** among siblings moves whole subtrees. Moving past the first or last sibling crosses into the adjacent parent's child list, as Workflowy does. For example, with siblings A and B under one parent:
  - Moving B's first child up makes it the last child of A (A expands if collapsed). If B is the first sibling (there is no A), the child moves up one level and becomes B's previous sibling.
  - Moving A's last child down makes it the first child of B, mirroring the rule above.
  - Nothing moves outside the current zoom; at its edges, moving is a no-op.
- **Delete a subtree** (Delete or `d` in block selection) never asks for confirmation. It shows an undo message with the descendant count.
- **Complete / reopen** toggles a node's completion (storage rule: §7.2). Completed nodes stay in place; a toggle hides them. On a selection with mixed state, any open bullet means "complete all", otherwise "reopen all". When a newly completed bullet hides, the cursor moves to the next visible bullet and a message offers undo.
- **Editing under a filter:** a bullet you create or edit stays visible until the filter changes, even if it doesn't match. Indent and move use the real tree, including hidden bullets; the moved bullet stays visible with its ancestors.
- **Tasks:** toggle task makes a bullet a task, shown with a checkbox (§4.3). Completing a task checks its box.
- **Leading markers convert.** Typing `[ ] ` at the start of a bullet makes it a task, `[x] ` a completed task, and `## ` a heading-style bullet; the marker is removed. Text set by paste, import or `hyphal edit --text` follows the same rule, so stored bullet text never starts with a marker and the Markdown dialect needs no escape for them.
- **Duplicate** copies the subtree as the next sibling, with new IDs, pages, completion, task and heading state. Stars are not copied, and links inside the copy still point to their original targets.
- **Heading style:** toggle heading makes a bullet a section-like heading (§4.6). It changes only the look, never the structure.
- **Move to…** opens a fuzzy parent picker over all node text, built on the command palette (Phase 1). From Phase 2 it uses the jump palette, with starred locations first. This is how subtrees cross long distances.

### 5.5 Paste and clipboard

- Paste uses the browser's `paste` event, so any paste key or menu works and no clipboard permission is needed. Single-line paste inserts text, replacing any text selection. Pasting a URL over selected text makes it a link, in bullets and on pages.
- Multi-line paste is parsed with the shared Markdown dialect (§7.5) into a subtree. Plain lines become bullets; indentation becomes nesting; `^id` suffixes are ignored, so pasted nodes always get new IDs.
  - Cursor at the end of a bullet: the subtree is inserted as siblings after it.
  - Cursor mid-text: the bullet is split first (§5.2), and the subtree goes between the two halves.
  - Empty bullet: the subtree replaces it.
  - In block selection: inserted as siblings after the selection.
- Copy (block selection) writes the selected bullets and their descendants to the system clipboard as indented Markdown in the shared dialect: text with inline markup, task, completion and heading markers. Pages and IDs are not copied. The copy also carries HTML with nested lists, so it pastes properly into email and documents; pasting into Hyphal uses the Markdown. Cut copies, then deletes to the Trash.
- **Cut and paste is a move.** The app remembers its last cut. If pasted text is exactly the text that cut wrote, paste restores the cut bullets at the paste position instead of creating new ones: same IDs, pages, stars and history, as one change set (the cut and the move both stay undoable). Text pasted from anywhere else, or after the clipboard changed, creates new bullets as above. To copy a subtree with its pages, use Duplicate (§5.4). The `paste` event carries the text, so the app never reads the clipboard on its own.
- **Clipboard transport:** the Async Clipboard API (`navigator.clipboard.write`). The Markdown comes from the server, so the write happens in the same user gesture through a `ClipboardItem` holding a promise where the browser supports it, and otherwise right after the reply. A failure shows a message. Phase 0 verifies this on all three browser engines (the spike verified Chromium only).

### 5.6 Undo and redo

- **Undo is per session.** The server keeps one undo stack in memory, shared by all windows. Each step is one change set you started from the UI; all the saves from one visit to a page count as one step. Undo applies the step's inverse as a new change set whose `undo_of` points at it; redo applies it again. Any new change from the UI clears the redo steps. The server exiting clears the stack; older changes are recovered from the Trash, with `hyphal revert`, or from the History view (Phase 2).
- Changes from the CLI or an agent are never on the stack, so Ctrl+Z never undoes something you didn't do; they are reverted with `hyphal revert`.
- If a later change from another source touched the same fields, undo follows the revert rule: those fields are skipped and reported (§8.2).
- Typing coalesces into one step per node; the step closes when the typing operation seals (§7.3). Ctrl+Z first seals any open typing, so the typing just done is the step undone.
- Each step remembers the cursor and selection from before the change, and undo restores them. Undoing a change outside the current zoom zooms to show it, with a message.

### 5.7 Pages

- **Every bullet stays one line.** Long-form writing lives on an optional **page** attached to the bullet: a full Markdown document with headings, lists, code blocks, quotes and tables, as long as you like.
- **Page zoom:** `p` in block selection, or the page icon, opens the bullet's page, on a bullet without one an empty page. A page has its own address (`/#/<id>/page`), so Back, Forward and bookmarks work as they do for zooms. A page left empty is not created; clearing an existing page's text deletes the page, as one undoable change. Esc saves and returns to the outline in block selection on the page's bullet. Normal zoom (`z`) still shows only the bullet's children.
- **Live preview, one mode.** A page is always rendered and always editable, as in Obsidian. There is no separate view or edit mode.
  - **Rendered everywhere else.** Headings, emphasis, strikethrough, `==highlight==`, inline code, links, lists, task boxes, quotes, rules, tables and syntax-highlighted code blocks show as formatted text, with their markers hidden.
  - **Source where the cursor is.** Every line touched by the cursor or selection shows its Markdown, with markers dimmed, and is freely editable. A table or fenced code block opens whole when the cursor enters it.
  - **One document.** Arrow keys, clicking, selection, copy, delete and undo all work across blocks. Clicking rendered text lands on the same word in the source. Copy gives raw Markdown. Nothing reveals or hides during a mouse drag, so text never moves under the pointer.
  - **Task boxes** toggle on click, as an undoable edit, without moving the cursor.
  - **Links:** Ctrl+click or Ctrl+Enter follows the link; a plain click places the cursor. `[[links]]` zoom to their target (Phase 2).
  - **Images** are not rendered in Phase 1: `![alt](url)` shows its alt text. Pasted and dropped images arrive with attachments in Phase 3 (§10.1). Pages never reach the network (§2.6).
  - **Raw HTML stays literal text** and never runs. Only the link schemes in §9.3 are followable.
  - **Find:** Ctrl+F opens a find bar for this page, because the browser's find can't see text the editor hasn't drawn. Esc closes it first, then leaves the page.
  - Editor: CodeMirror 6, the one approved frontend library (§9.1). The outline does not use it, and it loads the first time a page opens, so it never slows launch.
- **Title:** the bullet's text is the page title, shown in a single-line field above the page and edited like a bullet (concealed markers, leading-marker conversion). Editing it renames the bullet. It is never part of `pages.markdown`. Enter or Down moves into the page, and Up from the page's first line moves back. A new page starts with the cursor in the page, not the title.
  - **Delete page** (command palette) removes the page from a bullet, as one undoable change.
- **Saving.** The browser sends the page's text within about 300 ms of a pause, and at once when the page loses focus or closes, so a closed tab or browser crash loses only keystrokes still in flight (§7.3). The server seals it under §7.3's rules: after 2 s idle, 5 s open, and on leaving the page.
- **Undo:** inside a page, Ctrl+Z and redo work through the page's own history, one edit at a time. Back in the outline, one undo step reverses the whole visit to the page (§5.6).
- **Indicator:** a dim page marker follows the bullet's text (§4.6). The outline never shows page content; zoom into the page to read it.
- **Layout:** the breadcrumb ends with the page icon after `Home › Projects › ControllerCraft`. Same centered reading column and theme as the outline.
- **A page belongs to its bullet:** it moves with it, goes to the Trash and comes back with it, and is copied by Duplicate.
- **Links and tags work on pages:** `[[` links to bullets, `#tags` count for the page's bullet, and pages appear in backlinks (Phase 3).
- **Rule of thumb for users:** structure goes in bullets, prose goes in pages. Lists inside a page are text, not tree nodes, so they can't be zoomed or moved in the outline.
- **Export:** each page becomes its own `.md` file linked from the outline export (§7.5). OPML export stores the page in the `_note` attribute, which Workflowy reads as a note.

### 5.8 Inline formatting and color

Formatting uses Markdown-style markers, typed directly or wrapped around selected text. Markers are concealed except on the bullet being edited.

| Style | Syntax | With text selected, type |
| --- | --- | --- |
| Bold | `**text**` | `*` twice |
| Italic | `*text*` | `*` |
| Strikethrough | `~~text~~` | `~` twice |
| Inline code | `` `text` `` | `` ` `` |
| Link (URL) | `[label](https://…)` | — (rendered as the label, a real link; `o` in block selection or a click opens it in a new tab) |
| Highlight | `==text==` | `=` twice |
| Text color | `<red>text</red>` | — (Color… in the command palette) |

- **Colors:** `red`, `orange`, `yellow`, `green`, `cyan`, `blue`, `purple`, `gray`. Each maps to a theme token, so colored text follows theme changes.
- `<red>` and `==highlight==` were chosen because they are short and readable when concealment is off. `<red>` is not HTML, so Markdown export converts it (below). `==highlight==` renders in Obsidian but not in plain CommonMark tools. Unknown tags are left as plain text.
- **Wrapping:** with text selected (Shift+arrows in text editing), typing `*`, `~`, `=` or `` ` `` wraps the selection in that character instead of replacing it, and keeps it selected, so a second press nests the wrap (`*` twice makes bold). The command palette's Bold, Italic and Color… commands apply to the text selection, or to whole bullets in block selection.
- Applying a color to whole bullets (`c` in block selection) wraps each selected bullet's text in one color tag, replacing any color tag that already wraps it.
- **Grammar:**
  - Opening markers can't be followed by a space and closing markers can't follow one, as in CommonMark, so `2 * 3 * 4` stays literal.
  - Styles nest (`**bold *and italic***`) but never overlap; an unmatched marker is literal text.
  - Inside an inline code span everything is literal.
  - A backslash escapes a marker: `\*`.
  - Color tags nest with other styles but not with each other.
  - Only the markers in the table are recognized while typing, so `_` and `__` stay literal (safe for `snake_case`). Importers convert `_italic_` and `__bold__`.
- **Color mapping:** each name maps to its `--color-<name>` token (§4.4). In the Omarchy theme, orange uses `orange`, purple uses `magenta` and gray uses `muted`.
- Markdown export keeps the markers; colors export as `<span style="color: …">` so Obsidian renders them.

### 5.9 Tags (Phase 2)

- `#tag` anywhere in a bullet or page. Letters, digits, `-`, `_` and `/` are allowed; `/` makes nested tags (`#project/lighthouse`), as in Obsidian.
- Tags render as colored text (chips, §4.6). Each tag gets a stable color from the `--color-*` tokens, and exposes `data-tag`, so `user.css` can override it. Tags inside code blocks and inline code on pages don't count; Go finds them with a small line scanner, since it no longer parses Markdown.
- `o` in block selection, or a click, sets the in-place filter to the bullet's tag. Enter never follows a tag; it always edits. Typing `#` offers completion from existing tags.
- A tag list in the command palette shows every tag with its count.

## 6. Navigation, search, integration and input

### 6.1 Navigation

- Collapse/expand one node, or all its descendants (keys in §6.4).
- Zoom in on the current node; zoom out one level; zoom to root.
- The cursor position is remembered per zoom level. Zooming out returns exactly where you were.
- Up/Down move through wrapped visual lines, not just logical bullets. Home/End are per visual line, then per bullet on a second press.

### 6.2 Search

- **In-place filter (Phase 1).** Ctrl+F (or `/` in block selection) filters the tree as you type: matching nodes stay visible with their ancestors, matches highlighted, everything else hidden. This includes collapsed branches and page text. Esc clears it and keeps the cursor on the selected match.
- **Jump palette (Phase 2).** Ctrl+K opens a fuzzy finder over all node text, showing each result's breadcrumb, with starred locations listed first. Enter zooms to the result's parent with the result selected. From Phase 2 the same palette backs "Move to…" (§5.4).
- **Search operators (Phase 2),** Workflowy-style, in both the filter and the palette:
  - `is:complete`, `is:open` (not completed, any bullet), `is:task` (open or done), `has:page`, `is:starred`; `is:task is:open` finds open tasks
  - `#tag` (includes nested tags), `changed:7d`, `created:2026-10`
  - `"exact phrase"`, `-word` to exclude, terms combined with AND by default, `OR` between terms
- **Search sees what you see.** Matching and highlighting run on the markup-stripped text (concealed markers removed, links shown as their display text), so `**bold** text` matches "bold text" and highlights map directly onto the rendered row.
- **Search runs in memory.** Every query is a case-insensitive substring scan of the in-memory tree, ignoring diacritics; there is no search index. The tree holds page text too, loaded in the background after the first paint so launch stays fast. Phase 0 verifies Unicode case folding, diacritics and the 50 ms target at 100,000 nodes.
- Trashed nodes never match.
- A match inside a page keeps its bullet visible with the page marker highlighted, and shows the matching line dimmed beneath it.
- Saved searches arrive in Phase 3 (§10.1).

### 6.3 Quick capture

- **Quick capture** (Phase 1): `hyphal add "text" [--under <node>]` (§10.2) appends to the Inbox node by default, in its own transaction. The Inbox is a top-level node named `Inbox`, created on first use; its ID is stored in `meta`, so it can be renamed or moved freely and capture follows it. If the Inbox is in the Trash or has been purged, capture creates a new top-level `Inbox`, points `meta` at it, and says so; a trashed old Inbox stays in the Trash untouched. A running server notices the change through SQLite's `PRAGMA data_version` and refreshes open tabs.

### 6.4 Keymap and browser support

This section is the only place key bindings are defined. Other sections name commands and may mention their keys, but this table wins. In code the keymap is data, one table per context, and a test fails if a key is bound twice in one context, a command in the block-selection table below has no single key there, or a default binding is one a browser reserves.

**`Ctrl` in this spec means `Mod`:** Ctrl on Linux, Cmd on macOS. Native text keys keep each platform's own behavior (below).

**Contexts:** global, text editing, block selection, filter input, page, overlays (palettes, pickers, help, backlinks) and the Trash view. The Esc chain moves between them (§4.7).

**Two layers.** Block selection is the complete layer for everyday bullet commands: each has a single key there. Chords are shortcuts for the most frequent commands. No default binding uses a key the browser won't give up (below).

**Global** (every context, unless that context's table rebinds the key)

| Command | Key | Notes |
| --- | --- | --- |
| Help overlay | `?` in block selection, F1 | F1 needs Fn on Mac laptops |
| Command palette | Ctrl+P | Replaces the browser's print |
| Jump palette (Phase 2) | Ctrl+K | |
| Undo / redo | Ctrl+Z / Ctrl+Y, Ctrl+Shift+Z | Always the app's stack, never the browser's (§5.6) |
| (Save) | Ctrl+S | Nothing to save; shows "Saved automatically" |
| Zoom back / forward | The browser's Back / Forward | Moves through zoom history (§4.1) |

**Text editing** (the default state). Commands without a chord here are in block selection: Esc, then the key.

| Command | Key | Notes |
| --- | --- | --- |
| New bullet, split, outdent empty bullet | Enter | §5.2 |
| Backspace / Delete at bullet edges | Backspace / Delete | §5.3 |
| Indent / outdent | Tab / Shift+Tab | |
| Block selection | Esc | |
| Move by character / visual line | Arrow keys | Crosses bullet edges at a stable column |
| Move by word | The platform's keys | Ctrl+Left/Right on Linux, Option+Left/Right on macOS |
| Start / end of visual line, then bullet | Home / End | Cmd+Left/Right on macOS, Ctrl+A / Ctrl+E on macOS |
| First / last bullet in the zoom | Ctrl+Home / Ctrl+End | Cmd+Up/Down on macOS |
| Scroll a screen | Page Up / Page Down | |
| Delete word backward | Ctrl+Backspace | Option+Backspace on macOS |
| Select all text in the bullet | Ctrl+A | Linux; Cmd+A on macOS |
| Extend text selection | Shift+arrows, Shift+Home/End, Ctrl+Shift+Left/Right | Within one bullet |
| Wrap selected text in a marker | `*`, `~`, `=`, `` ` `` | Only with text selected (§5.8) |
| Copy / cut selected text | Ctrl+C / Ctrl+X | |
| Paste | Ctrl+V | §5.5 |
| Move subtree up / down | Ctrl+Shift+Up / Down | |
| Toggle complete | Ctrl+Enter | |
| Filter | Ctrl+F | Replaces the browser's find, which can't see virtualized rows |

**Block selection.** Single keys act on the selected bullets. Chords from text editing that act on a bullet also work here and apply to the whole selection.

| Key | Command |
| --- | --- |
| Up / Down | Move the selection |
| Shift+Up / Shift+Down | Extend the selection across siblings |
| `-` / `=` | Collapse / expand |
| `1` … `9` | Expand to level N (`1` collapses everything below the children) |
| `0` | Expand all descendants |
| Tab / Shift+Tab | Indent / outdent |
| `[` / `]` | Move subtree up / down (also Ctrl+Shift+Up / Down) |
| `z` / `Z` | Zoom in / out |
| `~` | Zoom to root |
| Enter | Return to text editing, cursor at the end |
| `x` | Toggle complete |
| `t` | Toggle task |
| `#` | Toggle heading style |
| Delete, `d` | Delete to the Trash, with an undo message |
| Backspace | Nothing; a hint says Delete or `d` deletes |
| Ctrl+C | Copy as Markdown (§5.5) |
| Ctrl+X | Cut: copy, then delete to the Trash |
| `p` | Open page |
| `o` | Follow the bullet's link or tag; with several, a picker lists them. A URL opens in a new browser tab |
| `D` | Duplicate subtree |
| `h` | Show / hide completed |
| `c` | Color picker |
| `s` | Toggle star (Phase 2) |
| `m` | Move to… (§5.4) |
| `/` | Filter |
| `?` | Help overlay |
| Esc | Clear an active filter; otherwise nothing, with a hint |
| Other printable keys | Nothing, with a hint |

**Filter input** (§6.2): typing edits the query. Enter or Down moves focus to the outline with the filter still active, on the selected match. Esc clears the filter and leaves the cursor on the selected match. With the filter active, Ctrl+F (or `/` in block selection) returns to the input, and Esc in block selection clears it.

**Page** (§5.7): the text-editing keys for movement, selection, clipboard and wrapping, across the whole page. Enter inserts a newline and continues a list, Tab inserts two spaces, Ctrl+Enter follows the link at the cursor, Ctrl+F opens the page's find bar, and Esc saves and returns to the outline, in block selection on the page's bullet. Redo is Ctrl+Y on Linux and Cmd+Shift+Z on macOS, as in the outline.

**Overlays:** typing filters the list; Up/Down move; Enter chooses; Esc closes. In the color picker, `1`–`8` choose a color directly and `0` removes it.

**Trash view** (§4.8): Up/Down move; Enter previews the deletion; `r` restores it; `d` purges it after confirmation; Esc returns to the outline.

**Browser rules**

- **Supported browsers:** the current and previous major versions of Chromium-family browsers, Firefox and Safari, on Linux and macOS. CI runs Chromium, Firefox and WebKit. The app window (§4.7) is the recommended way to run on Linux.
- **Reserved keys.** Browsers keep some keys for themselves in a normal tab: at least Ctrl+W, Ctrl+T, Ctrl+N, Ctrl+Tab, and Ctrl+Q on Linux. No default binding uses them, and a config override that does gets a warning banner.
- **The rest are canceled.** The app calls `preventDefault` for its bindings (Ctrl+P, Ctrl+F, Ctrl+S and so on). Native text behavior is left alone wherever the app has no rule for it.
- **Native undo is disabled for bullets.** `beforeinput` `historyUndo` and `historyRedo` route to the app's stack, including from menus.
- **Compositors.** Super combinations under Hyprland and Ctrl+arrows under macOS Spaces are never used by default.
- **Keys are configurable** in TOML, per context. The same checks run when the config loads; on a conflict the app keeps the default binding for that command and shows a banner.
- **Human key matrix.** `/keyprobe` (development builds and `hyphal serve --dev`) lists every binding and which ones reach the page. It is recorded by hand for Chromium in a tab and an app window, Firefox and Safari, because automated tests bypass browser-level shortcuts.

### 6.5 Mouse

- Phase 1: scroll, click to place the cursor, click a glyph to toggle collapse, click a checkbox to complete, click a breadcrumb to zoom, click a bullet glyph with Alt to zoom, overlays clickable.
- Native text selection works inside the focused bullet. Dragging across bullets selects nothing in Phase 1.
- **Mouse selection (Phase 2):** dragging from one bullet across others enters block selection, and Shift+click extends a selection.
- **Drag-to-move (Phase 2):** drag a bullet's glyph to move its subtree, or the whole selection when dragging a selected bullet. A drop indicator shows the new parent and position before you let go, and dragging near the window's edge scrolls. Esc cancels. A drop is one undoable change set, and nothing moves outside the current zoom or into its own subtree. It uses native HTML drag and drop. Every move stays available from the keyboard.

### 6.6 Palettes, stars and small commands

- **Command palette (Ctrl+P, Phase 1):** every command searchable by name, showing its key binding, recently used first. It is the discoverability layer for everything in this spec, so key limits never hide a feature.
- **Stars (Phase 2):** "Toggle star" stars the bullet you're on: the focused bullet, or the selected ones in block selection. To star the zoom target, toggle it on the zoom heading. Stars appear first in the jump palette and in a Stars view; `is:starred` finds them.
- **Small commands (Phase 1),** with keys in §6.4 where they have one. Rarely used commands are palette-only by design: Delete page, Sort children, Copy link to node, and Bold, Italic and Color… applied to whole bullets.
  - Expand all (`0`) and expand to level 1–9 under the current node; level 1 collapses everything below the children
  - Duplicate subtree
  - Show / hide completed
  - Toggle task, toggle heading style
  - Move to… (fuzzy parent picker, §5.4)
  - Copy link to node — Phase 2, with node links (§10.1)
  - Sort children (alphabetical, completed last) — Phase 2

## 7. Storage architecture

The outline lives in one SQLite database, which is the only source of truth. Snapshots and exports are copies of it.

### 7.1 Locations

```text
Data directory
  Linux:  ~/.local/share/hyphal/            (honors XDG_DATA_HOME)
  macOS:  ~/Library/Application Support/hyphal/

  outline.db          # the data (WAL mode)
  snapshots/          # automatic backups (§8.5)
  exports/            # default target for scheduled export

Config (both platforms, CLI convention)
  ~/.config/hyphal/config.toml              (honors XDG_CONFIG_HOME)
  ~/.config/hyphal/themes/*.css             # your themes (§4.4)
  ~/.config/hyphal/user.css                 # your overrides (§4.4)

Runtime
  $XDG_RUNTIME_DIR/hyphal/server-*.json     # how `hyphal` finds a running server (§9.3)

The cookie signing secret is in the data directory, in a 0600 file (§9.3).

Sync folder (Phase 5, user-chosen, e.g. inside iCloud Drive or Syncthing)
  <sync-folder>/hyphal/<device-id>.log      # §7.6
```

Separate outlines are separate database files, selected with `--db` or a config default. `hyphal paths` prints every location in use.

### 7.2 Schema (sketch)

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
-- schema_version, format_id (neutral, never the app name),
-- root_id, inbox_id, ui_opened (§4.5), a one-paragraph description of the schema

CREATE TABLE nodes (
  id           TEXT PRIMARY KEY,     -- random, stable forever: 12 chars, lowercase base32
  parent_id    TEXT REFERENCES nodes(id),
  position     TEXT NOT NULL,        -- fractional index among siblings
  text         TEXT NOT NULL,        -- one line, inline markup as typed (§5.8)
  is_task      INTEGER NOT NULL DEFAULT 0,
  is_heading   INTEGER NOT NULL DEFAULT 0,  -- heading-style bullet (§4.6)
  completed_at TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  version      INTEGER NOT NULL DEFAULT 1,  -- bumped on every write (§7.3)
  deleted_at   TEXT,                 -- soft delete; non-null = in Trash
  deleted_by   TEXT                  -- change set that deleted it (§8.3)
);

CREATE TABLE pages (                 -- at most one page per bullet (§5.7)
  node_id    TEXT PRIMARY KEY REFERENCES nodes(id),
  markdown   TEXT NOT NULL,          -- full Markdown document
  updated_at TEXT NOT NULL,
  version    INTEGER NOT NULL DEFAULT 1   -- bumped on every write (§7.3)
);

CREATE TABLE stars (
  node_id    TEXT PRIMARY KEY REFERENCES nodes(id),
  starred_at TEXT NOT NULL
);

CREATE TABLE view_state (           -- per device; not in history or sync
  node_id   TEXT PRIMARY KEY REFERENCES nodes(id),
  collapsed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE ui_state (              -- per device; not in history or sync
  key   TEXT PRIMARY KEY,            -- show_completed (default true), zoom, cursor:<zoom-id>, palette_recent
  value TEXT NOT NULL                -- JSON
);

CREATE INDEX nodes_by_parent  ON nodes(parent_id, position);
CREATE INDEX nodes_in_trash   ON nodes(deleted_at) WHERE deleted_at IS NOT NULL;

-- change_sets and ops: see §8.2
```

- **IDs** are random and permanent. They are what the CLI and links use.
- **Ordering** uses fractional index strings, so moving or reordering a node rewrites one row.
- **Completion:** `is_task` marks an open task (`[ ]`); `completed_at` marks completion for any node. Reopening clears `completed_at` and keeps `is_task`, so a plain bullet never turns into a task by accident.
- **View state** (collapse) and **UI state** (show/hide completed, the current zoom, the cursor per zoom, palette recents) are per device and excluded from history: folding never creates undo steps. Both are included in JSON export so a restore brings back the same view. The filter is never persisted.
- **Timestamps** are RFC 3339 in UTC with milliseconds (`2026-10-17T09:30:00.123Z`), displayed and interpreted in local time (`created:2026-10`). `updated_at` changes when the node's own text, page, task, heading or completion changes, not on moves, stars or changes to descendants; `changed:7d` uses it.
- **Integrity:** `PRAGMA foreign_keys=ON` on every connection. History ops reference nodes by ID without a foreign key, so purging a node never cascades into history (§8.3).
- **File permissions:** the data directory is created `0700`, and the database, snapshots and exports `0600`.
- The schema stays plain enough that anyone opening the file in a SQLite browser understands it in a minute.
- **Formatting** is stored as the markup the user typed, not as separate style rows. The text stays readable in any SQLite browser and exports without conversion; rendering parses it on display.
- **Tags** are not stored separately: they are parsed from bullet text and pages into the in-memory tree at load and on every edit. A tag written in a page counts for the page's bullet.
- **Stars** are user data and part of history; collapse state is not.

### 7.3 Transactions and concurrency

- Every user action is one transaction that updates `nodes` and writes its history together (§8.2).
- **Typing is committed when it seals.** The browser sends each keystroke's resulting text to the server (§9.3). Consecutive typing in one node forms one operation, kept in the server's memory until it seals: after a 2-second pause, on leaving the node, on any structural action, on window close, or 5 seconds after it opened. It is then written once and never changed. A crash of the server loses at most the unsealed typing, 5 seconds at worst.
- **The browser keeps what the server hasn't acknowledged.** Every typing message has a sequence number, and every state reply echoes the last one applied. A reply built before the latest typing never overwrites it (the spike found this bug). If the connection drops, the page keeps unacknowledged text, shows a banner, reconnects and resends. A closed tab can only lose keystrokes still in flight on loopback.
- Pending typing is flushed and sealed when the last tab closes (after the grace period, §9.3), on SIGHUP and on SIGTERM, before the server exits.
- WAL mode with `synchronous=FULL`. The extra cost is negligible at this write rate.
- The server keeps an in-memory tree for the UI; all writes go through `core` to `store`.
- The CLI opens its own connection with a busy timeout and doesn't need the server. The server polls `PRAGMA data_version` about every 500 ms; when another connection has written, it reloads the whole tree on its worker and keeps each tab's zoom, cursor and selection by node ID. Unsealed typing survives the reload and still commits afterwards, under the concurrency rule below. Incremental reloading is a later optimization if ever needed.
- **Concurrent edits to one node.** Every write bumps the node's `version`. If another connection changes a node while the server holds unsealed typing on it, the user's typing is never dropped: it commits as a new change set after the other change, which stays in history and can be reverted, and a message says so. A CLI or agent that must not overwrite newer work passes `--if-version <n>` and gets a conflict error instead. The same rule applies to a page open in the built-in editor while `hyphal page set` runs.

### 7.4 Migrations

- Schema versions are numbered and migrations run forward only.
- A snapshot is taken before every migration.
- A database with a newer schema than the app knows opens read-only with a banner, never migrated backwards.

### 7.5 Export and import

| Format | Export | Import |
| --- | --- | --- |
| JSON | Lossless: every node including the Trash, pages, stars, collapse and UI state; IDs, positions and timestamps. History with `--history` | Into an empty database only; on a non-empty one (including the first-run sample outline) the error names `--replace`. `--replace` snapshots the database, then replaces its contents. `--under <node>` imports as new nodes with new IDs instead |
| Markdown | The shared dialect below; one file (`outline.md`, the default) or, with `--split`, one file per top-level node; pages as separate files in `pages/` | New nodes under a chosen parent |
| OPML | Plain text for Workflowy and other outliners (below) | New nodes under a chosen parent (for migrating from Workflowy and other outliners) |

- **Markdown and OPML imports** without `--under` land in one new top-level bullet named after the imported file (`workflowy-export.opml`), so the whole import can be inspected, moved or trashed as a unit. With `--under`, the imported top-level items become children of that node directly.
- **Drop to import (Phase 2):** dropping an `.opml` or `.md` file on the window imports it under the current zoom as new nodes, as one change set, with a message saying where it landed.
- JSON export never contains the config file or snapshots.
- Every export records the app and the export time without breaking its format: a top-level `exported_by` field in JSON, `<head>` elements in OPML (after the `<?xml` declaration), and an HTML comment on the first line of Markdown, which importers skip.
- Exports are written atomically (temp file, then rename).
- **Scheduled export** (Phase 2) is an optional setting: Markdown or JSON, daily or on quit, into a folder you choose (for example a Git repo), keeping the last N exports. There is no daemon: a daily export is due when the last one is more than 24 hours old, checked at launch, at quit and on the server's timer. `hyphal export --scheduled` runs it from a systemd timer, launchd or cron.
- CI checks that database → JSON → database is identical: every exported field is equal, including IDs, positions, timestamps, pages, stars and view state, and history when exported with `--history`.

**Markdown dialect.** Export, import and multi-line paste (§5.5) share one dialect, close to GitHub-flavored Markdown:

```markdown
- Plain bullet
- [ ] Open task
- [x] Completed task
- ## Heading-style bullet
  - Child, indented two spaces
- Bullet text with its ID ^k3j9x2m4q7tb
```

- Bullet text never starts with `[ ] `, `[x] ` or `## ` (§5.4), so those markers are unambiguous.
- Inline markup is written as stored (§5.8). Export converts color tags to `<span style="color: …">`; copy (§5.5) keeps them as typed.
- The ` ^id` suffix is written only by `hyphal export --ids`. Markdown import always creates new nodes and ignores the suffix.
- A completed bullet that is not a task exports as `[x]` and re-imports as a completed task. This small loss is documented; JSON keeps it exact.
- Import accepts any consistent indentation (spaces or tabs) and `-`, `*`, `+` or numbered list markers. Lines that aren't list items become bullets.

**Pages in exports:** Markdown export writes each page as its own `.md` file in a `pages/` folder, and the outline links to it. File names are a slug of the bullet text (lowercase letters, digits and hyphens, at most 50 characters) plus the node ID, so they never collide. The result reads much like an Obsidian vault. JSON export includes pages in full; OPML puts them in `_note`.

**Workflowy OPML import mapping.** Verified against a real Workflowy export in Phase 1 (§12.4).

| Workflowy | Imported as |
| --- | --- |
| Outline text with inline HTML | Bullet text with Markdown-style markup (§5.8) |
| `<b>`, `<i>`, `<s>`, `<code>`, `<a href>` | `**…**`, `*…*`, `~~…~~`, `` `…` ``, `[label](url)` |
| `<span class="colored c-red">` and other text colors | `<red>…</red>`, nearest of the eight colors |
| Background-color spans | `==…==` |
| `_note` | Page |
| `_complete="true"` | Completed bullet (not a task) |
| `#tags` | Tags, unchanged |
| `@mentions`, dates | Plain text |
| Mirrors | Independent copies; the mirror link is lost. The import summary counts them when the export marks them |
| Boards | Plain children; the board layout is lost |

**OPML export** writes each bullet's text with markup removed (colors and highlights dropped, link labels kept), its page as Markdown in `_note`, and completion as `_complete="true"`. It does not convert formatting back to Workflowy HTML; JSON and Markdown are the lossless and readable exports.

### 7.6 Sync (Phase 5)

Multi-device sync ships in Phase 5, and an iPhone app is a separate follow-up project. Nothing sync-specific is built before then; Phase 5 adds it with a schema migration.

**The planned model: per-device logs in a shared folder.** Each device appends its operations to its own file in a user-chosen sync folder (iCloud Drive, Syncthing or similar) and reads the others' files to merge. Devices never write the same file, so the file-sync tool never sees a conflict. No server is involved, and the live database never lives in the sync folder (§8.1).

**What earlier phases already provide,** because the app needs it anyway:

- Random, permanent IDs for every node (§7.2).
- History operations that are self-contained: `after` holds complete field values, not diffs, and a move carries parent and position together (§8.2).
- Soft deletes; undo and revert create new operations and never rewrite history.

**Added in Phase 5:** device IDs, hybrid logical clock timestamps on change sets, a device tie-breaker in sibling positions, per-field last-writer-wins merging, a proven tree-move algorithm for concurrent moves (Kleppmann et al.), the log format and compaction, tombstones for Forget node, and convergence tests. The open details are in §12.3.

## 8. Safety, history and recovery

Three layers protect the data, each against a different failure: history against bad changes, the Trash against deletes, and snapshots against a damaged database file. Off-machine backup of the data folder remains the user's job, and the docs say so plainly.

### 8.1 Durability

- WAL mode, `synchronous=FULL`, one transaction per action (§7.3).
- `PRAGMA quick_check` runs in the background after the first paint, so it never delays launch; its duration on large databases is measured in Phase 0. A failure switches the session to read-only at once, writes any uncommitted typing to a recovery Markdown file in the data directory, and offers to restore the latest good snapshot.
- **Failed writes** (disk full, permissions): the edit stays in memory, the save indicator turns red, a banner explains, and the write is retried every few seconds. Typing continues. If the server exits with unsaved edits, it writes them to a recovery Markdown file, and the next launch says where.
- **Never sync the live database** with Syncthing, Dropbox or similar; it can corrupt. The app warns if `outline.db` sits inside a known sync folder. Sync snapshots or exports instead.

### 8.2 History

```sql
CREATE TABLE change_sets (
  id          TEXT PRIMARY KEY,  -- random
  at          TEXT NOT NULL,     -- timestamp (§7.2)
  source      TEXT NOT NULL,     -- 'ui', 'cli', 'import', 'restore', 'agent:<name>'
  description TEXT,              -- e.g. 'Moved "Design notes"'
  undo_of     TEXT               -- change set this one undoes (§5.6)
);
CREATE INDEX change_sets_by_at ON change_sets(at);

CREATE TABLE ops (
  id            INTEGER PRIMARY KEY,
  change_set_id TEXT NOT NULL REFERENCES change_sets(id),
  node_id       TEXT NOT NULL,
  kind          TEXT NOT NULL,   -- insert, delete, restore, edit_text, edit_page, move,
                                 -- set_task, set_heading, set_complete, set_star
  before        TEXT,            -- JSON: prior values of the changed fields
  after         TEXT             -- JSON: complete new values, never diffs
);
CREATE INDEX ops_by_node ON ops(node_id);
CREATE INDEX ops_by_change_set ON ops(change_set_id);
-- move carries parent and position together: {parent_id, position}
-- edit_page with before = null creates a page; with after = null deletes it
-- a delete made by a merge (§5.3) records the survivor: after = {deleted: true, merged_into: <id>}
```

- **Current state is authoritative;** history is a record beside it, written in the same transaction. Nothing is rebuilt by replay.
- **Before and after values** make every operation reversible by applying its inverse.
- **Change sets** group the operations of one action: a subtree move, a CLI command, a page save. They are the unit of undo, revert and "what did this agent do?".
- **Coalescing:** consecutive edits to one node's text form a single operation, written once when it seals (§7.3) and never changed.
- **Append-only:** history is never rewritten, except by explicit erasure: `hyphal history purge` and Forget node (§8.4).
- **Revert** of an older change set applies its inverses to fields not changed since. Any skipped fields are reported, never overwritten.
- **History view** (Phase 2): browse a node's past versions and restore one, or list change sets by time and source.

### 8.3 Trash

- Deleting sets `deleted_at` and `deleted_by` (the deleting change set) on every node in the subtree; nothing is removed. The Trash view lists deletions, newest first.
- **Leftovers stay out of sight.** The Trash view hides two kinds of deletion whose content isn't lost: bullets merged away by Backspace or Delete (§5.3–5.3a), whose text now lives in the survivor, and a single empty leaf with no page. They are still soft-deleted, purge by age like any other deletion, and are undoable through history; `hyphal trash list --all` shows them.
- Restore brings back exactly one deletion: the nodes whose `deleted_by` matches. A child deleted separately earlier stays in the Trash.
- A restored subtree returns to its original position among its siblings when that position still exists, otherwise to the end of its parent. If the parent is gone too, it goes under the nearest surviving ancestor, else the root, and the message says where.
- Items older than 30 days are purged for real (configurable). "Empty Trash" purges everything immediately, after a one-line confirmation.
- Purging deletes the node rows with their pages, stars and view state. Purging a deletion also purges, in the same transaction, any descendants still in the Trash from earlier deletions, so no trashed row is ever left pointing at a purged parent. History ops that mention them stay until `hyphal history purge` removes them; reverting one reports the node as gone.
- After a delete the cursor moves to the next sibling, else the previous one, else the parent. The message names the bullet and counts its descendants.

### 8.4 Retention and erasure

- History is kept in full; nothing is compacted. `hyphal history purge --before <date>` removes older history when you want the space back.
- **Forget** (Phase 4) works on a Trash entry: it erases that deletion's whole subtree from `nodes`, `pages`, `stars`, `view_state` and history (purging earlier trashed descendants as in §8.3), and records a change set whose description says what was forgotten, without its content and with no ops. Snapshots still contain it until they rotate out, and the command says so.
- Expected growth is a few hundred KB per day of heavy use, about 100 MB a year. This is an estimate to verify in Phase 0.

### 8.5 Snapshots

- Taken with SQLite's online backup API: once a day when the outline has changed, and before every migration or restore. "Due" means the newest snapshot is more than 24 hours old; the server checks at launch, at exit and on its timer, and every CLI write checks too, so a window left open for days and a CLI-only outline both get snapshots.
- The last 14 are kept.
- Each snapshot is checked with `PRAGMA quick_check` after it is written; one that fails is deleted and reported. "Latest good snapshot" means the newest that passed.
- `hyphal snapshot list` lists them with their sizes; `hyphal snapshot restore <name>` restores one after snapshotting the current state first, so a restore is itself reversible.
- A restore never renames files over the database (with `-wal` and `-shm` files present that can corrupt it). It copies the snapshot into the live database with the online backup API. It refuses while a server holds the lock (§8.6). When the UI offers a restore itself (after a failed integrity check), the server closes its other connections first and reopens afterwards.

### 8.6 Locking and tabs

- One server per database, enforced with an exclusive `flock` on a lock file next to the database (`outline.db.lock`). It is not an SQLite lock, so CLI writers are never blocked by it, and the kernel releases it if the server dies. Running `hyphal` while a server holds the lock opens a window on that server (§9.3).
- **Any window can edit.** Open as many tabs or windows as you like, for example two zooms side by side. Each has its own zoom, cursor, selection, filter and scroll position; they share one server, one undo stack and one outline, and each shows the others' changes live. Whichever window has focus is the one you're typing in; losing focus seals its typing (§7.3), so two windows never hold unsealed text on one bullet. There is no role to manage and no takeover.
- **Read-only** applies to the whole server, and only for a real reason: a database with a newer schema, or a failed integrity check.
- The CLI doesn't take the lock. They are ordinary writers with a 5-second busy timeout; past it they exit with a `busy` error code (§10.2). SQLite serializes the writes.

## 9. Implementation

### 9.1 Stack

| Area | Choice | Notes |
| --- | --- | --- |
| Language | Go | Simple, fast to compile, one static binary; well suited to agent-driven development |
| UI | Native web components, HTML, CSS, JavaScript (ES modules) | Light DOM (§4.4). No frontend library or framework without Thor's approval, and no build step for our code: files are embedded with `go:embed` and served as written. CodeMirror is the one approved exception (below). |
| Browser features | `contenteditable="plaintext-only"`, CSS Custom Highlight API, `<dialog>`, Popover API, cascade layers, CSS nesting, `light-dark()`, custom properties, Async Clipboard, Selection API, `Intl.Segmenter`, WebSocket | Modern and native only. Anything without support in all three engines needs a fallback that keeps behavior correct |
| Server | Go `net/http` + `github.com/coder/websocket` | Loopback only. One WebSocket per tab |
| CLI | Cobra | Shared by quick capture, scripting and agents |
| Database | `modernc.org/sqlite` | Pure Go; no C toolchain, so cross-builds stay simple. Online backup API verified in P0-05 |
| Text | `rivo/uniseg` | Grapheme boundaries for search matches; the browser uses `Intl.Segmenter` |
| Page editor | CodeMirror 6 (MIT) | Live preview and the plain-text settings editor (§5.7). Committed, prebuilt vendor files, embedded like other assets, rebuilt only on upgrade, and loaded when the first page opens. Parses Markdown and highlights code in the browser, colored by theme tokens through CSS classes. Go never renders Markdown |
| Fuzzy matching | A Go fuzzy-finder library such as `sahilm/fuzzy` | Jump and command palettes, on the server |
| Import / export | `encoding/json`, `encoding/xml`, small list parser | JSON, OPML, Markdown |
| Config | TOML (`pelletier/go-toml/v2`) | Platform paths per §7.1 |
| Browser tests | Playwright (development only, never shipped) | Approved by Thor. Runs under Node, pinned in `mise.toml` |
| Distribution | GoReleaser | Homebrew tap for macOS, AUR package for Arch and Omarchy |

### 9.2 Package architecture

Both front ends share one core; only the database is authoritative.

```text
┌──────────────────────────────┐
│ Browser                      │
│ Web components, CSS themes   │
└──────────────┬───────────────┘
               │ WebSocket + HTTP (loopback)
┌──────────────▼───────────────┐      ┌──────────────────────────────┐
│ server + web (embedded)      │      │ cli                          │
│ Sessions, protocol, auth     │      │ Cobra commands, JSON output  │
└──────────────┬───────────────┘      └───────────────┬──────────────┘
               │ commands                             │ same operations
┌──────────────▼──────────────────────────────────────▼──────────────┐
│ core                                                               │
│ In-memory tree, domain operations, undo stack, search              │
└─────────────────────────────────┬──────────────────────────────────┘
                                  │ load, commit, revert
┌─────────────────────────────────▼──────────────────────────────────┐
│ store                                                              │
│ SQLite access, history writes, snapshots, import and export        │
└──────────┬──────────────────────┬───────────────────────┬──────────┘
           │ transactions         │ backup API            │ on demand
┌──────────▼─────────┐  ┌─────────▼──────────┐  ┌─────────▼──────────┐
│ outline.db         │  │ Snapshots          │  │ Exports            │
│ Nodes, pages,      │  │ Daily and          │  │ Markdown, JSON,    │
│ history; the only  │  │ pre-migration      │  │ OPML, on demand    │
│ source of truth    │  │ restorable copies  │  │ or scheduled       │
└────────────────────┘  └────────────────────┘  └────────────────────┘
```

Four Go layers (`server`, `cli`, `core`, `store`), one browser client (`web`), and three kinds of on-disk data.

The server and CLI never touch the database directly; only `store` does. The browser never touches anything but the server's protocol, and holds no rules about the outline.

### 9.3 Runtime model

- **Who owns what.** Go owns the tree, every rule in §5, undo, search, selection state and the keymap's definition. The browser owns the DOM, focus, the caret, scroll position, overlays, and typing inside the focused bullet. Rule logic is never duplicated in JavaScript.
- **Typing is local, structure is a round trip.** Typing updates the focused bullet's DOM at once and sends its text to the server, so it never waits for a reply. A structural key (Enter, Tab, Backspace at an edge and so on) sends an action and waits for the state reply. Keys pressed while a reply is pending are queued and replayed in order, so fast typing across Enter can't land on the wrong row. The spike verified this model.
- **Rendering.** Only a window of rows around the viewport is in the DOM. Each state reply carries that window; rows are reused by node ID so the focused element survives updates. The focused bullet is `contenteditable="plaintext-only"` and shows its source text; marker dimming and filter matches on it use the CSS Custom Highlight API with ranges from the server, so editing never touches the text nodes. All other rows render styled spans from the server's parsed runs, built with DOM APIs and never with `innerHTML`.
- **Slow work** (commits, reloads, snapshots, exports, the `data_version` poll) runs on server goroutines off the request path. Commits and reloads go through one FIFO worker.
- **Lifecycle.** `hyphal` takes the lock and starts a detached `hyphal serve`, or finds the running one through a `0600` state file in the runtime directory, and then opens a window. The server exits 10 seconds after its last tab closes, which covers reloads. `hyphal serve` runs in the foreground for debugging.
- **Security.** The server binds only `127.0.0.1`, on a stable port derived from the database path, moving to the next free port if it's taken, so addresses and the browser's per-site memory survive restarts. The cookie name includes the port, because browsers share cookies across ports. Every request must carry a correct `Host` and, for WebSocket and writes, `Origin`, which blocks DNS rebinding and cross-site WebSockets. A launch opens a URL holding a single-use ticket, exchanged at once for a long-lived `HttpOnly`, `SameSite=Strict` cookie, signed with a secret kept in the data directory, and removed from the address bar, so the process list never holds a usable secret. Pages are served with a Content Security Policy allowing only same-origin resources and no inline scripts. Raw HTML in a page stays literal text (§5.7), and links allow only `http`, `https`, `mailto` and node links.
- **Reconnect.** The page reconnects automatically after a dropped socket (a restarted server, a sleep/wake), resends unacknowledged typing, and asks for fresh state. If the server has exited, reopening needs `hyphal`.

### 9.4 Performance targets

| Measure | Target | Workload |
| --- | --- | --- |
| `hyphal` to first paint, browser already running | < 100 ms | 10,000 nodes; the integrity check runs after the first paint (§8.1) |
| `hyphal` to first paint, browser not running | Measured, no hard target | Depends on the browser; the spike saw 130 ms headless to over 1 s |
| Keypress to frame, typing | < 16 ms | Same, typing in a deep node |
| Enter or Tab, keypress to frame | < 33 ms p95 | Same; includes the round trip |
| One action committed (state + history) | < 5 ms on Linux | Same. On macOS, real durability needs `PRAGMA fullfsync`, often 5–20 ms; commits run on the worker, so this never delays a reply |
| In-place filter update | < 50 ms per keystroke | 100,000 nodes |
| Keypress to frame, typing on a page | Within a frame at p95 | A 2,000-line page; 20,000 lines measured only |
| First page opened, including loading the editor | Measured, no hard target | Later pages open at once |
| CLI change visible in an open tab | < 1 s | Any |

Targets are checked with generated databases: as absolute numbers on fixed benchmark hardware, and in CI only as regressions against a stored baseline, because shared runners are too noisy for absolute thresholds.

### 9.5 Testing

- **Export round trip:** database → JSON → database is identical, for generated and real outlines (property tests with `pgregory.net/rapid`).
- **History tests:** random sequences of operations followed by undo-all restore the original state; reverting any single change set leaves all other changes intact.
- **Crash injection:** kill the process at random points during editing; the database passes `PRAGMA integrity_check` and loses at most the unsealed typing (§7.3).
- **Migration tests:** a fixture database from every past schema version migrates cleanly, with a snapshot taken first.
- **Import tests:** real Workflowy OPML and Markdown exports from Logseq and Obsidian import without loss.
- **Parser fuzzing** with Go's built-in fuzzing for the inline-markup parser, the list importer and OPML.
- **Session tests:** Go tests drive a server session through the protocol without a browser: actions in, rows, cursor and messages out.
- **Browser tests:** Playwright drives real keystrokes in Chromium, Firefox and WebKit, on Linux and macOS.
- **Interaction matrix:** browser tests for every row of the Enter, Backspace and Delete tables (§5.2–5.3a) and every binding in §6.4.
- **Screen references:** ARIA snapshots as text goldens for structure, plus screenshots on Linux Chromium for review only.
- **Theme checks:** no raw values outside token files, AA contrast for bundled themes, and every token used.
- **CLI tests:** every command's `--json` output is checked against golden files.

### 9.6 Built by agents

The app will be built entirely by AI coding agents, working from a phased implementation plan derived from this spec. The spec and repository follow rules that make that reliable:

- **Acceptance criteria per feature,** written as observable behavior ("Backspace on an empty bullet with children does nothing and shows a hint"), each mapped to at least one automated test.
- **No manual verification steps.** Every behavior is checkable automatically: rules through `go test`, UI through browser tests, data through the CLI's `--json` output, safety through crash and round-trip tests. The human checkpoints are listed explicitly and are not acceptance criteria for agents: visual review (§4.6), the hands-on key matrix (§6.4), the comparison-set evaluation (§3), and the Phase 1 week of daily use (§11).
- **The CLI is the agents' test harness.** Agents create outlines, run commands and inspect results through the same CLI users get (§10.2), which is why it ships in Phase 1.
- **One package per task where possible.** The plan hands agents work along package boundaries (`core`, `store`, `server`, `web`, `cli`) with stable interfaces between them.
- **Golden files are reviewed, not regenerated blindly.** A changed golden must be explained in the commit message.
- **Stable section numbers** in this spec are referenced from the plan, tests and commit messages.

## 10. Obsidian-inspired features and the CLI

### 10.1 Obsidian-inspired features (Phases 2–3)

Obsidian's best ideas apply here to nodes rather than files.

- **Node links (Phase 2):** type `[[` to search and link to any bullet. Links are stored by node ID, so they survive moves and renames, and they display the target's current text. `o` in block selection (or a click) zooms to its target. Links render as real anchors to `/#/<id>`. Markdown export writes Obsidian-style links.
- **Backlinks (Phase 3):** an on-demand floating overlay listing every bullet that links to the current node, each with its breadcrumb.
- **Saved searches (Phase 3):** "Save search" in the command palette stores the current filter query under a name (`Open bugs` = `#bug is:open`). Saved searches are listed in the jump palette next to starred locations; choosing one opens the filter with that query. They are stored in their own small table and included in JSON export.
- **Templates (Phase 3):** any subtree tagged `#template` can be instantiated under the current node with one command, from the command palette.
- **Link previews (Phase 3):** hovering a `[[link]]` shows a small popover with the target's breadcrumb and first lines, through the Popover API. It never takes focus.
- Graph view and plugins remain non-goals.

**Web extras (Phase 3):**

- **Image attachments:** paste or drop an image onto a page. It is stored in the database, served from the app itself, undoable, included in JSON export, and written as files by Markdown export.
- **`hyphal://` links:** Copy link to node can give a link that opens Hyphal from another app, through a scheme handler in the `.desktop` file (Linux).
- **Print and PDF:** a print stylesheet renders a whole page or zoom, so the browser's print dialog gives a clean PDF. Ctrl+P stays the command palette; "Print…" is a palette command.
- **Theme picker:** a palette command lists themes with a live preview. The choice is per device, and overrides the `theme` setting until changed.

### 10.2 CLI and agent interface

A robust CLI ships in **Phase 1**, for users, scripts and AI agents alike. It is a thin layer over the same `core` operations as the UI, and it doubles as the build agents' test harness (§9.6). Phase 4 adds `forget`, batch `apply` and agent conveniences on top.

Every `<node>` argument accepts an ID or a path.

```bash
hyphal                      # start the server if needed and open a window (§9.3)
hyphal serve [--dev]        # run the server in the foreground
hyphal add "Fix tooltip positioning" [--under <node>] [--after <node> | --before <node>]
hyphal open [<node>]        # open a window at this bullet (§4.1)
hyphal capture              # Phase 2: small capture window (§4.7)
hyphal show <node> [--depth N] --json
hyphal tree [--root <node>] --json
hyphal search "tooltip #bug is:open" --json
hyphal edit <node> --text "New text" [--if-version N]
hyphal set <node> [--task | --no-task] [--heading | --no-heading]
hyphal move <node> --under <node> [--after <node> | --before <node> | --index N]
hyphal complete <node>      # and: reopen, star, unstar
hyphal delete <node>        # to Trash
hyphal forget <id>          # Phase 4: erase a Trash entry for good (§8.4); asks for --yes
hyphal trash list [--all]|restore <id>|empty   # <id>: the deleted subtree's top node
hyphal page get <node>      # Markdown to stdout
hyphal page set <node> --file page.md [--if-version N]   # or from stdin
hyphal page delete <node>
hyphal apply --file ops.json          # Phase 4: many operations as one change set
hyphal apply --schema                 # Phase 4: prints the ops.json JSON Schema
hyphal history list [--source agent:claude] --json
hyphal history purge --before <date>
hyphal revert <change-set-id>
hyphal export --format markdown|json|opml [--out <dir>] [--ids] [--history] [--split] [--scheduled]
hyphal import --format json|opml|markdown <file> [--under <node>] [--replace]
hyphal snapshot list|restore <name>
hyphal check                # integrity check (quick_check plus tree invariants)
hyphal paths
```

- Core verbs on nodes are top-level; everything else is grouped by noun (`trash`, `page`, `history`, `snapshot`, and `sync` in Phase 5). Tag commands arrive with tags in Phase 2.
- **Paths** are breadcrumb paths from Home, with `/` between segments: `Projects/ControllerCraft`. A literal `/` in a bullet's text is written `\/` (`Projects/2026\/10 review`). Segments match the bullet's text with markup stripped, exactly first, then case-insensitively. If a segment matches several siblings, the command fails with an `ambiguous` error listing the candidates and their IDs. Path lookups return the ID for later calls.
- **`ops.json`** (Phase 4) is a JSON array of operations, each `{"op": "<command>", ...arguments}`, using the same names and arguments as the commands above (`add`, `edit`, `set`, `move`, `complete`, `delete`, `page_set`, …). An operation can refer to a node added earlier in the batch by a temporary `"ref"`. `hyphal apply --schema` prints the exact schema.
- **Output:** every command supports `--json`, with a stable, versioned envelope: `{"ok": true, "data": …}` or `{"ok": false, "error": {"code": "…", "message": "…", "candidates": [...]}}`. Human-readable output is the default. Export without `--out` writes a single-file format to stdout.
- **Exit statuses:** 0 success, 1 other error, 2 usage error, 3 `not_found` or `ambiguous`, 4 `conflict` (`--if-version` mismatch), 5 `busy` (locked past the 5-second busy timeout, §8.6), 6 `read_only` (the database has a newer schema than this version knows).
- Every write is one change set tagged with its source: `cli` by default, `--source agent:<name>` for agents, so their changes can be reviewed and reverted as a unit. The source is self-declared: it is for attribution, not a security boundary.
- `hyphal apply` (Phase 4) lets an agent submit a batch of operations atomically; either all apply or none do.
- Writes are safe while a window is open; it picks them up within a second (§7.3).
- The database is always explicit: `--db`, then the `HYPHAL_DB` environment variable, then the config default.
- **Phase 4:** `forget`, `apply`, a shipped usage guide for agents, and richer query output. There is no MCP server: agents drive the CLI directly.

## 11. Staged delivery plan

Phase 1 delivers the core of the Workflowy clone (every Phase 1 *Must* row), usable daily on your own imported outline. Phase 2 completes Workflowy parity, Phase 3 adds the Obsidian-inspired features and web extras, and agent integration and sync come last.

Six phases, each with an exit gate:

| Phase | Scope | Exit gate |
|---|---|---|
| **0 · Storage and browser foundations** | Schema, history, JSON round trip; server skeleton, protocol, outline component, key matrix | Export and import are identical; a crash loses at most the unsealed typing; the outline skeleton passes in all three engines |
| **1 · Minimum lovable Workflowy clone** | Editing rules, block selection, pages with live preview, formatting; theming, palettes, CLI, quick capture, Workflowy import | A week of daily use on your own outline; side-by-side visual review passes |
| **2 · Full Workflowy parity** | Tags, node links, search operators, stars; drag-to-move, mouse selection, drop-to-import, jump palette, history; scheduled export, capture window | Every *Must* row of the parity checklist works |
| **3 · Knowledge and web extras** | Backlinks, link previews, saved searches, templates; image attachments, `hyphal://` links, print and PDF, theme picker | Backlinks list every linking bullet and page; a saved search reopens with the same results; a template instantiates with new IDs; a pasted image survives export and restore |
| **4 · Agent integration** | Batch `apply`, `forget`, agent usage guide, richer queries | An agent completes a scripted multi-step task with `apply` and the usage guide, and one `hyphal revert` undoes it as a unit |
| **5 · Multi-device sync** | Device IDs, HLC merge, per-device logs, compaction; any-folder sync (iCloud Drive, Syncthing) | Two machines edit offline and converge with nothing lost |

Each phase starts only after the previous gate passes.

**Separate follow-up project:** an iPhone app that syncs through the same per-device log format. It is out of scope here; this project only guarantees the format is documented and stable enough to build it.

Future and exploratory, with no phase: reaching this same web UI from a phone over a private network such as Tailscale, which would reverse the loopback non-goal and need real authentication; direct Wi-Fi sync, collaboration, graph view, plugins.

## 12. Non-goals, decisions and next steps

### 12.1 Non-goals

- No editing of the data outside the app. Exports are copies, and all editing happens in the app, or through the CLI for scripts and agents.
- No live sync through Git, Syncthing or cloud folders.
- No `@person` mentions and no board (kanban) view in V1.
- No mirrors (one bullet shown live in two places). Every node has exactly one parent; links, tags and saved searches cover the need.
- No daily journal or Today node, and no page-based model: everything lives in the one tree, and a page (§5.7) is an optional attachment to a bullet.
- No Vim-style modal editing or Vim motions.
- No terminal UI. Versions up to v3 specified one; the CLI remains.
- No accounts, subscription, cloud backend or database server.
- No rich-text or WYSIWYG editing; formatting is Markdown-style markup, rendered with concealment.
- No real-time multi-writer collaboration.
- No full Obsidian replacement, graph view or plugin system in V1.
- No Windows support.
- No hosted version, remote access or phone access to the local server. The server is loopback only.
- No iPhone or Android app in this project; a phone app is a separate follow-up (§11).

### 12.2 Resolved decisions

| # | Decision | Resolution | Section |
| --- | --- | --- | --- |
| 1 | Product Goal | Workflowy clone first; Obsidian-inspired features as the bonus | §1, §3 |
| 2 | Source of truth | One SQLite file; exports and snapshots are copies | §7 |
| 3 | Node identity | Random, permanent IDs in the database | §7.2 |
| 4 | Sibling order | Fractional index positions | §7.2 |
| 5 | History | Change sets with before/after operations, coalesced typing | §8.2 |
| 6 | Deletion | Soft delete into a Trash, purged after 30 days | §8.3 |
| 7 | Backup | Daily and pre-migration snapshots, last 14 kept; export on demand or on schedule | §8.5, §7.5 |
| 8 | Editor ergonomics | Modeless + Esc block selection; Enter/Backspace tables | §5 |
| 9 | Formatting | Markdown-style markers, concealed when not editing | §5.8 |
| 10 | Text color | `<red>text</red>` with theme-mapped color names; `==highlight==` | §5.8 |
| 11 | Tags | `#tag` with nested `/` tags; no `@person` | §5.9 |
| 12 | Theme | CSS custom-property tokens in cascade layers; a theme is one CSS file; light DOM; Omarchy's `colors.toml` followed live when present | §4.4 |
| 13 | Tasks | `is_task` plus `completed_at`; exported as checkboxes | §7.2 |
| 14 | Search | In-memory substring scan, no index; filter in P1, jump palette and operators in P2 | §6.2 |
| 15 | External editing | None; cut in row 58 | §12.2 |
| 16 | Configuration | XDG paths, TOML, explicit `--db` | §7.1 |
| 17 | Long-form content | Bullets stay one line; an optional Markdown page per bullet, opened with page zoom. Replaces notes | §5.7 |
| 18 | Language and UI stack | Go server with native web components, HTML, CSS and JS; no frontend libraries without approval; no build step; pure-Go SQLite; CodeMirror approved for pages (row 59) | §9.1 |
| 19 | Platforms | Linux (Omarchy first) and macOS; no Windows | §6.4, §7.1 |
| 20 | Sync | Nothing sync-specific before Phase 5; then per-device logs in a shared folder, no server | §7.6 |
| 21 | CLI | Full CLI with JSON output in Phase 1; batch `apply` and `forget` in Phase 4; no MCP server | §10.2 |
| 22 | Builders | Built entirely by AI agents from a phased plan; every behavior has an automated test | §9.6 |
| 23 | App name | Only in user-facing places; never in schema, ops, sync log or JSON fields | §1 |
| 24 | Root and Inbox | A fixed Home root; a top-level Inbox found by ID | §4.1, §6.3 |
| 25 | Keymap | One authoritative table per context; every everyday bullet command has a block-selection key, rare ones are palette-only; no browser-reserved keys; `Ctrl` means Cmd on macOS, native text keys stay native; Esc never zooms out; typing never leaves block selection | §6.4 |
| 26 | Overlays | Backlinks and palettes are floating overlays; no side panes | §4.7 |
| 27 | Undo scope | Per-session stack of UI-started changes; CLI and agent changes are reverted | §5.6 |
| 28 | Identity | Split keeps the ID on the first half; merges keep the earlier bullet and retarget stars and links | §5.2–5.3a |
| 29 | Concurrency | Per-node versions; UI typing is never dropped; `--if-version` for agents | §7.3 |
| 30 | Trash restore | Per deletion, to the original position, else the nearest surviving ancestor | §8.3 |
| 31 | History policy | Append-only, ops written once when sealed; no compaction; manual purge | §8.2, §8.4 |
| 32 | Sync moves | Moves carry parent and position together; a proven tree-move algorithm in Phase 5 | §7.6 |
| 33 | Markdown | One GFM-style dialect for export, import and paste | §7.5 |
| 34 | CLI shape | Core verbs top-level, the rest grouped by noun; `--json` envelope and fixed exit codes | §10.2 |
| 35 | Look | Comfortable density; semantic color tokens. Threads replaced indent guides (row 57) | §4.6 |
| 36 | Mirrors | Never; the outline stays a strict tree, and links, tags and saved searches cover multi-context items | §12.1 |
| 37 | Cut and paste | Pasting the app's own last cut moves the bullets intact (IDs, pages, history) | §5.5 |
| 38 | Page preview | None in the outline; zoom into the page to read it | §5.7 |
| 39 | Trash contents | Merge leftovers and empty leaves are hidden from the Trash view | §8.3 |
| 40 | Block-selection keys | Every everyday bullet command has one single key, no aliases: `[`/`]` move, `D` duplicate, `h` show/hide completed, `0` expand all, `~` zoom to root | §6.4 |
| 41 | Markdown export layout | One `outline.md` plus `pages/` by default; `--split` for one file per top-level node | §7.5 |
| 42 | Feedback | No toasts: one footer row shows a message (until the next key), the filter query, or key hints | §4.5 |
| 43 | Leading markers | `[ ] `, `[x] ` and `## ` at the start of a bullet always convert, so the dialect needs no escapes | §5.4, §7.5 |
| 44 | Clipboard | Async Clipboard API for copy; the `paste` event for paste, so the app never reads the clipboard on its own | §5.5 |
| 45 | Name | Hyphal; `hyphal` for the binary, directories, window class and `HYPHAL_DB` | §1 |
| 46 | Simplification pass | Cut: sync groundwork before Phase 5, Omarchy theme files, Nerd Font glyphs, history compaction, hourly snapshots, properties, saved-query virtual children, MCP server, OPML formatting export, desktop notifications, persistent undo, FTS5, `node_tags`, in-place history rows, incremental reload | §4–§11 |
| 47 | Chords | Text-editing chords limited to the Workflowy core; native text keys stay native on each platform | §6.4 |
| 48 | Platform | Local web app: one Go binary serving the UI on loopback, replacing the terminal UI | §1, §9 |
| 49 | Ownership | Go owns every rule and the undo stack; the browser owns DOM, focus, caret and typing in the focused bullet | §9.3 |
| 50 | Tabs | One server per database; any window can edit, each with its own zoom and cursor; losing focus seals typing (row 60) | §8.6 |
| 51 | Local security | Loopback only, `Host` and `Origin` checks, single-use launch ticket, `HttpOnly` cookie, strict CSP, raw HTML in pages stays literal | §9.3 |
| 52 | Zoom addresses | Each zoom has a URL; browser Back and Forward move through zooms | §4.1 |
| 53 | Page editor | Built-in editor moves to Phase 1, since the browser makes it cheap; superseded by row 59 | §5.7 |
| 54 | Mouse | Clicks and scrolling move to Phase 1 | §6.5 |
| 56 | Drag-to-move | Native drag and drop in Phase 2, with every move still available from the keyboard | §6.5 |
| 57 | Design style | Mycelium colors, Literata and Fraunces, hand-drawn thread icons, ruled threads with a lit focus path, Lit strands collapsed bullets, spore dots for expanded and leaf bullets, optional glowing thread, header and focused-row icons | §4.3, §4.6 |
| 58 | External editor | Cut the `$EDITOR` round trip: all editing happens in the app. Edit settings uses the built-in plain-text editor | §4.7, §5.7 |
| 59 | Pages | Obsidian-style live preview on CodeMirror 6 replaces view and edit modes, position sync and split view. goldmark and chroma are gone. Proven by spike P0-W2 | §5.7, §9.1 |
| 60 | Web review | Stable port per database; any window edits; page address, find bar and separate title field; pages send text on a short delay; CodeMirror loads on demand; no images in Phase 1; `column_width`, `footer`, `[tag_colors]` and the terminal capture prompt cut; 80-character column | §4–§9, §13 |
| 61 | Web additions | Phase 1: zoom animation, clickable Undo, paste URL as link, rich copy, `hyphal open`. Phase 2: mouse selection, drop-to-import. Phase 3: link previews, attachments, `hyphal://`, print and PDF, theme picker | §4–§11 |
| 55 | Cut features | Glyph ASCII fallbacks, terminal matrix, kitty, tmux, OSC sequences, Bubble Tea stack, wrap-layout engine, `zoom_transition`, `glyphs`, `background`, `mouse` settings | §4, §6.4, §13 |

### 12.3 Still open

**To settle before implementation resumes:**

- **Spike checks not yet run by hand.** The real key matrix and real IME use (`plan/reports/p0-w1-web-ui.md`). The Phase 0 human cards cover them.

**Other open items:**

- **Color palette size.** Eight named colors is the starting set; confirm each stays legible on light and dark themes, including Omarchy's.
- **History growth.** History is kept in full; measure real growth in Phase 0 and add a retention default only if it is needed.
- **Sync details** (Phase 5, §7.6): HLC drift handling, log compaction thresholds, and whether page text later merges line by line.

**Open questions by phase.** Each must be settled before its phase starts, as part of that phase's plan.

- **Phase 2**
  - *Tags (§5.9):* how `#` is told apart from Markdown headings in pages, issue numbers (`#123`), `C#`, URL fragments and code; Unicode letters; numeric-only tags; a trailing `/`; display case versus stored lowercase; a rename command; colors repeating with only eight palette colors.
  - *Node links (§10.1):* the stored form of `[[…]]` and how it stays readable in a SQLite browser; Obsidian export (links target files, not nodes); links to trashed or purged nodes; the format of Copy link to node.
  - *Screens:* History view, Stars view, tag list, jump palette result rows.
- **Phase 3**
  - *Templates:* stripping `#template` from instances; variables such as the date.
- **Phase 5**
  - *Sync view* layout and keys.

### 12.4 Immediate next steps

1. **Spec and plan reviewed, design talk held** (2026-10-31; §12.2 row 57).
2. **Finish the storage foundations:** schema, commit with history, typing coalescing, the JSON round trip and crash injection (Phase 0 cards).
3. **Web skeleton:** server, protocol and the outline web component, replacing `internal/webspike`.
4. **Reference screens:** the five screens listed in §4.6, reviewed side by side with Workflowy under three themes, one of them light.
5. **Key matrix:** record which bindings arrive in each browser and window type (§6.4).
6. **Workflowy import:** run a real Workflowy OPML export through the importer and list anything lost, including how mirrors appear and whether they can be detected.

## 13. Configuration reference

`config.toml` (§7.1) is optional; every key has a default. "Edit settings" opens it commented, listing every key below (§4.7).

| Key | Default | Meaning | Section |
| --- | --- | --- | --- |
| `db` | `<data dir>/outline.db` | Default database | §7.1 |
| `theme` | `"auto"` | `auto` (Omarchy if present, else follows the system), `light`, `dark`, or a file name in `themes/` | §4.4 |
| `browser` | `"auto"` | `auto` (app window on a Chromium-family browser, else the default browser), or a command template with `{url}` | §4.7 |
| `density` | `"comfortable"` | `comfortable` or `compact` | §4.6 |
| `thread_glow` | `false` | Soft glow on the lit thread path, with a brief pulse when focus moves | §4.3 |
| `[keys.<context>]` | — | Key overrides per context | §6.4 |
| `trash_days` | `30` | Days before the Trash purges | §8.3 |
| `snapshots.keep` | `14` | Daily snapshots kept | §8.5 |
| `export.format` | `"none"` | Scheduled export: `none`, `markdown` or `json` | §7.5 |
| `export.when` | `"daily"` | `daily` or `quit` | §7.5 |
| `export.dir` | `<data dir>/exports` | Scheduled export folder | §7.5 |
| `export.keep` | `10` | Scheduled exports kept | §7.5 |

- **Invalid config:** at launch, invalid keys fall back to their defaults and a banner names the file and line. On reload after "Edit settings", an invalid file is ignored entirely: the previous config stays in effect, with the same banner. Unknown keys produce a warning, not an error.
