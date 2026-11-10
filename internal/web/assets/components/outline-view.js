// <hyphal-outline>: the row window and the focused bullet (§4.3, §9.3).
// The server owns every outline rule; this element owns the DOM, focus, the
// caret, scrolling and typing inside the focused bullet. Structural keys wait
// for the server's reply, and keys pressed meanwhile queue and replay in order.

import { caretRect, offsetAtPoint, offsetsIn, rangeOf, setCaret } from "../lib/selection.js";
import { strings } from "../lib/strings.js";

/** Name of the marker highlight that `::highlight()` styles (D-042). */
export const MARKER_HIGHLIGHT = "hyphal-marker";

const WINDOW = 200; // rows the server sends around the viewport
const EDGE = 20; // ask for more rows this far from the window's edge
const SCROLL_MARGIN_ROWS = 3; // §4.6
const MODIFIERS = new Set(["Control", "Shift", "Alt", "AltGraph", "Meta", "CapsLock", "Fn", "OS"]);

/**
 * Maps an offset in a row's concealed text to its source text (A-013).
 * @param {any} row
 * @param {number} p
 * @returns {number}
 */
export function toSource(row, p) {
  const runs = row.runs.filter((r) => r.text.length > 0);
  let at = 0;
  for (let i = 0; i < runs.length; i++) {
    const len = runs[i].text.length;
    if (p < at + len || (p === at + len && i === runs.length - 1)) return runs[i].src[0] + (p - at);
    at += len;
  }
  return row.text.length;
}

/** @param {string} key */
const printable = (key) => [...key].length === 1;

export class OutlineView extends HTMLElement {
  constructor() {
    super();
    /** @type {import("../lib/protocol.js").Connection | null} */
    this.conn = null;
    this.state = null;
    /** @type {Map<string, HTMLElement>} */
    this.rowEls = new Map();
    this.queue = [];
    this.composing = false;
    this.deferred = null;
    this.goalX = null;
    this.avgRow = 24;
    this.scrollQueued = false;
    this.lastText = null; // the latest typing sent: {id, text, seq}
  }

  connectedCallback() {
    if (this.rowsEl) return;
    this.setAttribute("role", "tree");
    this.setAttribute("aria-label", strings.outline);
    this.topEl = document.createElement("div");
    this.rowsEl = document.createElement("div");
    this.rowsEl.className = "rows";
    this.bottomEl = document.createElement("div");
    this.append(this.topEl, this.rowsEl, this.bottomEl);

    this.addEventListener("scroll", () => this.onScroll());
    this.addEventListener("mousedown", (e) => this.onMouseDown(e));
    this.rowsEl.addEventListener("input", () => {
      if (!this.composing) this.pushText();
    });
    this.rowsEl.addEventListener("compositionstart", () => {
      this.composing = true;
    });
    this.rowsEl.addEventListener("compositionend", () => {
      this.composing = false;
      this.pushText();
      if (this.deferred) {
        const d = this.deferred;
        this.deferred = null;
        this.render(d.state, d.reply);
      }
      this.drain();
    });
    this.rowsEl.addEventListener("paste", (e) => {
      // Plain text only, on one line (D-020). TODO(P1-41): multi-line paste.
      e.preventDefault();
      const t = e.clipboardData?.getData("text/plain") ?? "";
      document.execCommand("insertText", false, t.replace(/\s*[\r\n]+\s*/g, " "));
    });
    this.rowsEl.addEventListener("drop", (e) => e.preventDefault());
    document.addEventListener("keydown", (e) => this.onKeyDown(e));
    document.addEventListener("beforeinput", (e) => {
      // The browser's own undo would fork the text from the server's (§5.6).
      if (e.inputType === "historyUndo" || e.inputType === "historyRedo") e.preventDefault(); // TODO(P1-37)
    });
  }

  /** The focused bullet's editable element, if it is in the DOM. */
  host() {
    return this.rowEls.get(this.state?.cursor ?? "")?.querySelector(".text") ?? null;
  }

  /**
   * Takes a state from the server. During composition it waits for
   * compositionend (CONTRACTS §12.3).
   * @param {any} s
   */
  receive(s) {
    const reply = s.seq > 0;
    if (this.composing) {
      this.deferred = { state: s, reply: reply || this.deferred?.reply };
      return;
    }
    this.render(s, reply);
    if (reply) this.drain();
  }

  render(s, isReply) {
    const prev = this.state;
    // A state built before the server took our latest typing must not undo it
    // (the textAck rule, CONTRACTS §12.3).
    if (this.lastText && s.textAck < this.lastText.seq) {
      const mine = s.rows.find((r) => r.id === this.lastText.id);
      if (mine) {
        mine.text = this.lastText.text;
        mine.markers = undefined;
      }
    }
    this.state = s;
    const editable = !s.readOnly && s.block.length === 0;
    const keep = new Set();
    let child = this.rowsEl.firstElementChild;
    for (const r of s.rows) {
      keep.add(r.id);
      let el = this.rowEls.get(r.id);
      if (!el) {
        el = this.buildRow();
        this.rowEls.set(r.id, el);
      }
      this.updateRow(el, r, r.id === s.cursor, editable);
      if (el === child) child = child.nextElementSibling;
      else this.rowsEl.insertBefore(el, child);
    }
    for (const [id, el] of this.rowEls) {
      if (!keep.has(id)) {
        el.remove();
        this.rowEls.delete(id);
      }
    }
    if (s.rows.length) {
      this.avgRow = 0.9 * this.avgRow + 0.1 * (this.rowsEl.getBoundingClientRect().height / s.rows.length);
    }
    this.topEl.style.height = `${s.start * this.avgRow}px`;
    this.bottomEl.style.height = `${Math.max(0, s.total - s.start - s.rows.length) * this.avgRow}px`;
    this.paintMarkers(s);

    const h = this.host();
    if (h && editable && (isReply || document.activeElement !== h || prev?.cursor !== s.cursor)) {
      if (document.activeElement !== h) h.focus({ preventScroll: true });
      if (isReply || prev?.cursor !== s.cursor) setCaret(h, s.off);
      if (isReply) this.ensureVisible();
    }
  }

  buildRow() {
    const el = document.createElement("div");
    el.className = "row";
    el.setAttribute("role", "treeitem");
    const glyph = document.createElement("span");
    glyph.className = "glyph";
    glyph.setAttribute("aria-hidden", "true");
    const box = document.createElement("span");
    box.className = "box";
    box.setAttribute("aria-hidden", "true");
    const text = document.createElement("div");
    text.className = "text";
    const count = document.createElement("span");
    count.className = "count";
    el.append(glyph, box, text, count);
    return el;
  }

  updateRow(el, r, focused, editable) {
    el.dataset.id = r.id;
    el.dataset.state = r.kids ? (r.collapsed ? "collapsed" : "expanded") : "leaf";
    el.toggleAttribute("data-focus", focused);
    el.toggleAttribute("data-done", r.done);
    el.toggleAttribute("data-heading", r.heading);
    el.toggleAttribute("data-zoom", r.depth < 0);
    el.setAttribute("aria-level", String(Math.max(r.depth, 0) + 1));
    if (r.kids) el.setAttribute("aria-expanded", String(!r.collapsed));
    else el.removeAttribute("aria-expanded");
    el.style.paddingInlineStart = `calc(${Math.max(r.depth, 0)} * var(--indent))`;
    const [, box, text, count] = el.children;
    box.hidden = !r.task;
    box.textContent = r.task ? (r.done ? "☑" : "☐") : "";
    count.textContent = r.hidden ? String(r.hidden) : "";

    if (focused) {
      // The focused bullet holds only its source text (D-042).
      if (el.mode !== "source" || (text.textContent !== r.text && !this.composing)) {
        const keepCaret = document.activeElement === text ? offsetsIn(text) : null;
        text.replaceChildren(document.createTextNode(r.text));
        el.mode = "source";
        el.runsKey = null;
        if (keepCaret) setCaret(text, Math.min(keepCaret.off, r.text.length));
      }
    } else {
      const key = JSON.stringify(r.runs);
      if (el.mode !== "runs" || el.runsKey !== key) {
        text.replaceChildren(...r.runs.map(runNode));
        el.mode = "runs";
        el.runsKey = key;
      }
    }
    const want = focused && editable;
    if (want !== text.isContentEditable) {
      if (want) {
        text.contentEditable = "plaintext-only";
        text.spellcheck = false;
      } else {
        text.removeAttribute("contenteditable");
      }
    }
  }

  /** Dims the focused bullet's markers through the Custom Highlight API (D-042). */
  paintMarkers(s) {
    if (!globalThis.CSS?.highlights) return; // engines without it show markers undimmed (D-042)
    const row = s.rows.find((r) => r.id === s.cursor);
    const h = this.host();
    if (!h || !row?.markers?.length) {
      CSS.highlights.delete(MARKER_HIGHLIGHT);
      return;
    }
    CSS.highlights.set(MARKER_HIGHLIGHT, new Highlight(...row.markers.map(([a, b]) => rangeOf(h, a, b))));
  }

  ensureVisible() {
    const el = this.rowEls.get(this.state?.cursor ?? "");
    if (!el) return;
    const b = el.getBoundingClientRect();
    const v = this.getBoundingClientRect();
    const margin = SCROLL_MARGIN_ROWS * this.avgRow;
    if (b.top < v.top + margin) this.scrollTop -= v.top + margin - b.top;
    else if (b.bottom > v.bottom - margin) this.scrollTop += b.bottom - (v.bottom - margin);
  }

  onScroll() {
    if (this.scrollQueued || !this.state) return;
    this.scrollQueued = true;
    requestAnimationFrame(() => {
      this.scrollQueued = false;
      const s = this.state;
      const first = Math.floor(this.scrollTop / this.avgRow);
      const visible = Math.ceil(this.clientHeight / this.avgRow);
      const end = s.start + s.rows.length;
      if ((first < s.start + EDGE && s.start > 0) || (first + visible > end - EDGE && end < s.total)) {
        this.conn.send({ t: "window", start: Math.max(0, first - WINDOW / 2 + visible), count: WINDOW });
      }
    });
  }

  onMouseDown(e) {
    const row = e.target.closest?.(".row");
    if (!row || !this.state || this.state.readOnly) return;
    const id = row.dataset.id;
    this.goalX = null;
    if (id === this.state.cursor) return; // the browser places the caret
    e.preventDefault();
    const r = this.state.rows.find((x) => x.id === id);
    const off = toSource(r, offsetAtPoint(row.querySelector(".text"), e.clientX, e.clientY));
    this.conn.action("set_cursor", { id, off });
  }

  pushText() {
    const h = this.host();
    if (!h || !this.state) return;
    const text = (h.textContent ?? "").replace(/\n/g, "");
    const o = offsetsIn(h);
    const seq = this.conn.text(this.state.cursor, text, o?.off ?? text.length);
    this.lastText = { id: this.state.cursor, text, seq };
    const row = this.state.rows.find((r) => r.id === this.state.cursor);
    if (row) row.text = text;
  }

  onKeyDown(e) {
    if (!this.state || e.defaultPrevented) return;
    if (e.isComposing || e.keyCode === 229 || this.composing) return; // never queue composition (§9.3)
    if (MODIFIERS.has(e.key)) return;
    const k = { key: e.key, ctrl: e.ctrlKey, meta: e.metaKey, shift: e.shiftKey, alt: e.altKey };
    if (this.conn.inflight > 0 || this.queue.length) {
      e.preventDefault();
      this.queue.push(k);
      return;
    }
    if (this.runKey(k)) {
      e.preventDefault();
      this.drain();
    }
  }

  /** Runs a key; true when it was consumed, false for the browser's default. */
  runKey(k) {
    if (!this.state.cursor) return true;
    const h = this.host();
    if (!h || document.activeElement !== h) {
      // Scrolled away or focus elsewhere: return to the cursor, then replay (§9.3).
      if (h) {
        h.focus({ preventScroll: true });
        if (document.activeElement !== h) return true; // not editable (read-only)
        setCaret(h, this.state.off);
        this.ensureVisible();
      } else {
        this.conn.action("set_cursor", { id: this.state.cursor, off: this.state.off });
      }
      this.queue.unshift(k);
      return true;
    }
    return this.textKey(k, h);
  }

  textKey(k, h) {
    if (k.key !== "ArrowUp" && k.key !== "ArrowDown") this.goalX = null;
    if ((k.ctrl || k.meta) && !k.alt) {
      const key = k.key.toLowerCase();
      return key === "z" || key === "y"; // TODO(P1-37): undo and redo; never the browser's own
    }
    switch (k.key) {
      case "Enter":
      case "Tab":
        return true; // TODO(P1-33): structural keys
      case "ArrowUp":
      case "ArrowDown":
        if (k.shift || k.alt) return false;
        this.verticalMove(h, k.key === "ArrowUp" ? -1 : 1);
        return true;
      case "ArrowLeft":
      case "ArrowRight":
        return !k.shift && !k.alt && this.horizontalEdge(h, k.key === "ArrowLeft" ? -1 : 1);
    }
    return false;
  }

  /** Replays queued keys while no action is waiting for its reply. */
  drain() {
    while (this.conn.inflight === 0 && this.queue.length && !this.composing) {
      const k = this.queue.shift();
      if (this.runKey(k)) continue;
      if (k.ctrl || k.meta || k.alt) continue;
      // Keys the browser would have handled itself, replayed by hand.
      const sel = getSelection();
      const alter = k.shift ? "extend" : "move";
      if (printable(k.key)) document.execCommand("insertText", false, k.key);
      else if (k.key === "Backspace") document.execCommand("delete");
      else if (k.key === "Delete") document.execCommand("forwardDelete");
      else if (k.key === "ArrowLeft" || k.key === "ArrowRight") sel.modify(alter, k.key === "ArrowLeft" ? "backward" : "forward", "character");
      else if (k.key === "Home" || k.key === "End") sel.modify(alter, k.key === "Home" ? "backward" : "forward", "lineboundary");
    }
  }

  /**
   * Up and Down move by visual line inside the bullet, then to the next
   * bullet at the same column (§6.1).
   */
  verticalMove(h, dir) {
    const rect = caretRect(h);
    const x = this.goalX ?? rect.left;
    this.goalX = x;
    const lh = rect.height || this.avgRow;
    const y = dir < 0 ? rect.top - lh / 2 : rect.bottom + lh / 2;
    const b = h.getBoundingClientRect();
    if (y > b.top && y < b.bottom) {
      setCaret(h, offsetAtPoint(h, x, y));
      return;
    }
    const rows = this.state.rows;
    const next = rows[rows.findIndex((r) => r.id === this.state.cursor) + dir];
    if (!next) return;
    const el = this.rowEls.get(next.id).querySelector(".text");
    const tb = el.getBoundingClientRect();
    const ty = dir < 0 ? tb.bottom - lh / 2 : tb.top + lh / 2;
    this.conn.action("set_cursor", { id: next.id, off: toSource(next, offsetAtPoint(el, x, ty)) });
  }

  /** Left at the start and Right at the end cross into the next bullet (§6.1). */
  horizontalEdge(h, dir) {
    const o = offsetsIn(h);
    if (!o || o.off !== o.end) return false;
    const len = (h.textContent ?? "").length;
    if ((dir < 0 && o.off > 0) || (dir > 0 && o.off < len)) return false;
    const rows = this.state.rows;
    const next = rows[rows.findIndex((r) => r.id === this.state.cursor) + dir];
    if (next) this.conn.action("set_cursor", { id: next.id, off: dir < 0 ? next.text.length : 0 });
    return true;
  }
}

/**
 * One run as DOM: data never becomes HTML (ARCHITECTURE §5.1).
 * @param {{text: string, style?: string[], color?: string, href?: string}} run
 * @returns {Node}
 */
function runNode(run) {
  if (!run.style && !run.color && !run.href) return document.createTextNode(run.text);
  const el = document.createElement(run.href ? "a" : "span");
  if (run.href) el.href = run.href;
  for (const s of run.style ?? []) el.classList.add(`m-${s}`);
  if (run.color) el.dataset.color = run.color;
  el.textContent = run.text;
  return el;
}

customElements.define("hyphal-outline", OutlineView);
