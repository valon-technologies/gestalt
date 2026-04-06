import {
  test,
  expect,
  mockAuthInfo,
  mockIntegrations,
  mockTokens,
} from "./fixtures";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Authentication", () => {
  test.skip(
    hasBackend,
    "Auth flow tests use mocked routes and do not apply when running against a real server",
  );
  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("login page renders with provider button", async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
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

    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Dashboard" }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Integrations", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "API Tokens", exact: true }),
    ).toBeVisible();
  });

  test("authenticated user can open the embedded admin UI directly", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await page.route("**/metrics", (route) => {
      route.fulfill({
        status: 200,
        contentType: "text/plain",
        body: `
# TYPE target_info gauge
target_info{service_name="gestaltd"} 1
# TYPE gestaltd_operation_count_total counter
gestaltd_operation_count_total{gestalt_provider="slash\\\\nname",gestalt_operation="escaped"} 25
gestaltd_operation_count_total{gestalt_provider="example",gestalt_operation="echo"} 12
gestaltd_operation_count_total{gestalt_provider="slack",gestalt_operation="messages.list"} 8
# TYPE gestaltd_operation_error_count_total counter
gestaltd_operation_error_count_total{gestalt_provider="slash\\\\nname",gestalt_operation="escaped"} 0
gestaltd_operation_error_count_total{gestalt_provider="example",gestalt_operation="echo"} 2
gestaltd_operation_error_count_total{gestalt_provider="slack",gestalt_operation="messages.list"} 1
# TYPE gestaltd_operation_duration_seconds histogram
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slash\\\\nname",gestalt_operation="escaped",le="0.1"} 4
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slash\\\\nname",gestalt_operation="escaped",le="0.5"} 18
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slash\\\\nname",gestalt_operation="escaped",le="1"} 25
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slash\\\\nname",gestalt_operation="escaped",le="+Inf"} 25
gestaltd_operation_duration_seconds_sum{gestalt_provider="slash\\\\nname",gestalt_operation="escaped"} 6.25
gestaltd_operation_duration_seconds_count{gestalt_provider="slash\\\\nname",gestalt_operation="escaped"} 25
gestaltd_operation_duration_seconds_bucket{gestalt_provider="example",gestalt_operation="echo",le="0.1"} 3
gestaltd_operation_duration_seconds_bucket{gestalt_provider="example",gestalt_operation="echo",le="0.5"} 10
gestaltd_operation_duration_seconds_bucket{gestalt_provider="example",gestalt_operation="echo",le="1"} 12
gestaltd_operation_duration_seconds_bucket{gestalt_provider="example",gestalt_operation="echo",le="+Inf"} 12
gestaltd_operation_duration_seconds_sum{gestalt_provider="example",gestalt_operation="echo"} 3.6
gestaltd_operation_duration_seconds_count{gestalt_provider="example",gestalt_operation="echo"} 12
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="messages.list",le="0.1"} 2
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="messages.list",le="0.5"} 7
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="messages.list",le="1"} 8
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="messages.list",le="+Inf"} 8
gestaltd_operation_duration_seconds_sum{gestalt_provider="slack",gestalt_operation="messages.list"} 2.4
gestaltd_operation_duration_seconds_count{gestalt_provider="slack",gestalt_operation="messages.list"} 8
`.trim(),
      });
    });

    await page.goto("/admin/");
    await expect(page).toHaveURL(/\/admin\/$/);
    await expect(
      page.getByRole("heading", { name: "Prometheus metrics" }),
    ).toBeVisible();
    await expect(page.locator("#summary-requests")).toHaveText("45");
    await expect(page.locator("#summary-errors")).toHaveText("3");
    await expect(page.locator("#activity-chart")).toHaveAttribute(
      "data-chart-state",
      "ready",
    );
    await expect(page.locator("#latency-chart")).toHaveAttribute(
      "data-chart-state",
      "ready",
    );
    await expect(page.locator("#provider-chart")).toHaveAttribute(
      "data-chart-state",
      "ready",
    );
    await expect(page.locator("#activity-chart canvas")).toHaveCount(1);
    await expect(page.locator("#latency-chart canvas")).toHaveCount(1);
    await expect(page.locator("#provider-chart canvas")).toHaveCount(1);
    const chartColors = await page.evaluate(() => {
      const scope = window as Window & {
        __gestaltAdminTheme?: () => {
          border: string;
          foreground: string;
          surfaceRaised: string;
        };
      };
      return scope.__gestaltAdminTheme ? scope.__gestaltAdminTheme() : null;
    });
    expect(chartColors).not.toBeNull();
    expect(chartColors?.surfaceRaised).toMatch(/^rgba?\(/);
    expect(chartColors?.border).toMatch(/^rgba?\(/);
    expect(chartColors?.foreground).toMatch(/^rgba?\(/);
    await expect(page.getByText("Time window")).toBeVisible();
    await expect(page.locator("#time-window-select")).toHaveValue("1h");
    await expect(page.locator("#refresh-interval-select")).toHaveValue("15000");
    await page.locator("#time-window-select").selectOption("15m");
    await expect(page.locator("#time-window-select")).toHaveValue("15m");
    await expect(page.getByText("Top providers")).toBeVisible();
    await expect(page.locator("#provider-bars")).toContainText("slash\\nname");
    await expect(page.locator("#provider-bars .bar-name").first()).toHaveText("slash\\nname");
    await expect(page.locator("#provider-bars")).toContainText("example");
    await expect(page.locator("#provider-bars")).not.toContainText("unknown");
    await expect(page.locator("#metrics-output")).toHaveCount(0);
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
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await page.route("**/api/v1/auth/logout", (route) => {
      route.fulfill({ json: { status: "ok" } });
    });

    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await page.goto("/");
    await page.getByRole("button", { name: /Logout/i }).click();
    await expect(page).toHaveURL(/\/login/);
    await expect(await page.evaluate(() => localStorage.getItem("user_email"))).toBeNull();
  });

  test("auth callback rejects mismatched OAuth state (CSRF protection)", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      localStorage.clear();
      sessionStorage.clear();
      sessionStorage.setItem("oauth_state", "correct-state");
    });

    await page.goto("/auth/callback?code=test-code&state=wrong-state");
    await expect(page.getByText(/Invalid OAuth state/)).toBeVisible();
    await expect(page.getByText("Back to login")).toBeVisible();
  });

  test("auth callback rejects when no OAuth state was saved (CSRF protection)", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
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

  test("admin metrics 401 clears session and redirects to login", async ({
    page,
  }) => {
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await page.route("**/metrics", (route) => {
      route.fulfill({
        status: 401,
        contentType: "text/plain",
        body: "unauthorized",
      });
    });
    await page.addInitScript(() => {
      const originalRemoveItem = Storage.prototype.removeItem;
      Storage.prototype.removeItem = function (key: string) {
        if (key === "user_email") {
          window.name = "storage-blocked";
          throw new Error("storage blocked");
        }
        return originalRemoveItem.call(this, key);
      };
    });

    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await page.goto("/admin/");
    await page.waitForURL(/\/login/);
    await page.waitForLoadState("domcontentloaded");
    await expect(await page.evaluate(() => window.name)).toBe("storage-blocked");
  });

  test("admin metrics html fallback shows a clear unavailable message", async ({
    page,
  }) => {
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await page.route("**/metrics", (route) => {
      route.fulfill({
        status: 200,
        contentType: "text/html",
        body: "<!doctype html><html><body>not metrics</body></html>",
      });
    });

    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await page.goto("/admin/");
    await expect(
      page.locator("#status").getByText("Prometheus metrics are unavailable."),
    ).toBeVisible();
    await expect(page.locator("#activity-chart")).toHaveAttribute("data-chart-state", "empty");
    await expect(page.locator("#latency-chart")).toHaveAttribute("data-chart-state", "empty");
    await expect(page.locator("#provider-chart")).toHaveAttribute("data-chart-state", "empty");
    await expect(page.locator("#provider-bars")).toContainText(
      "Prometheus metrics are unavailable.",
    );
    await expect(page.locator("#metrics-output")).toHaveCount(0);
  });

  test("admin metrics error body is rendered as text, not injected as HTML", async ({
    page,
  }) => {
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });
    await page.route("**/metrics", (route) => {
      route.fulfill({
        status: 503,
        contentType: "text/plain",
        body: `<img src=x onerror="window.__gestaltXss=1">metrics unavailable`,
      });
    });

    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("user_email", "test@gestalt.dev");
      delete window.__gestaltXss;
    });
    await page.goto("/admin/");
    await expect(page.locator("#status")).toContainText("metrics unavailable");
    await expect(page.locator("#provider-bars")).toContainText(
      `<img src=x onerror="window.__gestaltXss=1">metrics unavailable`,
    );
    await expect(page.locator("#provider-bars img")).toHaveCount(0);
    await expect(page.locator("#metrics-output")).toHaveCount(0);
    const xssMarker = await page.evaluate(() => {
      const scope = window as Window & { __gestaltXss?: number };
      return scope.__gestaltXss ?? null;
    });
    expect(xssMarker).toBeNull();
  });
});
