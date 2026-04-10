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
      displayName: "Test SSO",
    });

    await page.goto("/login");
    await page.getByRole("link", { name: "documentation" }).click();

    await expect(page).toHaveURL(/\/docs/);
    await expect(
      page.getByRole("heading", {
        name: "Gestalt User Guide",
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Set Up The CLI" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Use With MCP" }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "http://localhost:8080/mcp" }),
    ).toBeVisible();
    await expect(page.getByText("Claude Code")).toBeVisible();
  });

  test("authenticated user can access docs without redirect", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockAuthInfo(page, {
      provider: "test-sso",
      display_name: "Test SSO",
    });

    await page.goto("/docs");
    await expect(page).toHaveURL(/\/docs/);
    await expect(
      page.getByRole("heading", { name: "Gestalt User Guide" }),
    ).toBeVisible();
    await expect(page.getByText("test@gestalt.dev")).toBeVisible();
  });
});
