import { writeFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { App, cursorText, footer, idle, open, outline, place, start } from "./helpers";

let app: App;
test.afterEach(() => app?.proc.kill());

test.describe("AC3 keys", () => {
  // Synthetic input bypasses browser-level shortcuts, so this only proves the app
  // handles a key that arrives. assets/keyprobe.html is the check in a real window.
  test("§6.4 chords reach the app under automation", async ({ page }) => {
    app = await start({ seed: "a||Alpha beta\n" });
    await open(page, app);
    await place(page, "a", 10);
    await page.keyboard.press("Control+w");
    expect(await outline(page)).toBe("Alpha ");
    for (const [key, text] of [["Control+s", /Saved/], ["Control+p", /Command palette/], ["Control+k", /Jump/], ["Control+q", /Quit/], ["F1", /Help/]] as const) {
      await page.keyboard.press(key);
      expect(await footer(page)).toMatch(text);
    }
  });

  test("Ctrl+U and Ctrl+A / Ctrl+E edit by visual line", async ({ page }) => {
    app = await start({ seed: "a||hello world\n" });
    await open(page, app);
    await place(page, "a", 5);
    await page.keyboard.press("Control+a");
    await page.keyboard.type(">");
    await idle(page);
    await page.keyboard.press("Control+e");
    await page.keyboard.type("<");
    expect(await outline(page)).toBe(">hello world<");
    await page.keyboard.press("Control+u");
    expect(await outline(page)).toBe("");
  });
});

test.describe("AC6 scale", () => {
  test("measure §9.4 targets at 100,000 nodes with 10,000 expanded", async ({ page, browserName, playwright }, info) => {
    test.setTimeout(180_000);
    app = await start({ gen: 100_000, expanded: 10_000 });
    const fcp = (p: typeof page) => p.evaluate(() => new Promise<number>((resolve) => {
      new PerformanceObserver((l) => {
        const e = l.getEntries().find((x) => x.name === "first-contentful-paint");
        if (e) resolve(e.startTime);
      }).observe({ type: "paint", buffered: true });
      setTimeout(() => resolve(-1), 3000);
    }));

    // Cold launch: a browser process that isn't running yet, plus the page.
    const t0 = performance.now();
    const coldBrowser = await playwright[browserName].launch();
    const coldPage = await coldBrowser.newPage();
    await coldPage.goto(app.url);
    const coldFcp = await fcp(coldPage);
    const coldLaunch = performance.now() - t0;
    await coldBrowser.close();

    // Warm launch: the browser is already running.
    await page.goto(app.url);
    await page.waitForFunction(() => (window as any).__spike?.state());
    const paint = { firstPaint: await fcp(page), coldFcp, coldLaunch };
    const total = await page.evaluate(() => (window as any).__spike.state().total);
    expect(total).toBeGreaterThanOrEqual(10_000);

    const first = await page.evaluate(() => (window as any).__spike.state().cursor);
    await place(page, first, 0);

    await page.evaluate(() => {
      const w = window as any;
      w.__samples = [];
      document.addEventListener("keydown", (e) => {
        const key = e.key;
        const t0 = performance.now();
        requestAnimationFrame(() => {
          const raf = performance.now();
          setTimeout(() => {
            w.__samples.push({ key, ms: raf - t0, afterPaint: performance.now() - t0 });
          }, 0);
        });
      }, { capture: true });
    });

    // 30 ms between keys is a very fast typist; the first 20 keys warm up the JIT.
    for (let i = 0; i < 320; i++) {
      await page.keyboard.press(i % 8 === 7 ? " " : "a");
      await page.waitForTimeout(30);
    }
    await idle(page);
    const typing = (await page.evaluate(() => (window as any).__samples.splice(0))).slice(20);

    const enter: number[] = [];
    for (let i = 0; i < 50; i++) {
      enter.push(await page.evaluate(() => new Promise<number>((resolve) => {
        const s = (window as any).__spike;
        const before = s.state();
        const t0 = performance.now();
        const h = document.querySelector<HTMLElement>(".row.focus > .text")!;
        h.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
        const tick = () => {
          if (s.state() !== before && s.idle()) requestAnimationFrame(() => resolve(performance.now() - t0));
          else setTimeout(tick, 0);
        };
        tick();
      })));
    }

    await page.keyboard.press("Control+f");
    const filter: number[] = [];
    for (const q of ["m", "my", "myc", "myce", "mycel", "myceli", "myceliu", "mycelium", "t", "to", "too", "tool", "toolt", "zür", "café", "lighthouse", "render", "x", "xyz"]) {
      filter.push(await page.evaluate((v) => new Promise<number>((resolve) => {
        const s = (window as any).__spike;
        const before = s.state();
        const el = document.getElementById("filter") as HTMLInputElement;
        const t0 = performance.now();
        el.value = v;
        el.dispatchEvent(new Event("input", { bubbles: true }));
        const tick = () => {
          if (s.state() !== before && s.idle()) requestAnimationFrame(() => resolve(performance.now() - t0));
          else setTimeout(tick, 0);
        };
        tick();
      }), q));
    }
    const pct = (a: number[], p: number) => [...a].sort((x, y) => x - y)[Math.floor(a.length * p)];
    const typingMs = typing.map((s: any) => s.afterPaint);
    const result = {
      browser: browserName, total, warmFirstPaintMs: paint.firstPaint, coldPageFirstPaintMs: paint.coldFcp, coldLaunchMs: Math.round(paint.coldLaunch),
      typing: { p50: pct(typingMs, 0.5), p95: pct(typingMs, 0.95), p99: pct(typingMs, 0.99), max: Math.max(...typingMs), n: typingMs.length },
      enter: { p50: pct(enter, 0.5), p95: pct(enter, 0.95), n: enter.length },
      filter: { p50: pct(filter, 0.5), p95: pct(filter, 0.95), n: filter.length },
    };
    console.log("METRICS " + JSON.stringify(result));
    await info.attach("metrics", { body: JSON.stringify(result), contentType: "application/json" });
  });

  test("scrolling far away and pressing a key returns to the cursor", async ({ page }) => {
    test.setTimeout(60_000);
    app = await start({ gen: 100_000, expanded: 10_000 });
    await open(page, app);
    const first = await page.evaluate(() => (window as any).__spike.state().cursor);
    await place(page, first, 0);
    await page.evaluate(() => { document.getElementById("scroller")!.scrollTop = 100_000; });
    await expect.poll(() => page.evaluate(() => (window as any).__spike.state().start)).toBeGreaterThan(1000);
    expect(await page.locator(`.row[data-id="${first}"]`).count()).toBe(0);
    await page.keyboard.press("Enter");
    await idle(page);
    await expect(page.locator(".row.focus")).toBeVisible();
    await expect(page.locator(".row.focus")).toBeInViewport();
  });
});

test.describe("AC7 theme", () => {
  test("a colors.toml change recolors without a reload", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n" });
    await open(page, app);
    await page.evaluate(() => { (window as any).__notReloaded = true; });
    await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(0, 0, 0)");
    writeFileSync(app.colors, 'mode = "light"\naccent = "#aa0000"\nbackground = "#ffffff"\nforeground = "#111111"\nmuted = "#777777"\n');
    await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(255, 255, 255)");
    expect(await page.evaluate(() => document.documentElement.style.getPropertyValue("--accent"))).toBe("#aa0000");
    expect(await page.evaluate(() => document.documentElement.style.getPropertyValue("color-scheme"))).toBe("light");
    expect(await page.evaluate(() => (window as any).__notReloaded)).toBe(true);
  });

  test("the page uses the system monospace font", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n" });
    await open(page, app);
    expect(await page.evaluate(() => getComputedStyle(document.body).fontFamily)).toBe("monospace");
  });
});

test.describe("AC8 security in a real browser", () => {
  test("the token leaves the address bar and the cookie is HttpOnly", async ({ page, context }) => {
    app = await start({ seed: "a||Alpha\n" });
    await open(page, app);
    expect(page.url()).not.toContain("token");
    expect(await page.evaluate(() => document.cookie)).toBe("");
    const c = (await context.cookies())[0];
    expect(c.httpOnly).toBe(true);
    expect(c.sameSite).toBe("Strict");
  });

  test("a page without the token is refused", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n" });
    const res = await page.goto(app.url.split("?")[0]);
    expect(res?.status()).toBe(403);
  });
});

test.describe("AC9 tabs", () => {
  test("a second tab is read-only, sees changes, and takes over when the first closes", async ({ page, context }) => {
    app = await start({ seed: "a||Alpha\n" });
    await open(page, app);
    await place(page, "a", 5);
    const second = await context.newPage();
    await second.goto(app.url);
    await second.waitForFunction(() => (window as any).__spike?.state());
    await expect(second.locator("#banner")).toHaveText(/Read-only/);
    await second.locator(".row .text").first().click();
    await second.keyboard.press("x");
    expect(await second.locator(".text[contenteditable]").count()).toBe(0);
    expect(await second.locator("#footer").innerText()).toMatch(/Read-only/);

    await page.keyboard.type("!");
    await idle(page);
    await expect.poll(() => second.evaluate(() => (window as any).__spike.outline())).toBe("Alpha!");

    await page.close();
    await expect(second.locator("#banner")).toBeHidden();
    await second.locator(".row .text").first().click();
    await idle(second);
    await second.keyboard.type("?");
    await expect.poll(() => second.evaluate(() => (window as any).__spike.state().rows[0].text)).toMatch(/\?/);
  });
});

test.describe("Report: $EDITOR and clipboard", () => {
  test("the editor round trip applies on exit as one undoable step", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n", editor: `printf 'Edited\n' > "$1"` });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("e");
    await expect.poll(() => outline(page)).toBe("Edited");
    await page.keyboard.press("Control+z");
    expect(await outline(page)).toBe("Alpha");
  });

  test("a failed editor applies nothing", async ({ page }) => {
    app = await start({ seed: "a||Alpha\n", editor: "exit 3" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("e");
    await expect.poll(() => page.locator("#footer").innerText()).toMatch(/editor failed/);
    expect(await outline(page)).toBe("Alpha");
  });

  test("Ctrl+C copies the subtree as Markdown after a server round trip", async ({ page, context, browserName }) => {
    test.skip(browserName !== "chromium", "clipboard read permission is Chromium only in Playwright");
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    app = await start({ seed: "a||A\n  k||Kid\nb||B\n" });
    await open(page, app);
    await place(page, "a", 0);
    await page.keyboard.press("Escape");
    await idle(page);
    await page.keyboard.press("Control+c");
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("- A\n  - Kid\n");
    expect(await cursorText(page)).toBe("A");
  });
});
