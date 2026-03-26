import { test, expect, hasLiveBackend, loginAsDevUser } from "./fixtures";

test.describe("Integration smoke", () => {
  test.skip(!hasLiveBackend, "Requires PLAYWRIGHT_BASE_URL");

  test("login, browse integrations, and create/revoke a token", async ({ page }) => {
    const email = await loginAsDevUser(page, "smoke");

    await page.goto("/integrations");
    await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();
    await expect(page.getByText(email)).toBeVisible();

    await page.goto("/tokens");
    const tokenName = `e2e-smoke-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    await page.getByLabel("Token name").fill(tokenName);
    await page.getByRole("button", { name: "Create Token" }).click();
    await expect(page.getByText(tokenName)).toBeVisible();

    const row = page.locator("tr", { hasText: tokenName });
    await row.getByRole("button", { name: "Revoke" }).click();
    await expect(page.getByText(tokenName)).toHaveCount(0);
  });
});
