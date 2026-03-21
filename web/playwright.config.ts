import { defineConfig, devices } from "@playwright/test";

const webPort = Number(process.env.WEB_PORT) || 3000;
const apiUrl = process.env.TOOLSHED_API_URL; // set when running against real Go server

export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 2,
  timeout: 60000,

  reporter: [
    [process.env.CI ? "dot" : "list"],
    ["html", { open: process.env.CI ? "never" : "on-failure" }],
  ],

  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL || `http://localhost:${webPort}`,
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
    actionTimeout: 15000,
    navigationTimeout: 20000,
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],

  expect: {
    timeout: 10000,
  },

  webServer: {
    command: apiUrl
      ? `TOOLSHED_API_URL=${apiUrl} npm run dev -- --port ${webPort}`
      : `npm run dev -- --port ${webPort}`,
    url: `http://localhost:${webPort}`,
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
    stdout: "ignore",
    stderr: "pipe",
  },
});
