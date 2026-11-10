// Scale and security in a real browser (P0-18 AC5, AC1).
import { expect, test } from "@playwright/test";
import { idle, open, place, start, stop, ticketURL } from "./helpers.mjs";

let app;
test.afterEach(async () => {
  await stop(app);
  app = null;
});

test.describe("AC5 scale", () => {
  test("scroll away and return", async ({ page }) => {
    test.setTimeout(120_000);
    // 100,000 nodes, 10,000 visible: each top-level row hides nine children.
    const lines = [];
    for (let i = 0; i < 10_000; i++) {
      lines.push(`- Row ${i} {collapsed}`);
      for (let j = 0; j < 9; j++) lines.push(`  - Kid ${i}.${j}`);
    }
    app = await start({ seed: lines.join("\n") });
    await open(page, app);
    const st = await page.evaluate(() => window.__hyphal.state());
    expect(st.total).toBe(10_000);
    const first = st.cursor;
    await place(page, first, 0);
    await page.evaluate(() => {
      document.querySelector("hyphal-outline").scrollTop = 100_000;
    });
    await expect.poll(() => page.evaluate(() => window.__hyphal.state().start)).toBeGreaterThan(1000);
    expect(await page.locator(`.row[data-id="${first}"]`).count()).toBe(0);
    await page.keyboard.type("x");
    await idle(page);
    await expect(page.locator(".row[data-focus]")).toBeInViewport();
    await expect(page.locator(".row[data-focus] > .text")).toBeFocused();
    await expect(page.locator(".row[data-focus] > .text")).toHaveText("xRow 0");
  });
});

test.describe("AC1 security in a real browser", () => {
  test("the ticket leaves the address bar and the cookie is HttpOnly", async ({ page, context }) => {
    app = await start({ seed: "- Alpha" });
    await open(page, app);
    expect(page.url()).toBe(`${app.origin}/`);
    expect(await page.evaluate(() => document.cookie)).toBe("");
    const [c] = await context.cookies();
    expect(c.name).toBe(`hyphal_${new URL(app.origin).port}`);
    expect(c.httpOnly).toBe(true);
    expect(c.sameSite).toBe("Strict");
  });

  test("a used ticket and a page without the cookie are refused", async ({ browser }) => {
    app = await start({ seed: "- Alpha" });
    const url = await ticketURL(app);
    const first = await browser.newContext();
    expect((await (await first.newPage()).goto(url))?.status()).toBe(200);
    const second = await browser.newContext();
    const page = await second.newPage();
    expect((await page.goto(url))?.status()).toBe(403);
    expect((await page.goto(`${app.origin}/`))?.status()).toBe(403);
    await expect(page.locator("body")).toContainText("Run hyphal");
    await first.close();
    await second.close();
  });
});
