import {
  test,
  expect,
  mockTokens,
  mockIntegrations,
} from "./fixtures";
import type { APIToken } from "../src/lib/api";

const sampleTokens: APIToken[] = [
  {
    id: "tok-1",
    name: "ci-pipeline",
    scopes: "read",
    createdAt: "2026-01-15T10:00:00Z",
  },
  {
    id: "tok-2",
    name: "deploy-key",
    scopes: "",
    createdAt: "2026-02-20T14:30:00Z",
    expiresAt: "2027-02-20T14:30:00Z",
  },
];

test.describe("Token Management", () => {
  test("displays token list", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockTokens(page, sampleTokens);
    await mockIntegrations(page, []);

    await page.goto("/tokens");
    await expect(
      page.getByRole("heading", { name: "API Tokens" }),
    ).toBeVisible();
    await expect(page.getByText("ci-pipeline")).toBeVisible();
    await expect(page.getByText("deploy-key")).toBeVisible();
  });

  test("shows empty state when no tokens", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockTokens(page, []);
    await mockIntegrations(page, []);

    await page.goto("/tokens");
    await expect(page.getByText("No API tokens yet.")).toBeVisible();
  });

  test("creates a token and shows plaintext", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    // Start with empty tokens, then after creation return the new one.
    let tokens: APIToken[] = [];
    await page.route("**/api/v1/tokens", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: tokens });
      } else if (request.method() === "POST") {
        tokens = [
          {
            id: "tok-new",
            name: "my-new-token",
            scopes: "",
            createdAt: new Date().toISOString(),
          },
        ];
        route.fulfill({
          status: 201,
          json: { id: "tok-new", name: "my-new-token", token: "gestalt_abc123secret" },
        });
      } else {
        route.continue();
      }
    });
    await mockIntegrations(page, []);

    await page.goto("/tokens");
    await page.getByLabel("Token name").fill("my-new-token");
    await page.getByRole("button", { name: "Create Token" }).click();

    await expect(
      page.getByText("Copy this token now"),
    ).toBeVisible();
    await expect(page.getByText("gestalt_abc123secret")).toBeVisible();
    await expect(
      page.locator("tr", { hasText: "my-new-token" }),
    ).toBeVisible();
  });

  test("keeps the created token visible when stale list requests finish later", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let tokens: APIToken[] = [];
    let getCount = 0;

    await page.route("**/api/v1/tokens", async (route, request) => {
      if (request.method() === "GET") {
        getCount += 1;
        if (getCount === 1) {
          await new Promise((resolve) => setTimeout(resolve, 250));
          await route.fulfill({ json: [] });
          return;
        }
        await route.fulfill({ json: tokens });
        return;
      }

      if (request.method() === "POST") {
        tokens = [
          {
            id: "tok-race",
            name: "race-token",
            scopes: "",
            createdAt: new Date().toISOString(),
          },
        ];
        await route.fulfill({
          status: 201,
          json: { id: "tok-race", name: "race-token", token: "gestalt_race_secret" },
        });
        return;
      }

      await route.continue();
    });
    await mockIntegrations(page, []);

    await page.goto("/tokens");
    await page.getByLabel("Token name").fill("race-token");
    await page.getByRole("button", { name: "Create Token" }).click();

    await expect(page.getByText("Copy this token now")).toBeVisible();
    await expect(page.locator("tr", { hasText: "race-token" })).toBeVisible();
    await expect(page.getByText("No API tokens yet.")).toBeHidden();
  });

  test("revokes a token", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    let tokens = [...sampleTokens];
    await page.route("**/api/v1/tokens", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: tokens });
      } else {
        route.continue();
      }
    });
    await page.route("**/api/v1/tokens/*", (route, request) => {
      if (request.method() === "DELETE") {
        tokens = tokens.filter((t) => !request.url().includes(t.id));
        route.fulfill({ json: { status: "revoked" } });
      } else {
        route.continue();
      }
    });
    await mockIntegrations(page, []);

    await page.goto("/tokens");
    await expect(page.getByText("ci-pipeline")).toBeVisible();

    // Click the first Revoke button.
    await page.getByRole("button", { name: "Revoke" }).first().click();
    await expect(page.getByText("ci-pipeline")).toBeHidden();
  });
});
