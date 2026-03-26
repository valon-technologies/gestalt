import { test, expect, hasLiveBackend, loginAsDevUser } from "./fixtures";

test.describe("Authentication", () => {
  test.skip(!hasLiveBackend, "Requires PLAYWRIGHT_BASE_URL");

  test("login page shows the live auth provider and dev login form", async ({ page }) => {
    await page.goto("/login");

    await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
    await expect(page.getByRole("button", { name: /Sign in with / })).toBeVisible();
    await expect(page.getByRole("button", { name: "Dev Login" })).toBeVisible();
  });

  test("dev login lands on the dashboard", async ({ page }) => {
    const email = await loginAsDevUser(page, "auth");

    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByText(email)).toBeVisible();
    await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
  });

  test("logout clears the session and returns to login", async ({ authenticatedPage, authenticatedEmail }) => {
    const page = authenticatedPage;

    await page.getByRole("button", { name: "Logout" }).click();

    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
    await expect(page.getByText(authenticatedEmail)).toHaveCount(0);
    expect(await page.evaluate(() => localStorage.getItem("user_email"))).toBeNull();
  });

  test("expired session redirects to login on the next API request", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    const hostname = new URL(page.url()).hostname;

    await page.context().clearCookies();
    await page.context().addCookies([
      {
        name: "session_token",
        value: "invalid-session-token",
        domain: hostname,
        path: "/",
        httpOnly: true,
        secure: false,
      },
    ]);

    await page.goto("/tokens");

    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
  });
});
