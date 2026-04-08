import { test, expect } from "@playwright/test";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Integration: Go server contract", () => {
  test.skip(!hasBackend, "Requires PLAYWRIGHT_BASE_URL (real Go server)");

  test.describe.configure({ mode: "serial" });

  async function injectAuth(page: import("@playwright/test").Page) {
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
});
