import { defineConfig, devices } from "@playwright/test";

const viewport = { width: 1000, height: 700 };
export default defineConfig({
  testDir: "e2e",
  timeout: 30_000,
  fullyParallel: true,
  retries: 0,
  reporter: [["list"]],
  use: { baseURL: "http://127.0.0.1:47900", viewport },
  webServer: { command: "node server.mjs", url: "http://127.0.0.1:47900/", reuseExistingServer: true },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport } },
    { name: "firefox", use: { ...devices["Desktop Firefox"], viewport } },
    { name: "webkit", use: { ...devices["Desktop Safari"], viewport } },
  ],
});
