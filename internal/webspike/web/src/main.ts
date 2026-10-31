// P0-W1 browser client. Go owns structure rules; the browser owns typing inside
// the focused bullet. Structural keys pressed while a reply is in flight are
// queued, so fast typing can't land on the wrong row.

interface Row {
  id: string; text: string; depth: number; kids: boolean; collapsed: boolean;
  done: boolean; task: boolean; heading: boolean; page: boolean;
}
interface State {
  t: "state"; seq: number; total: number; start: number; rows: Row[]; cursor: string; off: number;
  block: string[] | null; crumbs: string[]; filter: string; msg: string; readOnly: boolean; textAck: number;
}
interface QueuedKey { key: string; ctrl: boolean; shift: boolean; alt: boolean; }

const $ = (id: string) => document.getElementById(id)!;
const scroller = $("scroller"), rowsEl = $("rows"), topEl = $("top"), bottomEl = $("bottom");
const footer = $("footer"), crumbs = $("crumbs"), banner = $("banner"), filterbar = $("filterbar");
const filterInput = $("filter") as HTMLInputElement;

const WINDOW = 200;
let ws: WebSocket;
let state: State | null = null;
let seq = 0;
let inflight = 0;
const queue: QueuedKey[] = [];
let composing = false;
let deferred: State | null = null;
let goalX: number | null = null;
let avgRow = 24;
let localMsg = "";
let scrollQueued = false;
let textSent = 0;
let lastText: { id: string; text: string } | null = null;
const rowEls = new Map<string, HTMLElement>();

const send = (m: object) => ws.send(JSON.stringify(m));
function act(name: string, extra: object = {}) {
  inflight++;
  send({ t: "action", name, seq: ++seq, ...extra });
}
const inBlock = () => !!state?.block?.length;
const host = () => rowEls.get(state?.cursor ?? "")?.querySelector<HTMLElement>(".text") ?? null;

// Selection, in UTF-16 offsets within one host.

function offsetsIn(el: HTMLElement): { off: number; end: number } | null {
  const sel = getSelection();
  if (!sel || !sel.anchorNode || !el.contains(sel.anchorNode) || !el.contains(sel.focusNode)) return null;
  const at = (node: Node, offset: number) => {
    const r = document.createRange();
    r.setStart(el, 0);
    r.setEnd(node, offset);
    return r.toString().length;
  };
  const a = at(sel.anchorNode, sel.anchorOffset), f = at(sel.focusNode!, sel.focusOffset);
  return { off: Math.min(a, f), end: Math.max(a, f) };
}

function setCaret(el: HTMLElement, off: number) {
  const sel = getSelection()!;
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  let left = off;
  let node = walker.nextNode() as Text | null;
  const r = document.createRange();
  if (!node) {
    r.setStart(el, 0);
  } else {
    while (left > node.length) {
      const next = walker.nextNode() as Text | null;
      if (!next) { left = node.length; break; }
      left -= node.length;
      node = next;
    }
    r.setStart(node, left);
  }
  r.collapse(true);
  sel.removeAllRanges();
  sel.addRange(r);
}

function caretRect(el: HTMLElement): DOMRect {
  const sel = getSelection()!;
  if (sel.rangeCount && el.textContent) {
    const r = sel.getRangeAt(0).cloneRange();
    r.collapse(sel.focusNode === r.endContainer && sel.focusOffset === r.endOffset ? false : true);
    const rects = r.getClientRects();
    if (rects.length) return rects[rects.length - 1];
    const o = offsetsIn(el);
    if (o) {
      const c = document.createRange();
      const t = el.firstChild as Text;
      if (t && o.off > 0) {
        c.setStart(t, o.off - 1); c.setEnd(t, o.off);
        const w = c.getBoundingClientRect();
        return new DOMRect(w.right, w.top, 0, w.height);
      }
      if (t) {
        c.setStart(t, 0); c.setEnd(t, Math.min(1, t.length));
        const w = c.getBoundingClientRect();
        return new DOMRect(w.left, w.top, 0, w.height);
      }
    }
  }
  const b = el.getBoundingClientRect();
  return new DOMRect(b.left, b.top, 0, parseFloat(getComputedStyle(el).lineHeight) || 20);
}

function offsetAtPoint(el: HTMLElement, x: number, y: number): number {
  const b = el.getBoundingClientRect();
  const cx = Math.max(b.left + 1, Math.min(x, b.right - 1));
  let node: Node | null = null, off = 0;
  const d = document as any;
  if (d.caretPositionFromPoint) {
    const p = d.caretPositionFromPoint(cx, y);
    if (p) { node = p.offsetNode; off = p.offset; }
  } else if (d.caretRangeFromPoint) {
    const r = d.caretRangeFromPoint(cx, y);
    if (r) { node = r.startContainer; off = r.startOffset; }
  }
  if (!node || !el.contains(node)) return y < b.top + b.height / 2 ? 0 : (el.textContent ?? "").length;
  const r = document.createRange();
  r.setStart(el, 0);
  r.setEnd(node, off);
  return r.toString().length;
}

// Rendering.

function buildRow(): HTMLElement {
  const el = document.createElement("div");
  el.className = "row";
  el.innerHTML = '<span class="glyph"></span><span class="box" hidden></span><div class="text"></div><span class="mark" hidden>≡</span>';
  return el;
}

function updateRow(el: HTMLElement, r: Row, focused: boolean, selected: boolean, editable: boolean) {
  el.dataset.id = r.id;
  el.style.paddingLeft = Math.max(r.depth, 0) * 2 + "ch";
  el.className = "row" + (r.done ? " done" : "") + (r.heading ? " heading" : "") + (r.depth < 0 ? " zoomhead" : "") +
    (focused ? " focus" : "") + (selected ? " block" : "");
  const [glyph, box, text, mark] = Array.from(el.children) as HTMLElement[];
  glyph.textContent = r.kids ? (r.collapsed ? "▸" : "▾") : "•";
  box.hidden = !r.task;
  box.textContent = r.done ? "☑" : "☐";
  mark.hidden = !r.page;
  if (text.textContent !== r.text && !(composing && focused)) text.textContent = r.text;
  const want = focused && editable;
  if (want !== text.isContentEditable) {
    if (want) {
      try { text.contentEditable = "plaintext-only"; } catch { text.contentEditable = "true"; }
      text.spellcheck = false;
    } else {
      text.removeAttribute("contenteditable");
    }
  }
}

function render(s: State, isReply: boolean) {
  const prev = state;
  // A state built before the server applied our latest typing must not undo it.
  if (lastText && s.textAck < textSent) {
    const mine = s.rows.find((r) => r.id === lastText!.id);
    if (mine) mine.text = lastText.text;
  }
  state = s;
  const keep = new Set<string>();
  const block = new Set(s.block ?? []);
  const editable = !s.readOnly && block.size === 0;
  let child = rowsEl.firstElementChild;
  for (const r of s.rows) {
    keep.add(r.id);
    let el = rowEls.get(r.id);
    if (!el) { el = buildRow(); rowEls.set(r.id, el); }
    updateRow(el, r, r.id === s.cursor, block.has(r.id), editable);
    if (el === child) {
      child = child.nextElementSibling;
    } else {
      rowsEl.insertBefore(el, child);
    }
  }
  while (child) {
    const next = child.nextElementSibling;
    rowEls.delete((child as HTMLElement).dataset.id!);
    child.remove();
    child = next;
  }
  for (const [id, el] of rowEls) if (!keep.has(id)) { el.remove(); rowEls.delete(id); }

  if (s.rows.length) {
    avgRow = 0.9 * avgRow + 0.1 * (rowsEl.getBoundingClientRect().height / s.rows.length);
  }
  topEl.style.height = s.start * avgRow + "px";
  bottomEl.style.height = Math.max(0, s.total - s.start - s.rows.length) * avgRow + "px";

  crumbs.textContent = s.crumbs.join(" › ") + (s.filter ? `   / ${s.filter} · ${s.total} rows` : "");
  banner.hidden = !s.readOnly;
  banner.textContent = s.readOnly ? "Read-only: this outline is open for editing in another tab" : "";
  localMsg = s.msg || (isReply ? "" : localMsg);
  showFooter();

  if (document.activeElement === filterInput) return;
  const h = host();
  if (block.size) {
    if (document.activeElement !== scroller) scroller.focus({ preventScroll: true });
    getSelection()?.removeAllRanges();
    if (isReply) ensureVisible();
  } else if (h && editable) {
    if (isReply || document.activeElement !== h || prev?.cursor !== s.cursor) {
      if (document.activeElement !== h) h.focus({ preventScroll: true });
      if (isReply || prev?.cursor !== s.cursor) setCaret(h, s.off);
      if (isReply) ensureVisible();
    }
  }
}

function ensureVisible() {
  const el = rowEls.get(state?.cursor ?? "");
  if (!el) return;
  const b = el.getBoundingClientRect(), v = scroller.getBoundingClientRect();
  const margin = 3 * 24;
  if (b.top < v.top + margin) scroller.scrollTop -= v.top + margin - b.top;
  else if (b.bottom > v.bottom - margin) scroller.scrollTop += b.bottom - (v.bottom - margin);
}

function showFooter() {
  footer.textContent = localMsg ||
    (inBlock() ? "x done · d delete · z zoom · Enter edit · ? help" : "Enter new · Tab indent · Esc select · Ctrl+F filter");
}

scroller.addEventListener("scroll", () => {
  if (scrollQueued || !state) return;
  scrollQueued = true;
  requestAnimationFrame(() => {
    scrollQueued = false;
    if (!state) return;
    const first = Math.floor(scroller.scrollTop / avgRow);
    const vis = Math.ceil(scroller.clientHeight / avgRow);
    const end = state.start + state.rows.length;
    if ((first < state.start + 20 && state.start > 0) || (first + vis > end - 20 && end < state.total)) {
      send({ t: "window", start: Math.max(0, first - WINDOW / 2 + vis), count: WINDOW, seq: 0 });
    }
  });
});

// Text keys.

function pushText() {
  const h = host();
  if (!h || !state) return;
  const text = (h.textContent ?? "").replace(/\n/g, "");
  const o = offsetsIn(h);
  lastText = { id: state.cursor, text };
  send({ t: "text", id: state.cursor, seq: ++textSent, text, off: o?.off ?? text.length });
  const row = state!.rows.find((r) => r.id === state!.cursor);
  if (row) row.text = text;
}

function verticalMove(dir: number) {
  const h = host();
  if (!h || !state) return;
  const rect = caretRect(h);
  const x = goalX ?? rect.left;
  goalX = x;
  const lh = rect.height || 20;
  const y = dir < 0 ? rect.top - lh / 2 : rect.bottom + lh / 2;
  const b = h.getBoundingClientRect();
  if (y > b.top && y < b.bottom) {
    setCaret(h, offsetAtPoint(h, x, y));
    return;
  }
  const i = state!.rows.findIndex((r) => r.id === state!.cursor) + dir;
  const next = state.rows[i];
  if (!next) {
    localMsg = "";
    return;
  }
  const el = rowEls.get(next.id)!.querySelector<HTMLElement>(".text")!;
  el.scrollIntoView({ block: "nearest" });
  const tb = el.getBoundingClientRect();
  const ty = dir < 0 ? tb.bottom - lh / 2 : tb.top + lh / 2;
  act("setCursor", { id: next.id, off: offsetAtPoint(el, x, ty) });
}

function horizontalEdge(dir: number): boolean {
  const h = host();
  if (!h || !state) return false;
  const o = offsetsIn(h);
  if (!o || o.off !== o.end) return false;
  const len = (h.textContent ?? "").length;
  if ((dir < 0 && o.off > 0) || (dir > 0 && o.off < len)) return false;
  const next = state!.rows[state!.rows.findIndex((r) => r.id === state!.cursor) + dir];
  if (!next) return true;
  act("setCursor", { id: next.id, off: dir < 0 ? next.text.length : 0 });
  return true;
}

function lineEdge(dir: "backward" | "forward") {
  const h = host();
  if (!h) return;
  const sel = getSelection() as any;
  const before = offsetsIn(h)?.off;
  sel.modify("move", dir, "lineboundary");
  if (offsetsIn(h)?.off === before) setCaret(h, dir === "backward" ? 0 : (h.textContent ?? "").length);
}

function deleteBack(unit: "word" | "lineboundary") {
  const sel = getSelection() as any;
  if (sel.isCollapsed) sel.modify("extend", "backward", unit);
  document.execCommand("delete");
}

function openFilter() {
  filterbar.hidden = false;
  filterInput.value = state?.filter ?? "";
  filterInput.focus();
}

// Returns true when the key was consumed.
function textKey(k: QueuedKey): boolean {
  const h = host();
  const o = h ? offsetsIn(h) : null;
  if (k.key !== "ArrowUp" && k.key !== "ArrowDown") goalX = null;
  if (k.ctrl && !k.alt) {
    switch (k.key.toLowerCase()) {
      case "z": act(k.shift ? "redo" : "undo"); return true;
      case "y": act("redo"); return true;
      case "f": openFilter(); return true;
      case "enter": act("done"); return true;
      case "s": act("hint", { text: "Saved automatically" }); return true;
      case "q": act("hint", { text: "Quit" }); return true;
      case "p": act("hint", { text: "Command palette" }); return true;
      case "k": act("hint", { text: "Jump palette" }); return true;
      case "a": lineEdge("backward"); return true;
      case "e": lineEdge("forward"); return true;
      case "u": deleteBack("lineboundary"); return true;
      case "w": deleteBack("word"); return true;
      case "backspace": deleteBack("word"); return true;
      case "arrowup": if (k.shift) { act("move", { dir: -1 }); return true; } return false;
      case "arrowdown": if (k.shift) { act("move", { dir: 1 }); return true; } return false;
      case "home": if (state?.rows[0]) act("setCursor", { id: state.rows[0].id, off: 0 }); return true;
      case "end": { const r = state?.rows.at(-1); if (r) act("setCursor", { id: r.id, off: r.text.length }); return true; }
    }
    return false;
  }
  if (k.alt) {
    if (k.key === "b" || k.key === "f") {
      (getSelection() as any).modify("move", k.key === "b" ? "backward" : "forward", "word");
      return true;
    }
    if (k.key === "Backspace") { deleteBack("word"); return true; }
    return false;
  }
  switch (k.key) {
    case "Enter": act("enter", { off: o?.off ?? 0, end: o?.end ?? 0 }); return true;
    case "Tab": act(k.shift ? "outdent" : "indent"); return true;
    case "Escape": act("blockEnter"); return true;
    case "F1": act("hint", { text: "Help" }); return true;
    case "Backspace":
      if (o && o.off === 0 && o.end === 0) { act("backspace"); return true; }
      return false;
    case "Delete":
      if (o && o.off === o.end && o.off === (h?.textContent ?? "").length) { act("delete"); return true; }
      return false;
    case "ArrowUp": case "ArrowDown":
      if (k.shift) return false;
      verticalMove(k.key === "ArrowUp" ? -1 : 1);
      return true;
    case "ArrowLeft": case "ArrowRight":
      return !k.shift && horizontalEdge(k.key === "ArrowLeft" ? -1 : 1);
    case "Home": if (k.shift) return false; lineEdge("backward"); return true;
    case "End": if (k.shift) return false; lineEdge("forward"); return true;
  }
  return false;
}

function blockKey(k: QueuedKey): boolean {
  if (k.ctrl) {
    switch (k.key.toLowerCase()) {
      case "z": act(k.shift ? "redo" : "undo"); return true;
      case "y": act("redo"); return true;
      case "c": act("copy"); act("hint", { text: "Copied as Markdown" }); return true;
      case "x": act("copy"); act("blockDelete"); return true;
      case "arrowup": if (k.shift) { act("move", { dir: -1 }); return true; } return false;
      case "arrowdown": if (k.shift) { act("move", { dir: 1 }); return true; } return false;
    }
    return false;
  }
  switch (k.key) {
    case "ArrowUp": act(k.shift ? "blockExtend" : "blockMove", { dir: -1 }); return true;
    case "ArrowDown": act(k.shift ? "blockExtend" : "blockMove", { dir: 1 }); return true;
    case "Enter": act("blockExit"); return true;
    case "Tab": act(k.shift ? "outdent" : "indent"); return true;
    case "Escape":
      if (state?.filter) { closeFilter(); return true; }
      act("hint", { text: "Nothing to back out of. Enter returns to typing" }); return true;
    case "-": act("collapse"); return true;
    case "=": act("expand"); return true;
    case "[": act("move", { dir: -1 }); return true;
    case "]": act("move", { dir: 1 }); return true;
    case "z": act("zoomIn"); return true;
    case "Z": act("zoomOut"); return true;
    case "~": act("zoomRoot"); return true;
    case "x": act("done"); return true;
    case "t": act("task"); return true;
    case "#": act("heading"); return true;
    case "d": case "Delete": act("blockDelete"); return true;
    case "e": act("editor"); return true;
    case "/": openFilter(); return true;
    case "?": case "F1": act("hint", { text: "Help" }); return true;
    case "Backspace": act("hint", { text: "Delete or d deletes" }); return true;
  }
  if (k.key.length === 1) { act("hint", { text: `${k.key} does nothing here` }); return true; }
  return false;
}

function closeFilter() {
  filterbar.hidden = true;
  filterInput.value = "";
  act("filter", { text: "" });
}

function runKey(k: QueuedKey): boolean {
  if (!state) return true;
  if (!rowEls.has(state.cursor)) {
    act("setCursor", { id: state.cursor, off: state.off });
    queue.unshift(k);
    return true;
  }
  return inBlock() ? blockKey(k) : textKey(k);
}

function drain() {
  while (inflight === 0 && queue.length) {
    const k = queue.shift()!;
    if (runKey(k)) continue;
    if (k.key.length === 1 && !k.ctrl && !k.alt) document.execCommand("insertText", false, k.key);
  }
}

document.addEventListener("keydown", (e) => {
  localMsg = "";
  if (e.isComposing || e.keyCode === 229 || composing) return;
  if (document.activeElement === filterInput) return;
  if (state?.readOnly) {
    if (!["Control", "Shift", "Alt", "Meta", "ArrowUp", "ArrowDown", "PageUp", "PageDown"].includes(e.key)) {
      e.preventDefault();
      footer.textContent = "Read-only: this outline is open for editing in another tab";
    }
    return;
  }
  if (["Control", "Shift", "Alt", "Meta", "PageUp", "PageDown"].includes(e.key)) return;
  const k: QueuedKey = { key: e.key, ctrl: e.ctrlKey, shift: e.shiftKey, alt: e.altKey };
  if (inflight > 0 || queue.length) {
    e.preventDefault();
    queue.push(k);
    return;
  }
  if (runKey(k)) e.preventDefault();
});

document.addEventListener("beforeinput", (e) => {
  if (e.inputType === "historyUndo" || e.inputType === "historyRedo") {
    e.preventDefault();
    act(e.inputType === "historyUndo" ? "undo" : "redo");
  }
});

rowsEl.addEventListener("input", () => { if (!composing) pushText(); });
rowsEl.addEventListener("compositionstart", () => { composing = true; });
rowsEl.addEventListener("compositionend", () => {
  composing = false;
  pushText();
  if (deferred) { const d = deferred; deferred = null; render(d, false); }
  drain();
});
rowsEl.addEventListener("paste", (e) => {
  e.preventDefault();
  const t = (e as ClipboardEvent).clipboardData?.getData("text/plain") ?? "";
  document.execCommand("insertText", false, t.replace(/\s*\n\s*/g, " "));
});
rowsEl.addEventListener("drop", (e) => e.preventDefault());

rowsEl.addEventListener("mousedown", (e) => {
  const target = e.target as HTMLElement;
  const row = target.closest<HTMLElement>(".row");
  if (!row || !state || state.readOnly) return;
  const id = row.dataset.id!;
  goalX = null;
  if (target.classList.contains("glyph")) {
    e.preventDefault();
    const r = state.rows.find((x) => x.id === id);
    act("setCursor", { id, off: 0 });
    if (r?.kids) act(r.collapsed ? "expand" : "collapse");
    return;
  }
  if (id === state.cursor && !inBlock()) return;
  e.preventDefault();
  const text = row.querySelector<HTMLElement>(".text")!;
  act("setCursor", { id, off: offsetAtPoint(text, e.clientX, e.clientY) });
});

filterInput.addEventListener("input", () => act("filter", { text: filterInput.value }));
filterInput.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    closeFilter();
    scroller.focus();
    host()?.focus();
  } else if (e.key === "Enter" || e.key === "ArrowDown") {
    e.preventDefault();
    const r = state?.rows.find((x) => x.depth >= 0);
    filterInput.blur();
    if (r) act("setCursor", { id: r.id, off: 0 });
  }
});

// Theme: colors.toml roles become CSS custom properties (AC7).

function applyTheme(c: Record<string, string>) {
  const root = document.documentElement.style;
  const map: Record<string, string> = { background: "--bg", foreground: "--fg", accent: "--accent", muted: "--muted", selection: "--sel" };
  for (const [k, v] of Object.entries(map)) if (c[k]) root.setProperty(v, c[k]);
  if (c.mode) root.setProperty("color-scheme", c.mode);
}

function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.t === "theme") {
      applyTheme(m.colors);
    } else if (m.t === "clip") {
      navigator.clipboard.writeText(m.text).catch((err) => { localMsg = `Clipboard failed: ${err}`; showFooter(); });
    } else if (m.t === "state") {
      const reply = m.seq > 0;
      if (reply) inflight--;
      if (composing) { deferred = m; return; }
      render(m, reply);
      if (reply) drain();
    }
  };
  ws.onclose = () => {
    banner.hidden = false;
    banner.textContent = "The local server stopped";
  };
}

(window as any).__spike = {
  outline: () => state?.rows.map((r) => {
    const flags = (r.done ? " [x]" : "") + (r.collapsed ? " [c]" : "");
    return r.depth < 0 ? `# ${r.text}` : "  ".repeat(r.depth) + r.text + flags;
  }).join("\n") ?? "",
  state: () => state,
  caret: () => { const h = host(); return h ? offsetsIn(h) : null; },
  idle: () => inflight === 0 && queue.length === 0,
  setCursor: (id: string, off: number) => act("setCursor", { id, off }),
};

connect();
