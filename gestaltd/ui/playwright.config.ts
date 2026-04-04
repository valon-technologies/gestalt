import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.PLAYWRIGHT_BASE_URL;
const apiPort = Number(process.env.API_PORT) || 8080;

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
    baseURL: baseURL || `http://localhost:${apiPort}`,
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

  ...(baseURL
    ? {}
    : {
        webServer: {
          command: "./dev.sh",
          url: `http://localhost:${apiPort}`,
          reuseExistingServer: !process.env.CI,
          timeout: 120000,
          stdout: "ignore",
          stderr: "pipe",
        },
      }),
});
