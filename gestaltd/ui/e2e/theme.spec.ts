import {
  test,
  expect,
  mockIntegrations,
  mockMetricsOverview,
  mockTokens,
  sampleMetricsOverview,
} from "./fixtures";

test.describe("Theme", () => {
  test("toggle enables dark mode and persists the selection", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await page.addInitScript(() => {
      localStorage.setItem("theme", "light");
    });
    await mockIntegrations(page, []);
    await mockTokens(page, []);
    await mockMetricsOverview(page, sampleMetricsOverview);

    await page.goto("/");

    const toggle = page.getByRole("button", { name: "Toggle theme" });
    await expect(toggle).toHaveAttribute("title", "Light mode");
    await toggle.click();

    await expect
      .poll(async () => page.evaluate(() => localStorage.getItem("theme")))
      .toBe("dark");
    await expect
      .poll(async () =>
        page.evaluate(() =>
          document.documentElement.classList.contains("dark"),
        ),
      )
      .toBe(true);
  });
});
