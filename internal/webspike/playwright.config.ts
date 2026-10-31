import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  globalSetup: "./e2e/setup.ts",
  timeout: 30_000,
  fullyParallel: true,
  retries: 0,
  reporter: [["list"], ["json", { outputFile: "results/results.json" }]],
  use: { viewport: { width: 1000, height: 700 } },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1000, height: 700 } } },
    { name: "firefox", use: { ...devices["Desktop Firefox"], viewport: { width: 1000, height: 700 } } },
    { name: "webkit", use: { ...devices["Desktop Safari"], viewport: { width: 1000, height: 700 } } },
  ],
});
