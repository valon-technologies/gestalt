import {
  test,
  expect,
  mockAuthInfo,
  mockIntegrations,
  mockTokens,
} from "./fixtures";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

function encodeWrappedState(hostState: string): string {
  return Buffer.from(
    JSON.stringify({ host_state: hostState }),
    "utf8",
  ).toString("base64url");
}

async function seedOAuthState(
  page: import("@playwright/test").Page,
  oauthState?: string,
) {
  await page.addInitScript((state) => {
    localStorage.clear();
    sessionStorage.clear();
    if (state) {
      sessionStorage.setItem("oauth_state", state);
    }
  }, oauthState);
}

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
      displayName: "Test SSO",
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
      displayName: "Test SSO",
    });
    await page.goto("/login");
    await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Sign in with Test SSO/i }),
    ).toBeVisible();
  });

  test("no-auth server redirects to dashboard without showing logout", async ({
    page,
  }) => {
    await mockAuthInfo(page, {
      provider: "none",
      displayName: "none",
      loginSupported: false,
    });
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/login");
    await expect(page).toHaveURL("/");
    await expect(page.getByText("anonymous@gestalt")).toBeVisible();
    await expect(page.getByRole("button", { name: /Logout/i })).toHaveCount(0);

    await page.evaluate(() => {
      localStorage.clear();
      localStorage.setItem("user_email", "anonymous@gestalt");
      sessionStorage.clear();
    });

    await page.goto("/");
    await expect(page).toHaveURL("/");
    await expect(page.getByText("anonymous@gestalt")).toBeVisible();
    await expect(page.getByRole("button", { name: /Logout/i })).toHaveCount(0);
  });

  test("authenticated user sees dashboard", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await page.route("**/api/v1/auth/info", (route) => {
      route.abort();
    });
    await mockIntegrations(page, [
      { name: "test-svc", displayName: "Test Service" },
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
    await expect(
      page.getByRole("button", { name: /Logout/i }),
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
      displayName: "Test SSO",
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
    await seedOAuthState(page, "correct-state");

    await page.goto("/auth/callback?code=test-code&state=wrong-state");
    await expect(page.getByText(/Invalid OAuth state/)).toBeVisible();
    await expect(page.getByText("Back to login")).toBeVisible();
  });

  test("auth callback rejects when no OAuth state was saved (CSRF protection)", async ({
    page,
  }) => {
    await seedOAuthState(page);
    await page.goto("/auth/callback?code=attacker-code&state=attacker-state");
    await expect(page.getByText(/Invalid OAuth state/)).toBeVisible();
  });

  test("auth callback accepts wrapped host_state and completes login", async ({
    page,
  }) => {
    const wrappedState = encodeWrappedState("correct-state");

    await seedOAuthState(page, "correct-state");
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    let callbackState: string | null = null;
    await page.route("**/api/v1/auth/login/callback?**", (route, request) => {
      callbackState = new URL(request.url()).searchParams.get("state");
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          email: "test@gestalt.dev",
          displayName: "Test User",
        }),
      });
    });

    await page.goto(`/auth/callback?code=test-code&state=${wrappedState}`);

    await expect.poll(() => callbackState).toBe(wrappedState);
    await expect(page).toHaveURL("/");
    await expect(page.getByText("test@gestalt.dev")).toBeVisible();
  });

  test("auth callback redirects wrapped CLI state to the local listener", async ({
    page,
  }) => {
    const port = 43123;
    const cliState = "cli-original-state";
    const wrappedState = encodeWrappedState(`cli:${port}:${cliState}`);

    await seedOAuthState(page);

    let localCallbackURL: string | null = null;
    let serverCallbackCalled = false;

    await page.route(`http://127.0.0.1:${port}/**`, (route, request) => {
      localCallbackURL = request.url();
      route.fulfill({
        status: 200,
        contentType: "text/html",
        body: "<!doctype html><title>CLI callback</title><p>ok</p>",
      });
    });
    await page.route("**/api/v1/auth/login/callback?**", (route) => {
      serverCallbackCalled = true;
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          email: "unexpected@gestalt.dev",
          displayName: "Unexpected",
        }),
      });
    });

    await page.goto(`/auth/callback?code=test-code&state=${wrappedState}`);

    await expect(page).toHaveURL(new RegExp(`^http://127\\.0\\.0\\.1:${port}/\\?`));
    await expect.poll(() => localCallbackURL).not.toBeNull();
    expect(new URL(localCallbackURL!).searchParams.get("code")).toBe("test-code");
    expect(new URL(localCallbackURL!).searchParams.get("state")).toBe(cliState);
    expect(serverCallbackCalled).toBe(false);
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
      displayName: "Test SSO",
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
      displayName: "Test SSO",
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
      displayName: "Test SSO",
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
