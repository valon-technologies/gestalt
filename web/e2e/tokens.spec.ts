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
    created_at: "2026-01-15T10:00:00Z",
  },
  {
    id: "tok-2",
    name: "deploy-key",
    scopes: "",
    created_at: "2026-02-20T14:30:00Z",
    expires_at: "2027-02-20T14:30:00Z",
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
            created_at: new Date().toISOString(),
          },
        ];
        route.fulfill({
          status: 201,
          json: { id: "tok-new", name: "my-new-token", token: "tshed_abc123secret" },
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
    await expect(page.getByText("tshed_abc123secret")).toBeVisible();
    await expect(page.getByText("my-new-token")).toBeVisible();
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
