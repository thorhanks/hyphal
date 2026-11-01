import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { LanguageDescription, indentUnit, syntaxHighlighting } from "@codemirror/language";
import { markdown, markdownKeymap, markdownLanguage } from "@codemirror/lang-markdown";
import { javascript } from "@codemirror/lang-javascript";
import { python } from "@codemirror/lang-python";
import { go } from "@codemirror/lang-go";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { classHighlighter } from "@lezer/highlight";
import { livePreview } from "./livepreview.js";
import { sample, longPage } from "./sample.js";

// §5.8 ==highlight==, to prove a Hyphal extension fits the parser.
const Highlight = {
  defineNodes: ["Highlight", "HighlightMark"],
  parseInline: [{
    name: "Highlight",
    after: "Emphasis",
    parse(cx, next, pos) {
      if (next !== 61 || cx.char(pos + 1) !== 61) return -1;
      const end = cx.slice(pos + 2, cx.end).indexOf("==");
      if (end < 1) return -1;
      const close = pos + 2 + end;
      return cx.addElement(cx.elt("Highlight", pos, close + 2, [cx.elt("HighlightMark", pos, pos + 2), cx.elt("HighlightMark", close, close + 2)]));
    },
  }],
};

const lang = (name, alias, support) => LanguageDescription.of({ name, alias, support: support() });
const codeLanguages = [
  lang("javascript", ["js"], javascript), lang("python", ["py"], python), lang("go", [], go),
  lang("css", [], css), lang("html", [], html),
];

// CodeMirror's base styles are unlayered, so they beat anything in a cascade layer.
// Only these overrides live in JS, and every value is a token, so one CSS file still restyles a page.
const tokens = EditorView.theme({
  "&.cm-editor": { height: "100%", background: "var(--bg)", color: "var(--text)", fontSize: "var(--size)" },
  "&.cm-editor.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-text)", lineHeight: "var(--lead)" },
  ".cm-content": { maxWidth: "var(--column)", margin: "0 auto", padding: "32px 0", caretColor: "var(--accent)", flexGrow: "1" },
  ".cm-line": { padding: "0 16px" },
  ".cm-line.lp-collapsed": { height: "0", padding: "0", lineHeight: "0", fontSize: "0", overflow: "hidden" },
});

const params = new URLSearchParams(location.search);
const doc = params.get("long") ? longPage(Number(params.get("long"))) : params.get("doc") ?? sample;

const view = new EditorView({
  parent: document.getElementById("page"),
  state: EditorState.create({
    doc,
    extensions: [
      history(),
      indentUnit.of("  "),
      keymap.of([...markdownKeymap, ...defaultKeymap, ...historyKeymap, indentWithTab]),
      markdown({ base: markdownLanguage, extensions: [Highlight], codeLanguages, addKeymap: false }),
      syntaxHighlighting(classHighlighter),
      EditorView.lineWrapping,
      tokens,
      EditorView.contentAttributes.of({ "aria-label": "Page text", spellcheck: "true" }),
      livePreview,
    ],
  }),
});
if (!params.get("nofocus")) view.focus();
window.__page = { view };
