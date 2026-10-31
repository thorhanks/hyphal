import { expect, test } from "@playwright/test";
import { App, cursorIs, cursorText, footer, open, outline, place, start, idle } from "./helpers";

let app: App;
test.afterEach(() => app?.proc.kill());

const base = "a||Alpha\nb||Beta\n";

interface Case {
  name: string; seed: string; at: [string, number]; key: string; want: string;
  cursorText?: string; cursorOff?: number; hint?: RegExp; select?: [number];
}

const cases: Case[] = [
  // §5.2 Enter
  { name: "Enter at start inserts above and stays on the text", seed: base, at: ["a", 0], key: "Enter", want: "\nAlpha\nBeta", cursorText: "Alpha", cursorOff: 0 },
  { name: "Enter mid-text splits; children stay with the first half", seed: "a||Alpha\n  k||Kid\n", at: ["a", 2], key: "Enter", want: "Al\n  Kid\npha", cursorText: "pha", cursorOff: 0 },
  { name: "Enter at end of expanded parent makes a first child", seed: "a||Alpha\n  k||Kid\n", at: ["a", 5], key: "Enter", want: "Alpha\n  \n  Kid", cursorText: "", cursorOff: 0 },
  { name: "Enter at end of collapsed parent makes a sibling", seed: "a|c|Alpha\n  k||Kid\nb||Beta\n", at: ["a", 5], key: "Enter", want: "Alpha [c]\n\nBeta", cursorText: "", cursorOff: 0 },
  { name: "Enter at end of a leaf makes a sibling", seed: base, at: ["a", 5], key: "Enter", want: "Alpha\n\nBeta", cursorText: "", cursorOff: 0 },
  { name: "Enter on a nested empty bullet outdents it", seed: "a||Alpha\n  e||\n", at: ["e", 0], key: "Enter", want: "Alpha\n", cursorText: "", cursorOff: 0 },
  { name: "Enter on a top-level empty bullet adds a sibling", seed: "e||\nb||Beta\n", at: ["e", 0], key: "Enter", want: "\n\nBeta", cursorText: "", cursorOff: 0 },
  // §5.3 Backspace
  { name: "Backspace at the first bullet is a no-op", seed: base, at: ["a", 0], key: "Backspace", want: "Alpha\nBeta", cursorText: "Alpha", cursorOff: 0 },
  { name: "Backspace deletes an empty leaf", seed: "a||Alpha\ne||\n", at: ["e", 0], key: "Backspace", want: "Alpha", cursorText: "Alpha", cursorOff: 5 },
  { name: "Backspace on an empty bullet with children hints", seed: "a||Alpha\ne||\n  k||Kid\n", at: ["e", 0], key: "Backspace", want: "Alpha\n\n  Kid", hint: /Esc then Delete/ },
  { name: "Backspace with both pages hints", seed: "a|p|Alpha\nb|p|Beta\n", at: ["b", 0], key: "Backspace", want: "Alpha\nBeta", hint: /page/ },
  { name: "Backspace merges a leaf into the previous bullet", seed: base, at: ["b", 0], key: "Backspace", want: "AlphaBeta", cursorText: "AlphaBeta", cursorOff: 5 },
  { name: "Backspace merges and adopts children", seed: "a||Alpha\nb||Beta\n  k||Kid\n", at: ["b", 0], key: "Backspace", want: "AlphaBeta\n  Kid", cursorText: "AlphaBeta", cursorOff: 5 },
  { name: "Backspace with children on both hints", seed: "a|c|Alpha\n  j||Jay\nb||Beta\n  k||Kid\n", at: ["b", 0], key: "Backspace", want: "Alpha [c]\nBeta\n  Kid", hint: /children/ },
  { name: "Backspace merges into the visually previous deeper bullet", seed: "a||Alpha\n  j||Jay\nb||Beta\n", at: ["b", 0], key: "Backspace", want: "Alpha\n  JayBeta", cursorText: "JayBeta", cursorOff: 3 },
  // §5.3a Delete
  { name: "Delete on the last bullet is a no-op", seed: base, at: ["b", 4], key: "Delete", want: "Alpha\nBeta" },
  { name: "Delete before own first child hints", seed: "a||Alpha\n  k||Kid\n", at: ["a", 5], key: "Delete", want: "Alpha\n  Kid", hint: /own child/ },
  { name: "Delete removes an empty next bullet", seed: "a||Alpha\ne||\nb||Beta\n", at: ["a", 5], key: "Delete", want: "Alpha\nBeta", cursorOff: 5 },
  { name: "Delete with both pages hints", seed: "a|p|Alpha\nb|p|Beta\n", at: ["a", 5], key: "Delete", want: "Alpha\nBeta", hint: /page/ },
  { name: "Delete merges the next leaf", seed: base, at: ["a", 5], key: "Delete", want: "AlphaBeta", cursorOff: 5 },
  { name: "Delete merges and adopts children", seed: "a||Alpha\nb||Beta\n  k||Kid\n", at: ["a", 5], key: "Delete", want: "AlphaBeta\n  Kid", cursorOff: 5 },
  // §5.4 Tab
  { name: "Tab on the first sibling is a no-op", seed: base, at: ["a", 2], key: "Tab", want: "Alpha\nBeta", cursorOff: 2 },
  { name: "Tab indents and keeps the caret", seed: base, at: ["b", 2], key: "Tab", want: "Alpha\n  Beta", cursorOff: 2 },
  { name: "Tab under a collapsed sibling expands it", seed: "a|c|Alpha\n  k||Kid\nb||Beta\n", at: ["b", 1], key: "Tab", want: "Alpha\n  Kid\n  Beta", cursorOff: 1 },
  { name: "Shift+Tab outdents without adopting later siblings", seed: "a||Alpha\n  j||Jay\n  k||Kid\n", at: ["j", 1], key: "Shift+Tab", want: "Alpha\n  Kid\nJay", cursorOff: 1 },
  { name: "Shift+Tab at the top level hints", seed: base, at: ["a", 0], key: "Shift+Tab", want: "Alpha\nBeta", hint: /top level/ },
  // §5.4 Move
  { name: "Move down swaps siblings", seed: base, at: ["a", 1], key: "Control+Shift+ArrowDown", want: "Beta\nAlpha", cursorOff: 1 },
  { name: "Move up crosses into the adjacent parent", seed: "a||A\nb||B\n  k||Kid\n", at: ["k", 0], key: "Control+Shift+ArrowUp", want: "A\n  Kid\nB" },
  { name: "Move up out of the first parent goes up a level", seed: "b||B\n  k||Kid\n", at: ["k", 0], key: "Control+Shift+ArrowUp", want: "Kid\nB" },
  { name: "Move down crosses into the next parent", seed: "a||A\n  k||Kid\nb||B\n  j||Jay\n", at: ["k", 0], key: "Control+Shift+ArrowDown", want: "A\nB\n  Kid\n  Jay" },
];

test.describe("AC1 rule tables", () => {
  for (const c of cases) {
    test(c.name, async ({ page }) => {
      app = await start({ seed: c.seed });
      await open(page, app);
      await place(page, c.at[0], c.at[1]);
      await page.keyboard.press(c.key);
      expect(await outline(page)).toBe(c.want);
      if (c.cursorText !== undefined) expect(await cursorText(page)).toBe(c.cursorText);
      if (c.cursorOff !== undefined) {
        await expect.poll(() => page.evaluate(() => (window as any).__spike.caret()?.off)).toBe(c.cursorOff);
      }
      if (c.hint) expect(await footer(page)).toMatch(c.hint);
    });
  }

  test("Enter with a text selection deletes it, then splits", async ({ page }) => {
    app = await start({ seed: "a||Alphabet\n" });
    await open(page, app);
    await place(page, "a", 2);
    await page.keyboard.press("Shift+ArrowRight");
    await page.keyboard.press("Shift+ArrowRight");
    await page.keyboard.press("Enter");
    expect(await outline(page)).toBe("Al\nabet");
  });

  test("Enter in the zoom heading makes a first child and never splits", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n  k||Kid\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await page.keyboard.press("z");
    await idle(page);
    await page.keyboard.press("Enter"); // text editing on Kid
    await place(page, "a", 2);
    await page.keyboard.press("Enter");
    expect(await outline(page)).toBe("# Alpha\n\nKid");
  });

  test("Backspace with a text selection deletes only that text", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "b", 0);
    await page.keyboard.press("Shift+ArrowRight");
    await page.keyboard.press("Backspace");
    expect(await outline(page)).toBe("Alpha\neta");
  });

  test("Backspace into the zoom heading never merges", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n  k||Kid\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await page.keyboard.press("z");
    await idle(page);
    await page.keyboard.press("Enter");
    await place(page, "k", 0);
    await page.keyboard.press("Backspace");
    expect(await outline(page)).toBe("# Alpha\nKid");
  });

  test("fast typing across Enter keeps every character in order", async ({ page }) => {
    app = await start({ seed: "a||\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.type("one", { delay: 0 });
    await page.keyboard.press("Enter");
    await page.keyboard.type("two", { delay: 0 });
    await page.keyboard.press("Enter");
    await page.keyboard.press("Tab");
    await page.keyboard.type("three", { delay: 0 });
    expect(await outline(page)).toBe("one\ntwo\n  three");
  });
});

test.describe("AC2 navigation and selection", () => {
  const long = "w ".repeat(150).trim();

  test("Up and Down move through wrapped lines, then bullets, at a stable column", async ({ page }) => {
    app = await start({ seed: `a||abcdefghij\nb||${long}\nc||abcdefghij\n` });
    await open(page, app);
    await place(page, "a", 6);
    const x0 = await page.evaluate(() => getSelection()!.getRangeAt(0).getClientRects()[0]?.left);
    await page.keyboard.press("ArrowDown");
    await cursorIs(page, "b", await page.evaluate(() => (window as any).__spike.caret().off));
    expect(await page.evaluate(() => (window as any).__spike.state().cursor)).toBe("b");
    const first = (await page.evaluate(() => (window as any).__spike.caret().off)) as number;
    await page.keyboard.press("ArrowDown");
    await idle(page);
    const second = (await page.evaluate(() => (window as any).__spike.caret().off)) as number;
    expect(await page.evaluate(() => (window as any).__spike.state().cursor)).toBe("b");
    expect(second).toBeGreaterThan(first);
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press("ArrowDown");
      await idle(page);
    }
    expect(await page.evaluate(() => (window as any).__spike.state().cursor)).toBe("c");
    const x1 = await page.evaluate(() => getSelection()!.getRangeAt(0).getClientRects()[0]?.left);
    expect(Math.abs(x1 - x0)).toBeLessThan(10);
  });

  test("Left and Right cross bullet edges", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "b", 0);
    await page.keyboard.press("ArrowLeft");
    await cursorIs(page, "a", 5);
    await page.keyboard.press("ArrowRight");
    await cursorIs(page, "b", 0);
  });

  test("Shift+arrows stay inside one bullet", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 3);
    await page.keyboard.press("Shift+ArrowDown");
    await page.keyboard.press("Shift+ArrowDown");
    const inRow = await page.evaluate(() => {
      const s = getSelection()!;
      const host = document.querySelector('.row[data-id="a"] > .text')!;
      return host.contains(s.anchorNode) && host.contains(s.focusNode);
    });
    expect(inRow).toBe(true);
    await page.keyboard.type("!");
    expect((await outline(page)).split("\n")).toHaveLength(2);
  });

  test("Esc enters block selection; Up, Down and Shift extend; Enter returns", async ({ page }) => {
    app = await start({ seed: "a||A\nb||B\nc||C\n  k||Kid\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("ArrowDown");
    await idle(page);
    await expect(page.locator(".row.block")).toHaveText(/B/);
    await page.keyboard.press("Shift+ArrowDown");
    await idle(page);
    await expect(page.locator(".row.block")).toHaveCount(2);
    await page.keyboard.press("Shift+ArrowDown");
    await idle(page);
    expect(await footer(page)).toMatch(/siblings only/);
    await page.keyboard.press("x");
    expect(await outline(page)).toBe("A\nB [x]\nC [x]\n  Kid");
    await page.keyboard.press("Enter");
    await idle(page);
    await expect(page.locator(".row.block")).toHaveCount(0);
    await cursorIs(page, "c", 1);
  });

  test("typed text after Esc never runs a surprise command", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("q");
    expect(await footer(page)).toMatch(/does nothing/);
    expect(await outline(page)).toBe("Alpha\nBeta");
  });

  test("clicking places the caret in the clicked bullet", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 0);
    const box = (await page.locator('.row[data-id="b"] > .text').boundingBox())!;
    await page.mouse.click(box.x + 1, box.y + box.height / 2);
    await cursorIs(page, "b", 0);
  });
});

test.describe("AC4 undo", () => {
  test("typing is one step per node and restores the caret", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 5);
    await page.keyboard.type(" one");
    await idle(page);
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toBe("Alpha\nBeta");
    await cursorIs(page, "a", 5);
    await page.keyboard.press("Control+y");
    expect(await outline(page)).toBe("Alpha one\nBeta");
  });

  test("structure, text and redo clearing", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 2);
    await page.keyboard.press("Enter");
    expect(await outline(page)).toBe("Al\npha\nBeta");
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toBe("Alpha\nBeta");
    await cursorIs(page, "a", 2);
    await page.keyboard.type("x");
    await idle(page);
    await page.keyboard.press("Control+y");
    expect(await footer(page)).toMatch(/Nothing to redo/);
    expect(await outline(page)).toBe("Alxpha\nBeta");
  });

  test("the browser's native undo never fires", async ({ page }) => {
    app = await start({ seed: base });
    await open(page, app);
    await place(page, "a", 5);
    await page.keyboard.type("zz");
    await idle(page);
    await page.keyboard.press("Enter");
    await page.keyboard.type("q");
    await idle(page);
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press("Control+z");
      await idle(page);
    }
    expect(await outline(page)).toBe("Alpha\nBeta");
    await page.evaluate(() => {
      const h = document.querySelector<HTMLElement>('.row.focus > .text')!;
      h.dispatchEvent(new InputEvent("beforeinput", { inputType: "historyRedo", cancelable: true, bubbles: true }));
    });
    expect(await outline(page)).toBe("Alphazz\nBeta");
  });

  test("delete to Trash shows the message and undo brings the subtree back", async ({ page }) => {
    app = await start({ seed: "a||A\n  k||Kid\nb||B\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("d");
    expect(await footer(page)).toMatch(/Moved "A" and 1 descendants to Trash/);
    expect(await outline(page)).toBe("B");
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toBe("A\n  Kid\nB");
  });

  test("undoing outside the zoom zooms to show it", async ({ page }) => {
    app = await start({ seed: "a||A\n  k||Kid\nb||B\n" });
    await open(page, app);
    await place(page, "b", 1);
    await page.keyboard.type("!");
    await idle(page);
    await page.keyboard.press("ArrowUp");
    await idle(page);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("z");
    await idle(page);
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toContain("B");
    expect(await footer(page)).toMatch(/Zoomed to show/);
  });
});

test.describe("AC5 text integrity", () => {
  test("emoji and combining marks split on grapheme offsets the browser reports", async ({ page }) => {
    app = await start({ seed: "a||\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.insertText("a😀éz");
    await idle(page);
    await page.keyboard.press("Home");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Enter");
    expect(await outline(page)).toBe("a😀\néz");
    await page.keyboard.press("Delete");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Backspace");
    const text = await outline(page);
    expect(text).not.toContain("�");
  });

  test("Backspace removes a whole emoji", async ({ page }) => {
    app = await start({ seed: "a||x😀\n" });
    await open(page, app);
    await place(page, "a", 3);
    await page.keyboard.press("Backspace");
    expect(await outline(page)).toBe("x");
  });

  test("IME composition updates the row only when it commits", async ({ page, browserName }) => {
    test.skip(browserName !== "chromium", "CDP composition events are Chromium only");
    app = await start({ seed: "a||Hi \nb||B\n" });
    await open(page, app);
    await place(page, "a", 3);
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Input.imeSetComposition", { text: "に", selectionStart: 1, selectionEnd: 1 });
    await cdp.send("Input.imeSetComposition", { text: "にほ", selectionStart: 2, selectionEnd: 2 });
    await cdp.send("Input.imeSetComposition", { text: "にほん", selectionStart: 3, selectionEnd: 3 });
    expect(await page.evaluate(() => (window as any).__spike.state().rows[0].text)).toBe("Hi ");
    await cdp.send("Input.insertText", { text: "日本" });
    await expect.poll(() => outline(page)).toBe("Hi 日本\nB");
    await page.keyboard.press("Enter");
    await page.keyboard.type("x");
    expect(await outline(page)).toBe("Hi 日本\nx\nB");
    await page.keyboard.press("Control+z");
    await page.keyboard.press("Control+z");
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toBe("Hi \nB");
  });

  test("a server update during composition does not break it", async ({ page, browserName }) => {
    test.skip(browserName !== "chromium", "CDP composition events are Chromium only");
    app = await start({ seed: "a||Hi\nb||B\n" });
    await open(page, app);
    await place(page, "a", 2);
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Input.imeSetComposition", { text: "か", selectionStart: 1, selectionEnd: 1 });
    await page.evaluate(() => (window as any).__spike.setCursor("a", 2));
    await cdp.send("Input.insertText", { text: "仮" });
    await expect.poll(() => outline(page)).toBe("Hi仮\nB");
  });
});
