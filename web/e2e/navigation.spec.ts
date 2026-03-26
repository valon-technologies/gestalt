import { test, expect, hasLiveBackend } from "./fixtures";

test.describe("Navigation", () => {
  test.skip(!hasLiveBackend, "Requires PLAYWRIGHT_BASE_URL");

  test("authenticated nav links are visible", async ({
    authenticatedPage,
    authenticatedEmail,
  }) => {
    const page = authenticatedPage;

    await page.goto("/");

    const nav = page.locator("nav");
    await expect(nav.getByRole("link", { name: "Dashboard" })).toBeVisible();
    await expect(nav.getByRole("link", { name: "Integrations" })).toBeVisible();
    await expect(nav.getByRole("link", { name: "API Tokens" })).toBeVisible();
    await expect(nav.getByRole("link", { name: "Docs" })).toBeVisible();
    await expect(nav.getByText(authenticatedEmail)).toBeVisible();
    await expect(nav.getByRole("button", { name: "Logout" })).toBeVisible();
  });

  test("dashboard links navigate to integrations and tokens", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;

    await page.goto("/");
    const nav = page.locator("nav");
    await nav.getByRole("link", { name: "Integrations" }).click();
    await expect(page).toHaveURL(/\/integrations$/);
    await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();

    await nav.getByRole("link", { name: "API Tokens" }).click();
    await expect(page).toHaveURL(/\/tokens$/);
    await expect(page.getByRole("heading", { name: "API Tokens" })).toBeVisible();
  });
});
