// Caret and selection helpers for the focused bullet. Offsets are UTF-16 code
// units within one host element, the unit the protocol uses (CONTRACTS §12).

/**
 * The selection inside el as {off, end}, or null when it is elsewhere.
 * @param {HTMLElement} el
 * @returns {{off: number, end: number} | null}
 */
export function offsetsIn(el) {
  const sel = getSelection();
  if (!sel || !sel.anchorNode || !el.contains(sel.anchorNode) || !el.contains(sel.focusNode)) return null;
  const at = (node, offset) => {
    const r = document.createRange();
    r.setStart(el, 0);
    r.setEnd(node, offset);
    return r.toString().length;
  };
  const a = at(sel.anchorNode, sel.anchorOffset);
  const f = at(sel.focusNode, sel.focusOffset);
  return { off: Math.min(a, f), end: Math.max(a, f) };
}

/**
 * The text node and offset for a UTF-16 offset in el.
 * @param {HTMLElement} el
 * @param {number} off
 * @returns {[Node, number]}
 */
export function pointAt(el, off) {
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  let left = Math.max(0, off);
  let node = walker.nextNode();
  if (!node) return [el, 0];
  while (left > node.length) {
    const next = walker.nextNode();
    if (!next) return [node, node.length];
    left -= node.length;
    node = next;
  }
  return [node, left];
}

/**
 * A Range over [start, end) of el's text.
 * @param {HTMLElement} el
 * @param {number} start
 * @param {number} end
 * @returns {Range}
 */
export function rangeOf(el, start, end) {
  const r = document.createRange();
  r.setStart(...pointAt(el, start));
  r.setEnd(...pointAt(el, end));
  return r;
}

/**
 * Puts a collapsed caret at off in el.
 * @param {HTMLElement} el
 * @param {number} off
 */
export function setCaret(el, off) {
  const sel = getSelection();
  const r = rangeOf(el, off, off);
  sel.removeAllRanges();
  sel.addRange(r);
}

/**
 * The caret's rectangle in el, also for an empty bullet or a caret at the end.
 * @param {HTMLElement} el
 * @returns {DOMRect}
 */
export function caretRect(el) {
  const sel = getSelection();
  const o = offsetsIn(el);
  if (sel.rangeCount && o && el.textContent) {
    const r = sel.getRangeAt(0).cloneRange();
    r.collapse(!(sel.focusNode === r.endContainer && sel.focusOffset === r.endOffset));
    const rects = r.getClientRects();
    if (rects.length) return rects[rects.length - 1];
    // A collapsed range has no rect in some engines: measure the next or previous character.
    const len = el.textContent.length;
    if (o.off > 0) {
      const w = rangeOf(el, o.off - 1, o.off).getBoundingClientRect();
      return new DOMRect(w.right, w.top, 0, w.height);
    }
    if (len > 0) {
      const w = rangeOf(el, 0, 1).getBoundingClientRect();
      return new DOMRect(w.left, w.top, 0, w.height);
    }
  }
  const b = el.getBoundingClientRect();
  return new DOMRect(b.left, b.top, 0, parseFloat(getComputedStyle(el).lineHeight) || b.height);
}

/**
 * The text offset in el nearest to a point, clamped into el's box.
 * @param {HTMLElement} el
 * @param {number} x
 * @param {number} y
 * @returns {number}
 */
export function offsetAtPoint(el, x, y) {
  const b = el.getBoundingClientRect();
  const cx = Math.max(b.left + 1, Math.min(x, b.right - 1));
  const cy = Math.max(b.top + 1, Math.min(y, b.bottom - 1));
  let node = null;
  let off = 0;
  if (document.caretPositionFromPoint) {
    const p = document.caretPositionFromPoint(cx, cy);
    if (p) [node, off] = [p.offsetNode, p.offset];
  } else if (document.caretRangeFromPoint) {
    // WebKit before 18.4 has only caretRangeFromPoint.
    const r = document.caretRangeFromPoint(cx, cy);
    if (r) [node, off] = [r.startContainer, r.startOffset];
  }
  if (!node || !el.contains(node)) return y < b.top + b.height / 2 ? 0 : (el.textContent ?? "").length;
  const r = document.createRange();
  r.setStart(el, 0);
  r.setEnd(node, off);
  return r.toString().length;
}
