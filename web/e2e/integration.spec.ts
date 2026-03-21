import { test, expect } from "@playwright/test";

const hasBackend = !!process.env.GESTALT_API_URL;

test.describe("Integration: Go server contract", () => {
  test.skip(!hasBackend, "Requires GESTALT_API_URL (real Go server)");

  test.describe.configure({ mode: "serial" });

  let authToken: string;

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
        const data = await res.json();
        if (data.token) return data.token;
      }
      if (attempt < retries) {
        await new Promise((r) => setTimeout(r, 1000));
      }
    }
    throw new Error("dev-login failed after retries");
  }

  test.beforeAll(async ({}, testInfo) => {
    const baseURL =
      testInfo.project.use.baseURL || "http://localhost:3000";
    authToken = await devLogin(baseURL);
  });

  function injectAuth(page: import("@playwright/test").Page) {
    return page.addInitScript(
      ({ token }) => {
        localStorage.setItem("session_token", token);
        localStorage.setItem("user_email", "e2e@gestalt.dev");
      },
      { token: authToken },
    );
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
