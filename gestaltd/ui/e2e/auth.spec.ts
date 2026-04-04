import {
  test,
  expect,
  mockAuthInfo,
  mockIntegrations,
  mockMetricsOverview,
  mockTokens,
  sampleMetricsOverview,
} from "./fixtures";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Authentication", () => {
  test.skip(
    hasBackend,
    "Auth flow tests use mocked routes and do not apply when running against a real server",
  );
  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("login page renders with provider button", async ({ page }) => {
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await page.goto("/login");
    await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Sign in with Test SSO/i }),
    ).toBeVisible();
  });

  test("authenticated user sees dashboard", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { name: "test-svc", display_name: "Test Service" },
    ]);
    await mockTokens(page, []);
    await mockMetricsOverview(page, sampleMetricsOverview);

    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Dashboard" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Operation activity" }),
    ).toBeVisible();
    await expect(page.getByText("Requests", { exact: true })).toBeVisible();
    await expect(page.getByText("1,240")).toBeVisible();
  });

  test("dashboard metrics bars scale from visible request volume only", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await mockMetricsOverview(page, {
      ...sampleMetricsOverview,
      series: Array.from({ length: 13 }, (_, index) => ({
        start: `2026-04-03T15:${String(index).padStart(2, "0")}:00Z`,
        requests: index === 0 ? 1000 : index === 11 ? 10 : index === 12 ? 20 : 5,
        errors: index === 11 ? 10 : 0,
        error_rate: index === 11 ? 1 : 0,
        p95_latency_ms: 100,
        throughput_rps: 1,
      })),
    });

    await page.goto("/");

    const bars = page.getByTestId("metrics-bar");
    await expect(bars).toHaveCount(12);
    await expect(bars.nth(10)).toHaveAttribute("style", /height: 50%/);
    await expect(bars.nth(11)).toHaveAttribute("style", /height: 100%/);
  });

  test("authenticated user on /login is redirected to dashboard", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await mockMetricsOverview(page, sampleMetricsOverview);

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
    await mockMetricsOverview(page, sampleMetricsOverview);
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
    await page.route("**/api/v1/metrics/overview**", (route) => {
      route.fulfill({ status: 401, json: { error: "invalid token" } });
    });

    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });
});
