import { test, expect } from "@playwright/test";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Integration: Go server contract", () => {
  test.skip(!hasBackend, "Requires PLAYWRIGHT_BASE_URL (real Go server)");

  test.describe.configure({ mode: "serial" });

  let sessionCookie: string;
  let serverHost: string;

  async function devLogin(
    baseURL: string,
    email = "e2e@gestalt.dev",
    retries = 3,
  ): Promise<string> {
    for (let attempt = 1; attempt <= retries; attempt++) {
      const res = await fetch(`${baseURL}/api/dev-login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (res.ok) {
        const setCookie = res.headers.get("set-cookie") || "";
        const match = setCookie.match(/session_token=([^;]+)/);
        if (match) return match[1];
      }
      if (attempt < retries) {
        await new Promise((r) => setTimeout(r, 1000));
      }
    }
    throw new Error("dev-login failed after retries");
  }

  test.beforeAll(async ({}, testInfo) => {
    const baseURL =
      testInfo.project.use.baseURL || "http://localhost:8080";
    serverHost = new URL(baseURL).hostname;
    sessionCookie = await devLogin(baseURL);
  });

  async function injectAuth(page: import("@playwright/test").Page) {
    await page.context().addCookies([{
      name: "session_token",
      value: sessionCookie,
      domain: serverHost,
      path: "/",
      httpOnly: true,
      secure: false,
    }]);
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

  test("dashboard loads from Go server", async ({ page }) => {
    await injectAuth(page);
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Dashboard" }),
    ).toBeVisible();
  });

  test("unauthenticated user is redirected to login", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("logout clears session and redirects to login", async ({ page }) => {
    await injectAuth(page);
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Dashboard" }),
    ).toBeVisible();

    await page.getByRole("button", { name: /log\s*out/i }).click();
    await expect(page).toHaveURL(/\/login/);
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
