import { test as base, expect, type Page, type Route } from "@playwright/test";
import type { Integration, APIToken } from "../src/lib/api";

export async function mockIntegrations(
  page: Page,
  integrations: Integration[],
  opts?: { onDisconnect?: (name: string) => void },
) {
  await page.route("**/api/v1/integrations", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: integrations });
    } else {
      route.fallback();
    }
  });

  await page.route("**/api/v1/integrations/*", (route: Route, request) => {
    if (request.method() === "DELETE") {
      const url = new URL(request.url());
      const name = url.pathname.split("/").pop() || "";
      opts?.onDisconnect?.(name);
      route.fulfill({ json: { status: "disconnected" } });
    } else {
      route.fallback();
    }
  });
}

export async function mockAuthInfo(
  page: Page,
  info: { provider: string; display_name: string },
) {
  await page.route("**/api/v1/auth/info", (route: Route) => {
    route.fulfill({ json: info });
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
      sessionStorage.setItem("session_token", "test-session-token");
      sessionStorage.setItem("user_email", "test@gestalt.dev");
    });
    await use(page);
  },
});

export { expect };
