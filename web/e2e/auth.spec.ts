import { test, expect, mockAuthInfo, mockIntegrations, mockTokens } from "./fixtures";

test.describe("Authentication", () => {
  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("login page renders with provider button", async ({ page }) => {
    await mockAuthInfo(page, { provider: "test-sso", display_name: "Test SSO" });
    await page.goto("/login");
    await expect(
      page.getByRole("heading", { name: "Gestalt" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Sign in with Test SSO/i }),
    ).toBeVisible();
  });

  test("authenticated user sees dashboard", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [{ name: "test-svc", display_name: "Test Service" }]);
    await mockTokens(page, []);

    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Dashboard" }),
    ).toBeVisible();
  });

  test("authenticated user on /login is redirected to dashboard", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/login");
    await expect(page).toHaveURL("/");
  });

  test("logout clears session and redirects to login", async ({ page }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      sessionStorage.setItem("session_token", "test-session-token");
      sessionStorage.setItem("user_email", "test@gestalt.dev");
    });
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/");
    await page.getByRole("button", { name: /Logout/i }).click();
    await expect(page).toHaveURL(/\/login/);
    const token = await page.evaluate(() => sessionStorage.getItem("session_token"));
    expect(token).toBeNull();
  });

  test("auth callback rejects mismatched OAuth state (CSRF protection)", async ({
    page,
  }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      sessionStorage.setItem("oauth_state", "correct-state");
    });

    await page.goto("/auth/callback?code=test-code&state=wrong-state");
    await expect(page.getByText(/Invalid OAuth state/)).toBeVisible();
    await expect(page.getByText("Back to login")).toBeVisible();
  });

  test("auth callback rejects when no OAuth state was saved (CSRF protection)", async ({
    page,
  }) => {
    await page.goto("/login");
    await page.goto("/auth/callback?code=attacker-code&state=attacker-state");
    await expect(page.getByText(/Invalid OAuth state/)).toBeVisible();
  });

  test("401 response clears session and redirects to login", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await page.route("**/api/v1/integrations", (route) => {
      route.fulfill({ status: 401, json: { error: "invalid token" } });
    });
    await page.route("**/api/v1/tokens", (route) => {
      route.fulfill({ status: 401, json: { error: "invalid token" } });
    });

    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });
});
