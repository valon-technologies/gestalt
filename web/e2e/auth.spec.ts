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
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await page.route("**/api/v1/auth/logout", (route) => {
      route.fulfill({ json: { status: "ok" } });
    });

    await page.goto("/");
    await page.getByRole("button", { name: /Logout/i }).click();
    await expect(page).toHaveURL(/\/login/);
    const email = await page.evaluate(() => localStorage.getItem("user_email"));
    expect(email).toBeNull();
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

  test("auth callback hands off CLI state without navigating to localhost", async ({
    page,
  }) => {
    const code = "test-code";
    const state = "original-state";
    const port = 12345;
    let callbackRequestUrl: string | null = null;

    await page.route(`**://127.0.0.1:${port}/**`, async (route, request) => {
      callbackRequestUrl = request.url();
      await route.fulfill({
        status: 200,
        contentType: "text/html",
        body: "<!doctype html><html><body>ok</body></html>",
      });
    });

    await page.goto(`/auth/callback?code=${code}&state=cli:${port}:${state}`);

    await expect.poll(() => callbackRequestUrl).toBeTruthy();
    expect(callbackRequestUrl).toContain(`http://127.0.0.1:${port}/`);

    const callbackUrl = new URL(callbackRequestUrl as string);
    expect(callbackUrl.searchParams.get("code")).toBe(code);
    expect(callbackUrl.searchParams.get("state")).toBe(state);

    await expect(page).not.toHaveURL(new RegExp(`127\\.0\\.0\\.1:${port}`));
    await expect(
      page.getByText("Login successful! You can close this tab."),
    ).toBeVisible();
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
