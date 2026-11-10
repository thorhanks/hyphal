// window.__hyphal: test hooks, served only by dev builds run with --dev
// (TESTING §5). Release builds don't embed this directory.

import { offsetsIn } from "../lib/selection.js";
import { MARKER_HIGHLIGHT } from "../components/outline-view.js";

/** @param {import("../components/app.js").App} app */
export function install(app) {
  const view = app.outline;
  const conn = app.conn;
  const offsetOf = (host, node, off) => {
    const r = document.createRange();
    r.setStart(host, 0);
    r.setEnd(node, off);
    return r.toString().length;
  };
  window.__hyphal = {
    state: () => view.state,
    idle: () => conn.inflight === 0 && view.queue.length === 0 && !view.composing,
    /** The visible rows as indented text, with [x] for done and [c] for collapsed. */
    outline: () =>
      view.state.rows
        .map((r) => {
          const flags = (r.done ? " [x]" : "") + (r.collapsed ? " [c]" : "");
          return r.depth < 0 ? `# ${r.text}` : "  ".repeat(r.depth) + r.text + flags;
        })
        .join("\n"),
    caret: () => {
      const h = view.host();
      return h ? offsetsIn(h) : null;
    },
    idOf: (text) => view.state.rows.find((r) => r.text === text)?.id ?? null,
    setCursor: (id, off) => conn.action("set_cursor", { id, off }),
    /** Holds incoming messages until release, to deliver a state late. */
    hold: () => {
      conn.held = [];
    },
    heldCount: () => conn.held?.length ?? 0,
    /** Delivers the first n held messages, or all of them and stops holding. */
    release: (n) => {
      const held = conn.held ?? [];
      const now = held.splice(0, n ?? held.length);
      if (n === undefined) conn.held = null;
      for (const m of now) conn.deliver(m);
    },
    send: (m) => conn.send(m),
    /** Marker highlight ranges on the focused bullet, as UTF-16 offsets. */
    markers: () => {
      const h = view.host();
      const hl = globalThis.CSS?.highlights?.get(MARKER_HIGHLIGHT);
      if (!h || !hl) return [];
      return [...hl].map((r) => [offsetOf(h, r.startContainer, r.startOffset), offsetOf(h, r.endContainer, r.endOffset)]);
    },
  };
}
