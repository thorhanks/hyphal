import { test, expect } from "@playwright/test";

const mod = process.platform === "darwin" ? "Meta" : "Control";

async function open(page, doc) {
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
  page.errors = errors;
  await page.goto(doc === undefined ? "/" : `/?doc=${encodeURIComponent(doc)}`);
  await page.waitForFunction(() => window.__page);
  return page;
}
const source = (page) => page.evaluate(() => __page.view.state.doc.toString());
const shown = (page) => page.evaluate(() => __page.view.contentDOM.innerText);
const caret = (page) => page.evaluate(() => __page.view.state.selection.main.head);
const place = (page, needle, extra = 0) => page.evaluate(([n, e]) => {
  const pos = __page.view.state.doc.toString().indexOf(n) + e;
  __page.view.dispatch({ selection: { anchor: pos } });
  __page.view.focus();
  return pos;
}, [needle, extra]);
const lineOf = (page, text) => page.locator(".cm-line", { hasText: text }).first();

test.afterEach(async ({ page }) => {
  expect(page.errors ?? []).toEqual([]);
});

test("rendered away from the cursor, source on the cursor's line", async ({ page }) => {
  await open(page, "# Title\n\nSome **bold** word\n\nOther *italic* word\n");
  await place(page, "Other");
  let text = await shown(page);
  expect(text).toContain("Title");
  expect(text).not.toContain("# Title");
  expect(text).toContain("Some bold word");
  expect(text).toContain("Other *italic* word");
  await place(page, "Some");
  text = await shown(page);
  expect(text).toContain("Some **bold** word");
  expect(text).toContain("Other italic word");
});

test("clicking rendered text lands on the same word in the source", async ({ page }) => {
  const doc = "First line here\n\nSome **bold** and *italic* words are here\n\nLast line";
  await open(page, doc);
  await place(page, "Last");
  const box = await page.evaluate(() => {
    const walker = document.createTreeWalker(__page.view.contentDOM, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const i = walker.currentNode.data.indexOf("words");
      if (i >= 0) {
        const r = document.createRange();
        r.setStart(walker.currentNode, i + 2);
        r.setEnd(walker.currentNode, i + 3);
        const b = r.getBoundingClientRect();
        return { x: b.left + b.width / 2, y: b.top + b.height / 2 };
      }
    }
    return null;
  });
  await page.mouse.click(box.x, box.y);
  const at = await caret(page);
  const word = doc.indexOf("words");
  expect(at).toBeGreaterThanOrEqual(word);
  expect(at).toBeLessThanOrEqual(word + 5);
  expect(await shown(page)).toContain("Some **bold**");
});

test("typing is free-form and the text is exact", async ({ page }) => {
  await open(page, "One two\n\nNext\n");
  await place(page, "two");
  await page.keyboard.type("**");
  await place(page, "**two", 5);
  await page.keyboard.type("**");
  expect(await source(page)).toBe("One **two**\n\nNext\n");
  await place(page, "Next");
  expect(await shown(page)).toContain("One two");
});

test("arrows cross every block and each one reveals in turn", async ({ page }) => {
  await open(page, "# Head\n\nPara\n\n- item\n\n> quote\n\n---\n\nEnd\n");
  await place(page, "# Head", 1);
  const seen = [];
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press("ArrowDown");
    seen.push(await page.evaluate(() => {
      const s = __page.view.state;
      return s.doc.lineAt(s.selection.main.head).text;
    }));
  }
  expect(seen).toEqual(["", "Para", "", "- item", "", "> quote", "", "---", "", "End"]);
  expect(await source(page)).toBe("# Head\n\nPara\n\n- item\n\n> quote\n\n---\n\nEnd\n");
});

test("a blank line splits and a fence swallows what follows, without errors", async ({ page }) => {
  await open(page, "One two\n\nAfter one\nAfter two\n");
  await place(page, "two");
  await page.keyboard.press("Enter");
  await page.keyboard.press("Enter");
  expect(await source(page)).toBe("One \n\ntwo\n\nAfter one\nAfter two\n");
  await place(page, "After one");
  await page.keyboard.type("```");
  await page.keyboard.press("Enter");
  await expect(page.locator(".lp-fence").first()).toBeVisible();
  expect(await page.locator(".lp-fence", { hasText: "After two" }).count()).toBe(1);
  await page.keyboard.press(`${mod}+z`);
  await page.keyboard.press(`${mod}+z`);
  await expect(page.locator(".lp-fence")).toHaveCount(0);
});

test("keyboard selection crosses blocks, and deleting it is exact", async ({ page }) => {
  await open(page, "# Head one\n\nPara **two** here\n\n- item three\n\nLast\n");
  await place(page, "one");
  for (let i = 0; i < 4; i++) await page.keyboard.press("Shift+ArrowDown");
  const sel = await page.evaluate(() => {
    const s = __page.view.state;
    return s.sliceDoc(s.selection.main.from, s.selection.main.to);
  });
  expect(sel.startsWith("one\n\nPara **two** here\n\n")).toBe(true);
  await page.keyboard.press("Backspace");
  const after = await source(page);
  expect(after.startsWith("# Head ")).toBe(true);
  expect(after).not.toContain("Para");
  await page.keyboard.press(`${mod}+z`);
  expect(await source(page)).toBe("# Head one\n\nPara **two** here\n\n- item three\n\nLast\n");
});

test("dragging across blocks selects without shifting the text", async ({ page }) => {
  await open(page, "Start here\n\nSome **bold** middle\n\n> quoted line\n\nEnd there\n\nOutside\n");
  await place(page, "Outside");
  const pos = (t) => lineOf(page, t).boundingBox();
  const a = await pos("Start here");
  const b = await pos("End there");
  const quoteBefore = await pos("quoted line");
  await page.mouse.move(a.x + 30, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(a.x + 30, (a.y + b.y) / 2, { steps: 4 });
  const quoteDuring = await pos("quoted line");
  expect(Math.abs(quoteDuring.y - quoteBefore.y)).toBeLessThan(1);
  expect(await shown(page)).toContain("Some bold middle");
  await page.mouse.move(b.x + 30, b.y + b.height / 2, { steps: 4 });
  await page.mouse.up();
  const sel = await page.evaluate(() => {
    const s = __page.view.state;
    return s.sliceDoc(s.selection.main.from, s.selection.main.to);
  });
  expect(sel).toContain("Some **bold** middle\n\n> quoted line\n\nE");
});

test("copying a selection across blocks gives raw Markdown", async ({ page, context, browserName }) => {
  test.skip(browserName !== "chromium", "clipboard permission API is Chromium-only in Playwright");
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await open(page, "# Head\n\nSome **bold** text\n\nEnd\n");
  await place(page, "End");
  await page.evaluate(() => __page.view.dispatch({ selection: { anchor: 0, head: __page.view.state.doc.toString().indexOf("End") } }));
  await page.keyboard.press(`${mod}+c`);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("# Head\n\nSome **bold** text\n\n");
});

test("undo and redo treat the page as one document", async ({ page }) => {
  const doc = "# Head\n\nPara\n\n- item\n";
  await open(page, doc);
  await place(page, "Para");
  await page.keyboard.type("New ");
  await page.keyboard.press(`${mod}+a`);
  await page.keyboard.press("Backspace");
  expect(await source(page)).toBe("");
  await page.keyboard.press(`${mod}+z`);
  expect(await source(page)).toContain("New Para");
  await page.keyboard.press(`${mod}+z`);
  expect(await source(page)).toBe(doc);
  await page.keyboard.press(process.platform === "darwin" ? "Meta+Shift+z" : "Control+y");
  expect(await source(page)).toContain("New Para");
});

test("a table renders, opens to source by arrow and click, and takes edits", async ({ page }) => {
  const doc = "Before\n\n| A | B |\n| --- | --- |\n| **one** | two |\n\nAfter\n";
  await open(page, doc);
  await place(page, "After");
  await expect(page.locator("table.lp-table")).toHaveCount(1);
  await expect(page.locator("table.lp-table strong")).toHaveText("one");
  await place(page, "Before");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("table.lp-table")).toHaveCount(0);
  expect(await page.evaluate(() => {
    const s = __page.view.state;
    return s.doc.lineAt(s.selection.main.head).text;
  })).toBe("| A | B |");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("table.lp-table")).toHaveCount(1);
  await page.locator("table.lp-table td", { hasText: "two" }).click();
  await expect(page.locator("table.lp-table")).toHaveCount(0);
  const at = await caret(page);
  expect(doc.slice(at - 3, at + 8)).toContain("two");
  await page.keyboard.type("X");
  expect(await source(page)).toContain("X");
  await place(page, "After");
  await expect(page.locator("table.lp-table td").nth(1)).toContainText("X");
});

test("a code block hides its fences until entered and highlights tokens", async ({ page }) => {
  await open(page, "Before\n\n```go\nfunc main() {}\n```\n\nAfter\n");
  await place(page, "After");
  await expect(page.locator(".lp-fence .tok-keyword").first()).toHaveText("func");
  const collapsed = await page.locator(".lp-collapsed").count();
  expect(collapsed).toBe(2);
  await place(page, "Before");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".lp-collapsed")).toHaveCount(0);
  expect(await shown(page)).toContain("```go");
});

test("lists render, Enter continues them, and a task box toggles with undo", async ({ page }) => {
  await open(page, "- one\n- [ ] task\n\nEnd\n");
  await place(page, "End");
  await expect(page.locator(".lp-bullet")).toHaveCount(1);
  await page.locator(".lp-check").click();
  expect(await source(page)).toContain("- [x] task");
  await page.keyboard.press(`${mod}+z`);
  expect(await source(page)).toContain("- [ ] task");
  await place(page, "one", 3);
  await page.keyboard.press("Enter");
  await page.keyboard.type("two");
  expect(await source(page)).toContain("- one\n- two\n");
});

test("raw HTML stays literal text and unsafe links are inert", async ({ page }) => {
  await open(page, `<script>window.__pwned = 1</script>\n\n<img src=x onerror="window.__pwned = 2">\n\n[bad](javascript:window.__pwned=3) and [good](https://example.com)\n\nEnd\n`);
  await place(page, "End");
  expect(await page.evaluate(() => window.__pwned)).toBeUndefined();
  await expect(page.locator(".cm-content script, .cm-content img:not(.cm-widgetBuffer)")).toHaveCount(0);
  await expect(page.locator("[data-href]")).toHaveCount(1);
  await expect(page.locator("[data-href]")).toHaveAttribute("data-href", "https://example.com");
});

test("emoji and combining marks don't corrupt text next to markup", async ({ page }) => {
  await open(page, "A **b** c\n\nEnd\n");
  await place(page, " c", 2);
  await page.keyboard.insertText(" 👩‍💻 é");
  expect(await source(page)).toBe("A **b** c 👩‍💻 é\n\nEnd\n");
  await place(page, "End");
  expect(await shown(page)).toContain("A b c 👩‍💻 é");
});

test("IME composition commits once on a line with markup", async ({ page, browserName }) => {
  test.skip(browserName !== "chromium", "CDP composition is Chromium-only");
  await open(page, "Some **bold** word\n\nEnd\n");
  await place(page, " word", 5);
  const cdp = await page.context().newCDPSession(page);
  const compose = (text) => cdp.send("Input.imeSetComposition", { text, selectionStart: text.length, selectionEnd: text.length });
  await compose(" に");
  await compose(" にほ");
  await compose(" にほん");
  await cdp.send("Input.insertText", { text: " 日本" });
  await page.keyboard.type("!");
  expect(await source(page)).toBe("Some **bold** word 日本!\n\nEnd\n");
});

test("theme tokens restyle the page with no JS", async ({ page }) => {
  await open(page, "# Head\n\nSome **bold** [link](https://example.com)\n\nEnd\n");
  await place(page, "End");
  const color = () => page.locator(".lp-link").first().evaluate((el) => getComputedStyle(el).color);
  const before = await color();
  await page.addStyleTag({ content: ":root { --accent: rgb(1, 2, 3); --bg: rgb(4, 5, 6); }" });
  expect(await color()).toBe("rgb(1, 2, 3)");
  expect(before).not.toBe("rgb(1, 2, 3)");
  expect(await page.locator(".cm-editor").evaluate((el) => getComputedStyle(el).backgroundColor)).toBe("rgb(4, 5, 6)");
  await page.addStyleTag({ content: ":root { --font-text: Courier; --column: 30rem; }" });
  expect(await page.locator(".lp-link").first().evaluate((el) => getComputedStyle(el).fontFamily)).toContain("Courier");
  expect(await page.locator(".cm-content").evaluate((el) => el.getBoundingClientRect().width)).toBeLessThan(520);
});

test("the caret's line doesn't move on screen when a neighbor reveals", async ({ page }) => {
  await open(page, "# Head\n\nOne **two** three\n\nFour **five** six\n\nEnd\n");
  await place(page, "One");
  const y1 = (await lineOf(page, "Four").boundingBox()).y;
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  const y2 = (await lineOf(page, "Four").boundingBox()).y;
  expect(Math.abs(y1 - y2)).toBeLessThan(1);
});

test("measure long page", async ({ page, browserName }, info) => {
  test.setTimeout(180_000);
  const results = {};
  for (const lines of [2000, 20000]) {
    const t0 = Date.now();
    await page.goto(`/?long=${lines}`);
    await page.waitForFunction(() => window.__page);
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => setTimeout(r))));
    const load = Date.now() - t0;
    const metrics = await page.evaluate(async () => {
      const v = __page.view;
      const frame = () => new Promise((r) => requestAnimationFrame(() => setTimeout(r)));
      const pct = (a, p) => [...a].sort((x, y) => x - y)[Math.floor(a.length * p)];
      const size = v.state.doc.length;
      const mid = v.state.doc.lineAt(Math.floor(size / 2)).from;
      v.dispatch({ selection: { anchor: mid }, scrollIntoView: true });
      await frame(); await frame();
      const type = [], move = [], jump = [];
      for (let i = 0; i < 60; i++) {
        const t = performance.now();
        const at = v.state.selection.main.head;
        v.dispatch({ changes: { from: at, insert: "x" }, selection: { anchor: at + 1 }, userEvent: "input.type" });
        await frame();
        type.push(performance.now() - t);
      }
      for (let i = 0; i < 40; i++) {
        const t = performance.now();
        const next = v.state.doc.lineAt(v.state.selection.main.head).to + 1;
        v.dispatch({ selection: { anchor: Math.min(next, size) }, scrollIntoView: true });
        await frame();
        move.push(performance.now() - t);
      }
      for (let i = 0; i < 10; i++) {
        const t = performance.now();
        v.dispatch({ selection: { anchor: Math.floor(size * ((i * 7) % 10) / 10) }, scrollIntoView: true });
        await frame();
        jump.push(performance.now() - t);
      }
      return { bytes: size, type: [pct(type, .5), pct(type, .95)], move: [pct(move, .5), pct(move, .95)], jump: [pct(jump, .5), pct(jump, .95)] };
    });
    results[lines] = { load, ...metrics };
  }
  console.log("MEASURE", browserName, JSON.stringify(results));
  info.attach("measure", { body: JSON.stringify(results) });
});
