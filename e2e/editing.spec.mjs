// Typing, navigation, text integrity and concealment (P0-18 AC2–AC4, AC6).
import { expect, test } from "@playwright/test";
import { cursorIs, host, idle, idOf, open, outline, place, start, stop } from "./helpers.mjs";

let app;
test.afterEach(async () => {
  await stop(app);
  app = null;
});

/** The outline as a newly opened window sees it: the server's tree. */
async function fresh(page) {
  const other = await page.context().newPage();
  await open(other, app);
  const text = await outline(other);
  await other.close();
  return text;
}

test.describe("AC2 typing", () => {
  test("typing keeps order", async ({ page }) => {
    app = await start({ seed: "- one\n- two\n- three" });
    await open(page, app);
    await place(page, await idOf(page, "one"), 3);
    // No waits between keys: ArrowRight at the end and ArrowLeft at the start
    // are round trips, and the keys behind them queue and replay in order.
    await page.keyboard.type("A");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.type("B");
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.type("C");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Home");
    await page.keyboard.type("D");
    expect(await outline(page)).toBe("oneAC\nBtwo\nDthree");
    expect(await fresh(page)).toBe("oneAC\nBtwo\nDthree");
  });

  test("typing shows at once", async ({ page }) => {
    app = await start({ seed: "- Alpha" });
    await open(page, app);
    const a = await idOf(page, "Alpha");
    await place(page, a, 5);
    // The text is in the DOM in the same task as the key: no reply needed.
    const shown = await page.evaluate(() => {
      const h = document.querySelector(".row[data-focus] > .text");
      document.execCommand("insertText", false, "!");
      return h.textContent;
    });
    expect(shown).toBe("Alpha!");
  });

  test("stale state keeps local text", async ({ page }) => {
    app = await start({ seed: "- Alpha\n- Beta" });
    await open(page, app);
    const a = await idOf(page, "Alpha");
    await place(page, a, 5);
    await page.keyboard.type("x");
    await idle(page);
    // The server builds a state now, before it sees "y"; hold it back until
    // "y" is typed, then deliver it.
    await page.evaluate(() => window.__hyphal.hold());
    await page.evaluate(() => window.__hyphal.send({ t: "window", start: 0, count: 200 }));
    await expect.poll(() => page.evaluate(() => window.__hyphal.heldCount())).toBeGreaterThan(0);
    await page.keyboard.type("y");
    await page.evaluate(() => window.__hyphal.release(1));
    const st = await page.evaluate(() => window.__hyphal.state());
    expect(st.textAck).toBeLessThan(2);
    await expect(host(page, a)).toHaveText("Alphaxy");
    await page.evaluate(() => window.__hyphal.release());
    await expect(host(page, a)).toHaveText("Alphaxy");
    await cursorIs(page, a, 7);
    expect(await fresh(page)).toBe("Alphaxy\nBeta");
  });
});

test.describe("AC3 navigation", () => {
  const long = "w ".repeat(150).trim();

  test("navigation at stable column", async ({ page }) => {
    app = await start({ seed: `- abcdefghij\n- ${long}\n- abcdefghij` });
    await open(page, app);
    const [a, b, c] = [await idOf(page, "abcdefghij"), await idOf(page, long), null];
    await place(page, a, 6);
    const x0 = await page.evaluate(() => getSelection().getRangeAt(0).getClientRects()[0]?.left);
    await page.keyboard.press("ArrowDown");
    await idle(page);
    expect(await page.evaluate(() => window.__hyphal.state().cursor)).toBe(b);
    const first = await page.evaluate(() => window.__hyphal.caret().off);
    expect(first).toBeLessThan(20); // the first visual line of the wrapped bullet
    await page.keyboard.press("ArrowDown");
    await idle(page);
    expect(await page.evaluate(() => window.__hyphal.state().cursor)).toBe(b);
    const second = await page.evaluate(() => window.__hyphal.caret().off);
    expect(second).toBeGreaterThan(first);
    let lines = 2;
    while ((await page.evaluate(() => window.__hyphal.state().cursor)) === b && lines < 30) {
      await page.keyboard.press("ArrowDown");
      await idle(page);
      lines++;
    }
    expect(lines).toBeGreaterThan(3); // it really wrapped
    const st = await page.evaluate(() => window.__hyphal.state());
    expect(st.cursor).not.toBe(b);
    expect(st.rows.find((r) => r.id === st.cursor).text).toBe("abcdefghij");
    const x1 = await page.evaluate(() => getSelection().getRangeAt(0).getClientRects()[0]?.left);
    expect(Math.abs(x1 - x0)).toBeLessThan(10);
    void c;
  });

  test("Left and Right cross bullet edges", async ({ page }) => {
    app = await start({ seed: "- Alpha\n- Beta" });
    await open(page, app);
    const [a, b] = [await idOf(page, "Alpha"), await idOf(page, "Beta")];
    await place(page, b, 0);
    await page.keyboard.press("ArrowLeft");
    await cursorIs(page, a, 5);
    await page.keyboard.press("ArrowRight");
    await cursorIs(page, b, 0);
  });

  test("click to place the caret", async ({ page }) => {
    app = await start({ seed: "- Alpha\n- Beta" });
    await open(page, app);
    const [a, b] = [await idOf(page, "Alpha"), await idOf(page, "Beta")];
    await place(page, a, 0);
    const box = await host(page, b).boundingBox();
    await page.mouse.click(box.x + 1, box.y + box.height / 2);
    await cursorIs(page, b, 0);
  });

  test("clicking concealed text maps to the source offset", async ({ page }) => {
    app = await start({ seed: "- Alpha\n- **bold** end" });
    await open(page, app);
    const [a, b] = [await idOf(page, "Alpha"), await idOf(page, "**bold** end")];
    await place(page, a, 0);
    // Click just after "end", the last concealed character.
    const box = await host(page, b).locator(":scope").evaluate((el) => {
      const r = document.createRange();
      const t = el.lastChild;
      r.setStart(t, t.length - 1);
      r.setEnd(t, t.length);
      const q = r.getBoundingClientRect();
      return { x: q.right - 1, y: q.top + q.height / 2 };
    });
    await page.mouse.click(box.x, box.y);
    await cursorIs(page, b, "**bold** end".length);
  });
});

test.describe("AC4 text integrity", () => {
  test("text integrity", async ({ page }) => {
    app = await start({ seed: "- start" });
    await open(page, app);
    const id = await idOf(page, "start");
    await place(page, id, 5);
    await page.keyboard.insertText("a😀éz");
    await idle(page);
    await page.keyboard.press("Home");
    for (let i = 0; i < 7; i++) await page.keyboard.press("ArrowRight"); // "start", "a", then 😀 as one grapheme
    await page.keyboard.type("|");
    await expect(host(page, id)).toHaveText("starta😀|éz");
    await page.keyboard.press("Backspace");
    await page.keyboard.press("Backspace"); // the whole emoji
    await expect(host(page, id)).toHaveText("startaéz");
    await page.keyboard.press("ArrowRight"); // over e and its combining mark
    await page.keyboard.type("|");
    await expect(host(page, id)).toHaveText("startaé|z");
    await idle(page);
    expect(await fresh(page)).toBe("startaé|z");
    // The server's cursor is in UTF-16 units, as the browser counts.
    expect(await page.evaluate(() => window.__hyphal.state().off)).toBe(9);
    await place(page, id, 9);
    await cursorIs(page, id, 9);
  });

  test("composition updates only on commit", async ({ page, browserName }) => {
    test.skip(browserName !== "chromium", "CDP composition events are Chromium only");
    app = await start({ seed: "- Hi \n- B" });
    await open(page, app);
    const id = await idOf(page, "Hi ");
    await place(page, id, 3);
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Input.imeSetComposition", { text: "に", selectionStart: 1, selectionEnd: 1 });
    await cdp.send("Input.imeSetComposition", { text: "にほ", selectionStart: 2, selectionEnd: 2 });
    // A server state during composition waits for its end.
    await page.evaluate(() => window.__hyphal.send({ t: "window", start: 0, count: 200 }));
    await cdp.send("Input.imeSetComposition", { text: "にほん", selectionStart: 3, selectionEnd: 3 });
    expect(await page.evaluate(() => window.__hyphal.state().textAck)).toBe(0);
    await cdp.send("Input.insertText", { text: "日本" });
    await expect.poll(() => fresh(page)).toBe("Hi 日本\nB");
    await expect(host(page, id)).toHaveText("Hi 日本");
  });
});

test.describe("AC6 concealment", () => {
  test("concealment", async ({ page }) => {
    app = await start({ seed: "- **bold** text\n- other" });
    await open(page, app);
    const b = await idOf(page, "**bold** text");
    await place(page, await idOf(page, "other"), 0);
    await expect(host(page, b)).toHaveText("bold text");
    const weight = await host(page, b).locator(".m-bold").evaluate((el) => getComputedStyle(el).fontWeight);
    expect(Number(weight)).toBeGreaterThanOrEqual(600);
    await place(page, b, 0);
    await expect(host(page, b)).toHaveText("**bold** text");
    expect(await host(page, b).locator(".m-bold").count()).toBe(0);
    await expect.poll(() => page.evaluate(() => window.__hyphal.markers())).toEqual([[0, 2], [6, 8]]);
    // Typing keeps the ranges on the markers.
    await page.keyboard.press("End");
    await page.keyboard.type(" *it*");
    await idle(page);
    await expect.poll(() => page.evaluate(() => window.__hyphal.markers())).toEqual([[0, 2], [6, 8], [14, 15], [17, 18]]);
  });

  test("bullet text stays text", async ({ page }) => {
    app = await start({ seed: "- <b>not bold</b> <script>x</script>\n- other" });
    await open(page, app);
    const id = await idOf(page, "<b>not bold</b> <script>x</script>");
    await place(page, await idOf(page, "other"), 0);
    await expect(host(page, id)).toHaveText("<b>not bold</b> <script>x</script>");
    expect(await host(page, id).locator("b, script").count()).toBe(0);
  });
});
