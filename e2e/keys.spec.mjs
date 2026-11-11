// Key dispatch from the keymap and the key probe (P0-19 AC4, AC5).
import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { idle, idOf, open, place, start, stop, ticketURL } from "./helpers.mjs";

let app;
test.afterEach(async () => {
  await stop(app);
  app = null;
});

/**
 * Dispatches a synthetic keydown to the focused bullet and reports whether
 * the page canceled it. Synthetic events have no browser default, so a
 * native key or a browser shortcut does nothing here.
 */
async function prevented(page, init) {
  await idle(page);
  return page.evaluate((init) => {
    const ev = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init });
    return !document.activeElement.dispatchEvent(ev);
  }, init);
}

const key = (k, mods = {}) => ({ key: k, code: /^[a-z]$/.test(k) ? `Key${k.toUpperCase()}` : "", ...mods });

test.describe("AC4 platform", () => {
  for (const platform of ["darwin", "linux"]) {
    test(`keymap platform ${platform}`, async ({ page }) => {
      app = await start({ seed: "- Alpha\n- Beta", platform });
      await open(page, app);
      await place(page, await idOf(page, "Alpha"), 2);
      const mac = platform === "darwin";
      const mod = mac ? { metaKey: true } : { ctrlKey: true };
      const other = mac ? { ctrlKey: true } : { metaKey: true };

      // Mod is Meta on macOS and Ctrl on Linux; the other modifier is unbound.
      const keymap = await page.evaluate(() => window.__hyphal.keymap());
      const keysOf = (id) => keymap.find((c) => c.id === id).keys.map((k) => k.key);
      expect(keysOf("undo")).toEqual([mac ? "meta+z" : "ctrl+z"]);
      expect(keymap.flatMap((c) => c.keys).filter((k) => k.key.includes("mod"))).toEqual([]);
      expect(await prevented(page, key("y", mod))).toBe(true); // redo
      expect(await prevented(page, key("y", other))).toBe(false);
      expect(await prevented(page, key("ArrowUp", { ...mod, shiftKey: true }))).toBe(true); // move up

      // Native keys stay with the browser: word moves, select all, extending
      // the selection, copy and paste.
      const word = mac ? { altKey: true } : { ctrlKey: true };
      expect(keysOf("move_word")).toEqual(mac ? ["alt+arrowleft", "alt+arrowright"] : ["ctrl+arrowleft", "ctrl+arrowright"]);
      expect(await prevented(page, key("ArrowRight", word))).toBe(false);
      expect(await prevented(page, key("a", mod))).toBe(false);
      expect(await prevented(page, key("c", mod))).toBe(false);
      expect(await prevented(page, key("v", mod))).toBe(false);
      expect(await prevented(page, key("ArrowLeft", { shiftKey: true }))).toBe(false);
      if (mac) {
        // macOS keeps Ctrl+A and Ctrl+E (D-043).
        expect(await prevented(page, key("a", { ctrlKey: true }))).toBe(false);
        expect(await prevented(page, key("e", { ctrlKey: true }))).toBe(false);
      }
      // Unbound keys and plain typing are never canceled.
      expect(await prevented(page, key("q"))).toBe(false);
      expect(await prevented(page, key("b", { altKey: true }))).toBe(false);
    });
  }
});

test.describe("AC5 key probe", () => {
  test("keyprobe records", async ({ page }) => {
    app = await start({ seed: "- Alpha" });
    await page.goto(await ticketURL(app));
    await page.goto(`${app.origin}/keyprobe`);
    await expect(page.locator(".bindings tbody tr").first()).toBeVisible();
    await page.locator(".pad").click();
    const mod = await page.evaluate(() => (navigator.platform.startsWith("Mac") ? "Meta" : "Control"));
    await page.keyboard.press(`${mod}+KeyK`); // jump palette, bound
    await page.keyboard.press("F1"); // help, bound
    await page.keyboard.press("Shift+Tab"); // outdent, bound in text and block
    await page.keyboard.press("F7"); // unbound
    await expect(page.locator('tr[data-key="f1"]').first()).toHaveAttribute("data-reached", "yes");

    const [download] = await Promise.all([page.waitForEvent("download"), page.locator(".download").click()]);
    const result = JSON.parse(readFileSync(await download.path(), "utf8"));
    expect(result.probe).toBe("keyprobe");
    expect(result.version).toBe(1);
    expect(typeof result.userAgent).toBe("string");
    const reached = (k) => result.bindings.filter((b) => b.key === k).map((b) => b.reached);
    const modKey = result.platform === "darwin" ? "meta+k" : "ctrl+k";
    expect(reached(modKey)).toEqual([true]);
    expect(reached("f1")).toEqual([true]);
    expect(reached("shift+tab")).toEqual([true, true]);
    expect(reached("escape").every((r) => r === false)).toBe(true);
    expect(result.reserved.map((r) => r.key)).toContain(result.platform === "darwin" ? "meta+w" : "ctrl+w");
    expect(result.other).toEqual(["f7"]);
    // Every binding the server sent is in the result.
    const sent = await page.evaluate(() => fetch("/keyprobe.json").then((r) => r.json()));
    expect(result.bindings.length).toBe(sent.keymap.flatMap((c) => c.keys).length);
  });
});
