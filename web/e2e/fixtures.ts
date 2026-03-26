import { test as base, expect, type Page } from "@playwright/test";

export const hasLiveBackend = !!process.env.PLAYWRIGHT_BASE_URL;

function uniqueEmail(seed: string): string {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return `e2e-${seed}-${suffix}@gestalt.dev`;
}

export async function loginAsDevUser(
  page: Page,
  seed = "test",
): Promise<string> {
  const email = uniqueEmail(seed);

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Gestalt" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Sign in with / }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Dev Login" })).toBeVisible();

  await page.locator('input[name="email"]').fill(email);
  await page.getByRole("button", { name: "Dev Login" }).click();

  await page.waitForURL((url) => url.pathname === "/");
  await expect(page.getByText(email)).toBeVisible();

  return email;
}

type CustomFixtures = {
  authenticatedPage: Page;
  authenticatedEmail: string;
};

export const test = base.extend<CustomFixtures>({
  authenticatedEmail: async ({ page }, use) => {
    const email = await loginAsDevUser(page, "authenticated");
    await use(email);
  },
  authenticatedPage: async ({ page, authenticatedEmail }, use) => {
    await expect(page.getByText(authenticatedEmail)).toBeVisible();
    await use(page);
  },
});

export { expect };
