import { test as base, expect, type Page, type Route } from "@playwright/test";
import type { APIToken, Integration } from "../src/lib/api";

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

export async function mockManualConnect(
  page: Page,
  opts?: { onConnect?: (integration: string, credential: string) => void },
) {
  await page.route(
    "**/api/v1/auth/connect-manual",
    async (route: Route, request) => {
      if (request.method() === "POST") {
        const body = request.postDataJSON() as {
          integration: string;
          credential: string;
        };
        opts?.onConnect?.(body.integration, body.credential);
        await route.fulfill({ json: { status: "connected" } });
      } else {
        await route.fallback();
      }
    },
  );
}

export async function mockAuthInfo(
  page: Page,
  info: { provider: string; displayName: string; loginSupported?: boolean },
) {
  await page.route("**/api/v1/auth/info", (route: Route) => {
    route.fulfill({ json: { loginSupported: true, ...info } });
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
  authenticatedPage: async ({ page }, runAuthenticatedPage) => {
    await page.addInitScript(() => {
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await runAuthenticatedPage(page);
  },
});

export { expect };
