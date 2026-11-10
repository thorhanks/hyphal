import { defineConfig, devices } from "@playwright/test";

const viewport = { width: 1000, height: 700 };

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.mjs",
  globalSetup: "./global-setup.mjs",
  timeout: 30_000,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["line"]] : [["list"]],
  use: { viewport },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport } },
    { name: "firefox", use: { ...devices["Desktop Firefox"], viewport } },
    { name: "webkit", use: { ...devices["Desktop Safari"], viewport } },
  ],
});
