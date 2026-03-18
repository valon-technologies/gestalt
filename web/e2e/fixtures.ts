import { test as base, expect, type Page, type Route } from "@playwright/test";
import type { Integration, APIToken } from "../src/lib/api";

export async function mockIntegrations(page: Page, integrations: Integration[]) {
  await page.route("**/api/v1/integrations", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: integrations });
    } else {
      route.fallback();
    }
  });
}

export async function mockTokens(page: Page, tokens: APIToken[]) {
  await page.route("**/api/v1/tokens", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: tokens });
    } else {
      route.fallback();
    }
  });
}

type CustomFixtures = {
  authenticatedPage: Page;
};

export const test = base.extend<CustomFixtures>({
  authenticatedPage: async ({ page }, use) => {
    await page.addInitScript(() => {
      localStorage.setItem("session_token", "test-session-token");
      localStorage.setItem("user_email", "test@toolshed.dev");
    });
    await use(page);
  },
});

export { expect };
