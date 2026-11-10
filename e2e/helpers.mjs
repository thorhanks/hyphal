// Shared helpers for the browser tests (plan/TESTING.md §5). Every server runs
// with all of its paths in a fresh temp directory, never the real data or theme.

import { spawn } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect } from "@playwright/test";

export const repo = join(dirname(fileURLToPath(import.meta.url)), "..");
export const binary = join(repo, "e2e", ".bin", "hyphal");

/**
 * Starts `hyphal serve --dev --seed` on a fixture (CONTRACTS §11).
 * @param {{seed: string}} o
 */
export async function start({ seed }) {
  const dir = mkdtempSync(join(tmpdir(), "hyphal-e2e-"));
  writeFileSync(join(dir, "seed.txt"), seed);
  const env = {
    ...process.env,
    HOME: dir,
    XDG_DATA_HOME: join(dir, "data"),
    XDG_CONFIG_HOME: join(dir, "config"),
    XDG_RUNTIME_DIR: join(dir, "run"),
    HYPHAL_DB: join(dir, "data", "outline.db"),
  };
  const proc = spawn(binary, ["serve", "--dev", "--seed", join(dir, "seed.txt")], { env, stdio: ["ignore", "pipe", "pipe"] });
  let stderr = "";
  proc.stderr.on("data", (d) => (stderr += d));
  const exited = new Promise((resolve) => proc.on("exit", resolve));
  const url = await new Promise((resolve, reject) => {
    let out = "";
    proc.stdout.on("data", (d) => {
      out += d;
      const m = /URL (\S+)/.exec(out);
      if (m) resolve(m[1]);
    });
    exited.then((code) => reject(new Error(`hyphal serve exited ${code}: ${stderr}`)));
  });
  return { url, dir, proc, exited, origin: new URL(url).origin };
}

export async function stop(app) {
  if (!app) return;
  app.proc.kill("SIGTERM");
  await app.exited;
  rmSync(app.dir, { recursive: true, force: true });
}

/** A new launch URL: the state file's secret buys a single-use ticket (D-041). */
export async function ticketURL(app) {
  const run = join(app.dir, "run", "hyphal");
  const name = readdirSync(run).find((n) => n.startsWith("server-"));
  const { secret } = JSON.parse(readFileSync(join(run, name), "utf8"));
  const res = await fetch(`${app.origin}/internal/ticket`, { method: "POST", body: secret });
  expect(res.status).toBe(200);
  return `${app.origin}/?ticket=${await res.text()}`;
}

export async function open(page, app) {
  await page.goto(await ticketURL(app));
  await page.waitForFunction(() => window.__hyphal?.state());
}

export async function idle(page) {
  await page.waitForFunction(() => window.__hyphal.idle());
}

/** The visible rows as indented text. */
export async function outline(page) {
  await idle(page);
  return page.evaluate(() => window.__hyphal.outline());
}

export async function idOf(page, text) {
  const id = await page.evaluate((t) => window.__hyphal.idOf(t), text);
  expect(id, `a row with text ${JSON.stringify(text)}`).toBeTruthy();
  return id;
}

export function host(page, id) {
  return page.locator(`.row[data-id="${id}"] > .text`);
}

/** Puts the cursor in a bullet at a UTF-16 offset of its source text. */
export async function place(page, id, off) {
  await page.evaluate(([i, o]) => window.__hyphal.setCursor(i, o), [id, off]);
  await idle(page);
  await expect(host(page, id)).toBeFocused();
}

export async function cursorIs(page, id, off) {
  await idle(page);
  await expect.poll(() => page.evaluate(() => window.__hyphal.state().cursor)).toBe(id);
  await expect.poll(() => page.evaluate(() => window.__hyphal.caret()?.off)).toBe(off);
}

/** The server's text for a node, from the latest state. */
export async function serverText(page, id) {
  await idle(page);
  return page.evaluate((i) => window.__hyphal.state().rows.find((r) => r.id === i)?.text, id);
}
