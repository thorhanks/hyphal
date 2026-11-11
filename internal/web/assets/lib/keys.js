// Key dispatch from the keymap the server sends in welcome (§6.4,
// CONTRACTS §12.2). The server resolves Mod for the platform; the page only
// turns events into the same key strings and looks them up.

/**
 * @typedef {{context: string, key: string}} Key
 * @typedef {{id: string, name: string, phase: number, contexts: string[],
 *   paletteOnly?: boolean, block?: boolean, native?: boolean, hidden?: boolean,
 *   keys: Key[]}} Command
 */

const NAMED = new Map([[" ", "space"]]);
const LETTER_OR_DIGIT = /^(?:Key([A-Z])|Digit([0-9]))$/;

/**
 * The key string of an event, in internal/keymap's notation: ctrl, meta,
 * alt, shift, then the key. A character keeps its own shift ("Z", "?")
 * unless ctrl, meta or alt is held; then letters are lowercase and shift is
 * written ("ctrl+shift+z").
 * @param {{key: string, code?: string, ctrlKey: boolean, metaKey: boolean,
 *   altKey: boolean, shiftKey: boolean}} e
 * @returns {string}
 */
export function keyString(e) {
  let key = NAMED.get(e.key) ?? e.key;
  const chord = e.ctrlKey || e.metaKey || e.altKey;
  const mods = [];
  if (e.ctrlKey) mods.push("ctrl");
  if (e.metaKey) mods.push("meta");
  if (e.altKey) mods.push("alt");
  if ([...key].length === 1) {
    if (chord) {
      // Option on macOS types a symbol ("∫" for b): name the key instead.
      const m = e.altKey ? LETTER_OR_DIGIT.exec(e.code ?? "") : null;
      key = m ? (m[1] ?? m[2]).toLowerCase() : key.toLowerCase();
      if (e.shiftKey) mods.push("shift");
    }
  } else {
    key = key.toLowerCase();
    if (e.shiftKey) mods.push("shift");
  }
  return [...mods, key].join("+");
}

export class Keymap {
  /** @param {Command[]} commands */
  constructor(commands) {
    /** @type {Map<string, Command>} */
    this.commands = new Map();
    /** @type {Map<string, Map<string, Command>>} context → key → command */
    this.keys = new Map();
    for (const c of commands) {
      this.commands.set(c.id, c);
      for (const k of c.keys) {
        if (!this.keys.has(k.context)) this.keys.set(k.context, new Map());
        this.keys.get(k.context)?.set(k.key, c);
      }
    }
  }

  /**
   * The command bound to key in context, then in global; null when unbound.
   * @param {string} context
   * @param {string} key
   * @returns {Command | null}
   */
  lookup(context, key) {
    return this.keys.get(context)?.get(key) ?? this.keys.get("global")?.get(key) ?? null;
  }
}
