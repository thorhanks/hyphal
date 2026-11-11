// /keyprobe: the human key matrix (§6.4 "Human key matrix",
// docs/keyprobe.md). It lists every binding of the keymap the server
// resolved, marks each key that reaches the page, and downloads the result
// as JSON. Like the app, it cancels bound keys and leaves native ones alone.

import { keyString } from "../lib/keys.js";

const MODIFIERS = new Set(["Control", "Shift", "Alt", "AltGraph", "Meta", "CapsLock", "Fn", "OS"]);
const text = {
  yes: "yes",
  no: "—",
  summary: (n, of, platform) => `${n} of ${of} bound keys reached the page (keymap for ${platform}).`,
};

/** @param {string} tag @param {string} [content] */
function el(tag, content) {
  const e = document.createElement(tag);
  if (content !== undefined) e.textContent = content;
  return e;
}

async function main() {
  const res = await fetch("/keyprobe.json", { cache: "no-store" });
  /** @type {{platform: string, keymap: import("../lib/keys.js").Command[], reserved: string[]}} */
  const data = await res.json();
  const reached = new Set();
  /** @type {string[]} */
  const other = [];

  const rows = [];
  for (const c of data.keymap) {
    for (const k of c.keys) rows.push({ context: k.context, key: k.key, command: c.id, native: !!c.native });
  }
  const body = /** @type {HTMLElement} */ (document.querySelector(".bindings tbody"));
  const cells = new Map(); // key → reached cells and rows
  const addRow = (tbody, values, key) => {
    const tr = el("tr");
    tr.dataset.key = key;
    tr.dataset.reached = "no";
    for (const v of values) {
      const td = el("td");
      td.append(el("code", v));
      tr.append(td);
    }
    const done = el("td", text.no);
    tr.append(done);
    tbody.append(tr);
    if (!cells.has(key)) cells.set(key, []);
    cells.get(key).push({ tr, done });
  };
  for (const r of rows) addRow(body, [r.context, r.key, r.command, r.native ? text.yes : ""], r.key);
  const reservedBody = /** @type {HTMLElement} */ (document.querySelector(".reserved tbody"));
  for (const k of data.reserved) addRow(reservedBody, [k], k);

  const bound = new Set(rows.map((r) => r.key));
  const summary = /** @type {HTMLElement} */ (document.querySelector(".summary"));
  const otherList = /** @type {HTMLElement} */ (document.querySelector(".other"));
  const update = () => {
    const n = [...bound].filter((k) => reached.has(k)).length;
    summary.textContent = text.summary(n, bound.size, data.platform);
  };
  update();

  window.addEventListener(
    "keydown",
    (e) => {
      if (MODIFIERS.has(e.key) || e.isComposing) return;
      const key = keyString(e);
      // Cancel what the app would cancel: bound keys that aren't native, in
      // any context.
      if (rows.some((r) => r.key === key && !r.native)) e.preventDefault();
      if (cells.has(key)) {
        reached.add(key);
        for (const { tr, done } of cells.get(key)) {
          tr.dataset.reached = "yes";
          done.textContent = text.yes;
        }
      } else if (!other.includes(key)) {
        other.push(key);
        otherList.append(el("li", key));
      }
      update();
    },
    { capture: true },
  );

  document.querySelector(".download")?.addEventListener("click", () => {
    const result = {
      probe: "keyprobe",
      version: 1,
      recordedAt: new Date().toISOString(),
      userAgent: navigator.userAgent,
      platform: data.platform,
      standalone: matchMedia("(display-mode: standalone)").matches || matchMedia("(display-mode: minimal-ui)").matches,
      bindings: rows.map((r) => ({ ...r, reached: reached.has(r.key) })),
      reserved: data.reserved.map((key) => ({ key, reached: reached.has(key) })),
      other,
    };
    const blob = new Blob([JSON.stringify(result, null, 2) + "\n"], { type: "application/json" });
    const a = el("a");
    a.href = URL.createObjectURL(blob);
    a.download = `keyprobe-${data.platform}-${Date.now()}.json`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  });
  /** @type {HTMLElement | null} */ (document.querySelector(".pad"))?.focus();
}

main();
