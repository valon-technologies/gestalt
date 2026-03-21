import { test, expect, mockIntegrations, mockTokens } from "./fixtures";

test.describe("Navigation", () => {
  test("nav links are visible when authenticated", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { name: "slack", display_name: "Slack", description: "Team messaging" },
    ]);
    await mockTokens(page, []);

    await page.goto("/");

    const nav = page.locator("nav");
    await expect(nav.getByRole("link", { name: "Dashboard" })).toBeVisible();
    await expect(
      nav.getByRole("link", { name: "Integrations" }),
    ).toBeVisible();
    await expect(
      nav.getByRole("link", { name: "API Tokens" }),
    ).toBeVisible();
    await expect(nav.getByRole("link", { name: "Docs" })).toBeVisible();
  });

  test("user email is displayed in nav", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/");
    await expect(page.getByText("test@gestalt.dev")).toBeVisible();
  });

  test("navigating to /integrations works", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { name: "slack", display_name: "Slack", description: "Team messaging" },
    ]);
    await mockTokens(page, []);

    await page.goto("/");
    const nav = page.locator("nav");
    await nav.getByRole("link", { name: "Integrations" }).click();
    await expect(page).toHaveURL("/integrations");
    await expect(
      page.getByRole("heading", { name: "Integrations" }),
    ).toBeVisible();
  });

  test("navigating to /tokens works", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/");
    const nav = page.locator("nav");
    await nav.getByRole("link", { name: "API Tokens" }).click();
    await expect(page).toHaveURL("/tokens");
    await expect(
      page.getByRole("heading", { name: "API Tokens" }),
    ).toBeVisible();
  });

});
