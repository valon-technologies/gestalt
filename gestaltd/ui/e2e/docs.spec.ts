import { test, expect, mockAuthInfo } from "./fixtures";

const hasBackend = !!process.env.PLAYWRIGHT_BASE_URL;

test.describe("Docs page", () => {
  test.skip(
    hasBackend,
    "Docs page test uses mocked auth info and does not apply when running against a real server",
  );

  test("docs are reachable before login and cover the main user workflows", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });

    await page.goto("/login");
    await page.getByRole("link", { name: "documentation" }).click();

    await expect(page).toHaveURL(/\/docs/);
    await expect(
      page.getByRole("heading", {
        name: "Use Gestalt from the terminal, browser, or any MCP-aware client.",
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Install the `gestalt` CLI" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Connect an MCP client to `gestaltd`" }),
    ).toBeVisible();
  });
});
