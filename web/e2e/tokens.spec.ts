import { test, expect, hasLiveBackend } from "./fixtures";

test.describe("Token Management", () => {
  test.skip(!hasLiveBackend, "Requires PLAYWRIGHT_BASE_URL");

  test("token list loads against the live backend", async ({ authenticatedPage }) => {
    const page = authenticatedPage;

    await page.goto("/tokens");

    await expect(page.getByRole("heading", { name: "API Tokens" })).toBeVisible();
    await expect(page.getByText("Manage tokens for programmatic access to the Gestalt API.")).toBeVisible();

    const emptyState = page.getByText("No API tokens yet.");
    if (await emptyState.count()) {
      await expect(emptyState).toBeVisible();
    } else {
      await expect(page.locator("table")).toBeVisible();
    }
  });

  test("creates and revokes a token against the live backend", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    const tokenName = `e2e-token-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

    await page.goto("/tokens");
    await page.getByLabel("Token name").fill(tokenName);
    await page.getByRole("button", { name: "Create Token" }).click();

    await expect(page.getByText("Copy this token now. It will not be shown again.")).toBeVisible();
    await expect(page.getByText(tokenName)).toBeVisible();

    const row = page.locator("tr", { hasText: tokenName });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Revoke" }).click();
    await expect(page.getByText(tokenName)).toHaveCount(0);
  });
});
