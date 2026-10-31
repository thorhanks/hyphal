import { ChildProcess, spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, Page } from "@playwright/test";

export interface App { url: string; dir: string; proc: ChildProcess; colors: string; started: number; }

// start runs a private server with temp paths only, never the real theme directory.
export async function start(args: { seed?: string; gen?: number; expanded?: number; editor?: string; colors?: string } = {}): Promise<App> {
  const dir = mkdtempSync(join(tmpdir(), "webspike-"));
  const colors = join(dir, "colors.toml");
  writeFileSync(colors, args.colors ?? 'mode = "dark"\naccent = "#112233"\nbackground = "#000000"\nforeground = "#eeeeee"\n');
  const a = ["--colors", colors, "--editor-cmd", args.editor ?? "true"];
  if (args.seed !== undefined) {
    writeFileSync(join(dir, "seed.txt"), args.seed);
    a.push("--seed", join(dir, "seed.txt"));
  }
  if (args.gen) a.push("--gen", String(args.gen), "--expanded", String(args.expanded ?? 10000));
  const started = performance.now();
  const proc = spawn("./webspike", a, { stdio: ["ignore", "pipe", "inherit"] });
  const url = await new Promise<string>((resolve, reject) => {
    proc.stdout!.on("data", (d: Buffer) => {
      const m = /URL (\S+)/.exec(d.toString());
      if (m) resolve(m[1]);
    });
    proc.on("exit", (c) => reject(new Error(`server exited ${c}`)));
  });
  return { url, dir, proc, colors, started };
}

export async function open(page: Page, app: App) {
  await page.goto(app.url);
  await page.waitForFunction(() => (window as any).__spike?.state());
}

export async function idle(page: Page) {
  await page.waitForFunction(() => (window as any).__spike.idle());
}

export async function outline(page: Page): Promise<string> {
  await idle(page);
  return page.evaluate(() => (window as any).__spike.outline());
}

export async function place(page: Page, id: string, off: number) {
  await page.evaluate(([i, o]) => (window as any).__spike.setCursor(i, o), [id, off] as const);
  await idle(page);
  await expect(page.locator(`.row[data-id="${id}"] > .text`)).toBeFocused();
}

export async function cursorIs(page: Page, id: string, off: number) {
  await idle(page);
  await expect.poll(() => page.evaluate(() => (window as any).__spike.state().cursor)).toBe(id);
  await expect.poll(() => page.evaluate(() => (window as any).__spike.caret()?.off)).toBe(off);
}

export async function cursorText(page: Page): Promise<string> {
  await idle(page);
  return page.evaluate(() => {
    const s = (window as any).__spike.state();
    return s.rows.find((r: any) => r.id === s.cursor).text;
  });
}

export async function footer(page: Page) {
  await idle(page);
  return page.locator("#footer").innerText();
}
