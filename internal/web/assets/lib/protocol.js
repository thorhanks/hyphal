// The WebSocket client for CONTRACTS §12. Actions and text have separate
// sequence counters; every action gets exactly one state reply echoing its seq.

export class Connection {
  /**
   * @param {(msg: any) => void} onMessage
   * @param {() => void} onClose
   */
  constructor(onMessage, onClose) {
    this.onMessage = onMessage;
    this.onClose = onClose;
    this.actionSeq = 0;
    this.textSeq = 0;
    this.inflight = 0; // actions without their state reply
    this.held = null; // dev hooks: messages held back, or null
    this.ws = null;
  }

  /** @param {any} hello */
  open(hello) {
    this.ws = new WebSocket(`ws://${location.host}/ws`);
    this.ws.onopen = () => this.send(hello);
    this.ws.onmessage = (ev) => {
      const m = JSON.parse(ev.data);
      if (this.held) this.held.push(m);
      else this.deliver(m);
    };
    this.ws.onclose = () => this.onClose();
  }

  deliver(m) {
    if (m.t === "state" && m.seq > 0) this.inflight--;
    this.onMessage(m);
  }

  /** @param {any} m */
  send(m) {
    this.ws.send(JSON.stringify(m));
  }

  /**
   * Sends an action; its reply arrives as a state with the same seq.
   * @param {string} name
   * @param {object} [fields]
   */
  action(name, fields = {}) {
    this.inflight++;
    this.send({ t: "action", seq: ++this.actionSeq, name, ...fields });
  }

  /**
   * Sends the focused bullet's text; the next states carry textAck (§7.3).
   * @param {string} id
   * @param {string} text
   * @param {number} off
   * @returns {number} the seq sent
   */
  text(id, text, off) {
    this.send({ t: "text", seq: ++this.textSeq, id, text, off });
    return this.textSeq;
  }
}
