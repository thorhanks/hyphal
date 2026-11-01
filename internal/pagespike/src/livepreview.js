// Obsidian-style live preview on CodeMirror 6: rendered everywhere except the
// lines the selection touches, which show their Markdown source.
import { Prec, StateEffect, StateField, RangeSetBuilder } from "@codemirror/state";
import { Decoration, EditorView, ViewPlugin, WidgetType, keymap } from "@codemirror/view";
import { syntaxTree, ensureSyntaxTree, syntaxTreeAvailable } from "@codemirror/language";

const setDragging = StateEffect.define();

// While the mouse is down nothing reveals or hides, so text doesn't shift under a drag.
const dragging = StateField.define({
  create: () => false,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setDragging)) return e.value;
    return value;
  },
});

// Line ranges that show source, as [from, to] pairs.
const revealed = StateField.define({
  create: (state) => linesOf(state),
  update(value, tr) {
    const stillDragging = tr.state.field(dragging);
    if (stillDragging && !tr.startState.field(dragging)) return value;
    if (stillDragging) return tr.docChanged ? value.map(([a, b]) => [tr.changes.mapPos(a), tr.changes.mapPos(b)]) : value;
    if (!tr.docChanged && !tr.selection && tr.startState.field(dragging) === stillDragging) return value;
    return linesOf(tr.state);
  },
});

function linesOf(state) {
  return state.selection.ranges.map((r) => [state.doc.lineAt(r.from).from, state.doc.lineAt(r.to).to]);
}
const touches = (ranges, from, to) => ranges.some(([a, b]) => a <= to && b >= from);

class BulletWidget extends WidgetType {
  eq() { return true; }
  toDOM() {
    const el = document.createElement("span");
    el.className = "lp-bullet";
    el.textContent = "•";
    return el;
  }
}

class RuleWidget extends WidgetType {
  eq() { return true; }
  toDOM() {
    const el = document.createElement("span");
    el.className = "lp-rule";
    return el;
  }
}

class CheckboxWidget extends WidgetType {
  constructor(checked) { super(); this.checked = checked; }
  eq(other) { return other.checked === this.checked; }
  toDOM(view) {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.className = "lp-check";
    box.checked = this.checked;
    box.tabIndex = -1;
    box.addEventListener("mousedown", (e) => e.preventDefault());
    box.addEventListener("click", (e) => {
      e.preventDefault();
      const from = view.posAtDOM(box);
      view.dispatch({ changes: { from: from + 1, to: from + 2, insert: this.checked ? " " : "x" }, userEvent: "input.toggle" });
    });
    return box;
  }
  ignoreEvent() { return true; }
}

class ImageWidget extends WidgetType {
  constructor(src, alt) { super(); this.src = src; this.alt = alt; }
  eq(other) { return other.src === this.src && other.alt === this.alt; }
  toDOM() {
    const img = document.createElement("img");
    img.className = "lp-image";
    img.alt = this.alt;
    if (safeImage(this.src)) img.src = this.src;
    return img;
  }
}

export function safeLink(url) {
  return /^(https?:|mailto:)/i.test(url.trim());
}
function safeImage(url) {
  return /^(https?:|\/|\.\/)/i.test(url.trim());
}

// Cells keep their source offset, so a click lands on the same text in the source.
function renderInline(state, node, parent) {
  const tags = { StrongEmphasis: "strong", Emphasis: "em", InlineCode: "code", Strikethrough: "s" };
  const marks = new Set(["EmphasisMark", "CodeMark", "StrikethroughMark", "LinkMark", "URL", "LinkTitle"]);
  let pos = node.from;
  for (let child = node.firstChild; child; child = child.nextSibling) {
    if (child.from > pos) parent.append(state.sliceDoc(pos, child.from));
    pos = child.to;
    if (marks.has(child.name)) continue;
    const tag = tags[child.name];
    if (tag) {
      const el = document.createElement(tag);
      renderInline(state, child, el);
      parent.append(el);
    } else if (child.name === "Link") {
      const el = document.createElement("span");
      el.className = "lp-link";
      renderInline(state, child, el);
      parent.append(el);
    } else {
      parent.append(state.sliceDoc(child.from, child.to));
    }
  }
  if (node.to > pos) parent.append(state.sliceDoc(pos, node.to));
}

class TableWidget extends WidgetType {
  constructor(source, rows) { super(); this.source = source; this.rows = rows; }
  eq(other) { return other.source === this.source; }
  toDOM(view) {
    const table = document.createElement("table");
    table.className = "lp-table";
    for (const row of this.rows) {
      const tr = document.createElement("tr");
      for (const cell of row.cells) {
        const td = document.createElement(row.header ? "th" : "td");
        td.dataset.offset = cell.offset;
        td.append(...cell.nodes.map((n) => n.cloneNode(true)));
        tr.append(td);
      }
      table.append(tr);
    }
    table.addEventListener("mousedown", (e) => {
      e.preventDefault();
      const td = e.target.closest("td,th");
      const base = view.posAtDOM(table);
      view.dispatch({ selection: { anchor: base + (td ? Number(td.dataset.offset) : 0) } });
      view.focus();
    });
    return table;
  }
  ignoreEvent() { return true; }
}

function tableRows(state, node) {
  const rows = [];
  for (let row = node.node.firstChild; row; row = row.nextSibling) {
    if (row.name !== "TableHeader" && row.name !== "TableRow") continue;
    const cells = [];
    for (let cell = row.firstChild; cell; cell = cell.nextSibling) {
      if (cell.name !== "TableCell") continue;
      const holder = document.createElement("span");
      renderInline(state, cell, holder);
      cells.push({ offset: cell.from - node.from, nodes: [...holder.childNodes] });
    }
    rows.push({ header: row.name === "TableHeader", cells });
  }
  return rows;
}

// Block widgets have to come from a state field. Rendered tables stay in the doc
// as zero-height lines, so the arrow keys walk into them and reveal the source.
const tables = StateField.define({
  create: (state) => buildTables(state),
  update(value, tr) {
    const reveal = tr.state.field(revealed);
    const treeMoved = syntaxTree(tr.state) !== syntaxTree(tr.startState);
    if (!tr.docChanged && !treeMoved && reveal === tr.startState.field(revealed)) return value;
    if (!tr.docChanged && !treeMoved && value.spans.every((s) => touches(reveal, s.from, s.to) === s.open)) return value;
    return buildTables(tr.state);
  },
  provide: (f) => EditorView.decorations.from(f, (v) => v.deco),
});

function buildTables(state) {
  const tree = ensureSyntaxTree(state, Math.min(state.doc.length, 2_000_000), 20) ?? syntaxTree(state);
  const reveal = state.field(revealed);
  const builder = new RangeSetBuilder();
  const spans = [];
  tree.topNode.cursor().iterate((n) => {
    if (n.name === "Document") return true;
    if (n.name === "Table") {
      const open = touches(reveal, n.from, n.to);
      spans.push({ from: n.from, to: n.to, open, all: true });
      if (!open) {
        const source = state.sliceDoc(n.from, n.to);
        builder.add(n.from, n.from, Decoration.widget({ widget: new TableWidget(source, tableRows(state, n)), block: true, side: -1 }));
        for (let line = state.doc.lineAt(n.from); ; line = state.doc.line(line.number + 1)) {
          builder.add(line.from, line.from, Decoration.line({ class: "lp-collapsed" }));
          if (line.to >= n.to || line.number === state.doc.lines) break;
        }
      }
    }
    if (n.name === "FencedCode") spans.push({ from: n.from, to: n.to, open: touches(reveal, n.from, n.to), all: false });
    return false;
  });
  return { deco: builder.finish(), spans };
}

// Zero-height lines are invisible to vertical motion, which works from screen
// coordinates, so the arrows step into a collapsed block explicitly.
function vertical(forward) {
  return (view) => {
    const { state } = view;
    const range = state.selection.main;
    if (!range.empty) return false;
    const line = state.doc.lineAt(range.head);
    const next = view.moveVertically(range, forward).head;
    const closed = state.field(tables).spans.filter((s) => !s.open);
    const hit = forward
      ? closed.find((s) => s.from > line.to && s.from <= Math.max(next, line.to + 1))
      : closed.findLast((s) => s.to < line.from && s.to >= Math.min(next, line.from - 1));
    if (!hit) return false;
    const target = forward ? hit.from : state.doc.lineAt(hit.to).from;
    view.dispatch({ selection: { anchor: target }, scrollIntoView: true });
    return true;
  };
}
const arrows = Prec.high(keymap.of([
  { key: "ArrowDown", run: vertical(true) },
  { key: "ArrowUp", run: vertical(false) },
]));

const heading = /^ATXHeading(\d)$/;
const setext = /^SetextHeading(\d)$/;
const inlineClass = {
  Emphasis: "lp-em", StrongEmphasis: "lp-strong", InlineCode: "lp-code", Strikethrough: "lp-strike",
  Highlight: "lp-highlight", HTMLTag: "lp-literal", HTMLBlock: "lp-literal",
};
const hiddenMark = new Set(["EmphasisMark", "CodeMark", "StrikethroughMark", "HighlightMark"]);

function buildInline(view) {
  const { state } = view;
  const reveal = state.field(revealed);
  const deco = [];
  const line = (pos, cls) => deco.push(Decoration.line({ class: cls }).range(state.doc.lineAt(pos).from));
  const hide = (from, to) => { if (to > from) deco.push(Decoration.replace({}).range(from, to)); };
  const dim = (from, to) => { if (to > from) deco.push(Decoration.mark({ class: "lp-syntax" }).range(from, to)); };
  const lineOpen = (from, to) => touches(reveal, state.doc.lineAt(from).from, state.doc.lineAt(to).to);
  const seenLines = new Set();
  const once = (pos, cls) => {
    const key = state.doc.lineAt(pos).from + cls;
    if (!seenLines.has(key)) { seenLines.add(key); line(pos, cls); }
  };

  for (const { from, to } of view.visibleRanges) {
    syntaxTree(state).iterate({
      from,
      to,
      enter(n) {
        const name = n.name;
        let m;
        if ((m = heading.exec(name)) || (m = setext.exec(name))) once(n.from, `lp-h lp-h${m[1]}`);
        else if (name === "Table") return false;
        else if (inlineClass[name]) deco.push(Decoration.mark({ class: inlineClass[name] }).range(n.from, n.to));
        else if (name === "HeaderMark") {
          const open = lineOpen(n.from, n.to);
          const after = state.sliceDoc(n.to, n.to + 1) === " " ? n.to + 1 : n.to;
          open ? dim(n.from, after) : hide(n.from, after);
        } else if (name === "CodeMark" && n.node.parent?.name === "FencedCode") {
          dim(n.from, n.to);
        } else if (hiddenMark.has(name)) {
          lineOpen(n.from, n.to) ? dim(n.from, n.to) : hide(n.from, n.to);
        } else if (name === "Link" ) {
          const url = n.node.getChild("URL");
          const href = url ? state.sliceDoc(url.from, url.to) : "";
          const attrs = href && safeLink(href) ? { "data-href": href, title: href } : {};
          deco.push(Decoration.mark({ class: "lp-link", attributes: attrs }).range(n.from, n.to));
          const open = lineOpen(n.from, n.to);
          const marks = n.node.getChildren("LinkMark");
          if (open) {
            for (let c = n.node.firstChild; c; c = c.nextSibling) {
              if (c.name === "LinkMark" || c.name === "URL" || c.name === "LinkTitle") dim(c.from, c.to);
            }
          } else if (marks.length) {
            // Everything from "](" to the end goes, including the space before a title.
            hide(marks[0].from, marks[0].to);
            if (marks[1]) hide(marks[1].from, n.to);
          }
          return open ? undefined : false;
        } else if (name === "Image") {
          if (!lineOpen(n.from, n.to)) {
            const url = n.node.getChild("URL");
            const src = url ? state.sliceDoc(url.from, url.to) : "";
            const text = state.sliceDoc(n.from, n.to).replace(/^!\[|\]\(.*$/g, "");
            deco.push(Decoration.replace({ widget: new ImageWidget(src, text) }).range(n.from, n.to));
            return false;
          }
        } else if (name === "QuoteMark") {
          once(n.from, "lp-quote");
          const after = state.sliceDoc(n.to, n.to + 1) === " " ? n.to + 1 : n.to;
          lineOpen(n.from, n.to) ? dim(n.from, after) : hide(n.from, after);
        } else if (name === "ListMark") {
          const text = state.sliceDoc(n.from, n.to);
          if (!lineOpen(n.from, n.to) && /^[-*+]$/.test(text)) {
            const task = n.node.parent?.getChild("Task");
            if (task) hide(n.from, n.to + 1);
            else deco.push(Decoration.replace({ widget: new BulletWidget() }).range(n.from, n.to));
          } else {
            deco.push(Decoration.mark({ class: "lp-listmark" }).range(n.from, n.to));
          }
        } else if (name === "TaskMarker") {
          if (!lineOpen(n.from, n.to)) {
            const checked = /x/i.test(state.sliceDoc(n.from, n.to));
            deco.push(Decoration.replace({ widget: new CheckboxWidget(checked) }).range(n.from, n.to));
          } else dim(n.from, n.to);
        } else if (name === "HorizontalRule") {
          if (!lineOpen(n.from, n.to)) deco.push(Decoration.replace({ widget: new RuleWidget() }).range(n.from, n.to));
          else dim(n.from, n.to);
        } else if (name === "FencedCode") {
          const open = touches(reveal, n.from, n.to);
          const first = state.doc.lineAt(n.from), last = state.doc.lineAt(n.to);
          for (let i = first.number; i <= last.number; i++) {
            const l = state.doc.line(i);
            if (l.to < from - 2000 || l.from > to + 2000) continue;
            const fence = i === first.number || (i === last.number && /^\s*(`{3,}|~{3,})\s*$/.test(l.text));
            once(l.from, fence && !open ? "lp-collapsed" : "lp-fence");
          }
        }
        return undefined;
      },
    });
  }
  return Decoration.set(deco, true);
}

const inline = ViewPlugin.fromClass(
  class {
    constructor(view) { this.decorations = buildInline(view); }
    update(u) {
      if (u.docChanged || u.viewportChanged || u.selectionSet || syntaxTree(u.startState) !== syntaxTree(u.state)
        || u.startState.field(revealed) !== u.state.field(revealed)) {
        this.decorations = buildInline(u.view);
      }
    }
  },
  { decorations: (v) => v.decorations },
);

const pointer = EditorView.domEventHandlers({
  mousedown(event, view) {
    const link = event.target.closest?.("[data-href]");
    if (link && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      window.open(link.dataset.href, "_blank", "noopener");
      return true;
    }
    if (event.button === 0) {
      view.dispatch({ effects: setDragging.of(true) });
      const up = () => {
        window.removeEventListener("mouseup", up, true);
        view.dispatch({ effects: setDragging.of(false) });
      };
      window.addEventListener("mouseup", up, true);
    }
    return false;
  },
});

export const livePreview = [dragging, revealed, tables, inline, pointer, arrows];
export { syntaxTreeAvailable };
