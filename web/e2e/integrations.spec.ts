import { test, expect, hasLiveBackend } from "./fixtures";

test.describe("Integrations", () => {
  test.skip(!hasLiveBackend, "Requires PLAYWRIGHT_BASE_URL");

  test("integrations page reflects the live backend", async ({ authenticatedPage }) => {
    const page = authenticatedPage;

    await page.goto("/integrations");

    await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();
    await expect(page.getByText("Browse and connect third-party services.")).toBeVisible();
    await expect(page.getByText("Loading...")).toBeHidden();

    const emptyState = page.getByText("No integrations registered.");
    if (await emptyState.count()) {
      await expect(emptyState).toBeVisible();
      return;
    }

    const settingsButtons = page.getByRole("button", { name: / settings$/ });
    await expect(settingsButtons.first()).toBeVisible();

    await settingsButtons.first().click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: /Connect|Disconnect|Add Connection/ })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
