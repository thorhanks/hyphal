// <hyphal-app>: the page shell (§4.5). It owns the connection and hands states
// to the outline. TODO(P1-30): the full shell, banners, save indicator and
// reconnect.

import { Connection } from "../lib/protocol.js";
import { strings } from "../lib/strings.js";
import "./outline-view.js";

/** The zoom in the address, `/#/<node-id>` (§4.1), or null for Home. */
function zoomFromHash() {
  const m = /^#\/([0-9a-z]+)$/.exec(location.hash);
  return m ? m[1] : null;
}

export class App extends HTMLElement {
  connectedCallback() {
    if (this.outline) return;
    this.crumbs = document.createElement("nav");
    this.crumbs.className = "crumbs";
    this.header = document.createElement("header");
    this.header.append(this.crumbs);
    this.outline = document.createElement("hyphal-outline");
    this.footer = document.createElement("footer");
    this.footer.setAttribute("role", "status");
    this.footer.setAttribute("aria-live", "polite");
    this.append(this.header, this.outline, this.footer);

    this.version = "";
    this.devLoaded = false;
    this.conn = new Connection(
      (m) => this.receive(m),
      () => this.say(strings.serverStopped),
    );
    this.outline.conn = this.conn;
    this.conn.open({ t: "hello", version: this.version, zoom: zoomFromHash() });
  }

  receive(m) {
    switch (m.t) {
      case "welcome":
        if (this.version && m.version !== this.version) location.reload();
        this.version = m.version;
        document.documentElement.dataset.density = m.config.density;
        if (m.dev && !this.devLoaded) {
          this.devLoaded = true;
          import("../dev/hooks.js").then((d) => d.install(this));
        }
        break;
      case "state":
        this.outline.receive(m);
        this.crumbs.textContent = m.crumbs.map((c) => c.text || strings.home).join(strings.crumbSeparator);
        this.say(m.message);
        break;
      case "error":
        this.say(m.message);
        break;
    }
  }

  /** @param {string} text */
  say(text) {
    this.footer.textContent = text;
  }
}

customElements.define("hyphal-app", App);
