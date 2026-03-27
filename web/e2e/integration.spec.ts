import { test, expect } from "@playwright/test";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Integration: Go server contract", () => {
  test.skip(!hasBackend, "Requires PLAYWRIGHT_BASE_URL (real Go server)");

  test.describe.configure({ mode: "serial" });

  async function injectAuth(page: import("@playwright/test").Page) {
    await page.addInitScript(() => {
      localStorage.setItem("user_email", "e2e@gestalt.dev");
    });
  }

  test("integrations page loads from Go server", async ({ page }) => {
    await injectAuth(page);
    await page.goto("/integrations");
    await expect(
      page.getByRole("heading", { name: "Integrations" }),
    ).toBeVisible();
  });

  test("tokens CRUD works against Go server", async ({ page }) => {
    await injectAuth(page);
    await page.goto("/tokens");
    await expect(
      page.getByRole("heading", { name: "API Tokens" }),
    ).toBeVisible();

    const tokenName = `e2e-test-${Date.now()}`;
    await page.getByLabel("Token name").fill(tokenName);
    await page.getByRole("button", { name: "Create Token" }).click();
    await expect(page.getByText("Copy this token now")).toBeVisible();
    await expect(page.getByText(tokenName)).toBeVisible();

    const row = page.locator("tr", { hasText: tokenName });
    await row.getByRole("button", { name: "Revoke" }).click();
    await expect(page.getByText(tokenName)).toBeHidden();
  });
});
