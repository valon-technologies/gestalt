import { test, expect, mockIntegrations, mockTokens } from "./fixtures";
import type { Integration } from "../src/lib/api";

const sampleIntegrations: Integration[] = [
  { name: "slack", display_name: "Slack", description: "Team messaging" },
  { name: "github", display_name: "GitHub", description: "Code hosting" },
  { name: "jira", display_name: "Jira" },
];

test.describe("Integrations", () => {
  test("displays integration cards", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, sampleIntegrations);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await expect(
      page.getByRole("heading", { name: "Integrations" }),
    ).toBeVisible();
    await expect(page.getByText("Slack")).toBeVisible();
    await expect(page.getByText("GitHub")).toBeVisible();
    await expect(page.getByText("Jira")).toBeVisible();
  });

  test("shows descriptions on cards", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, sampleIntegrations);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await expect(page.getByText("Team messaging")).toBeVisible();
    await expect(page.getByText("Code hosting")).toBeVisible();
  });

  test("each card has a Connect button", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, sampleIntegrations);
    await mockTokens(page, []);

    await page.goto("/integrations");
    const connectButtons = page.getByRole("button", { name: "Connect" });
    await expect(connectButtons).toHaveCount(3);
  });

  test("shows empty state when no integrations", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await expect(
      page.getByText("No integrations registered."),
    ).toBeVisible();
  });

  test("shows success flash when redirected after OAuth connect", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { name: "test-provider", display_name: "Test Provider", connected: true },
    ]);

    await page.goto("/integrations?connected=test-provider");
    await expect(page.getByText("test-provider connected successfully.")).toBeVisible();
    await expect(page).toHaveURL(/\/integrations$/);
  });

  test("shows error flash when redirected after OAuth failure", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, []);

    await page.goto("/integrations?error=token+exchange+failed");
    await expect(page.getByText("token exchange failed")).toBeVisible();
    await expect(page).toHaveURL(/\/integrations$/);
  });

  test("connected integration shows badge and hides Connect button", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { name: "slack", display_name: "Slack", description: "Messaging", connected: true },
      { name: "github", display_name: "GitHub", description: "Code" },
    ]);

    await page.goto("/integrations");
    await expect(page.getByText("Slack")).toBeVisible();
    await expect(page.getByText("GitHub")).toBeVisible();
    await expect(page.getByText("Connected")).toBeVisible();
    // Only the unconnected integration should have a Connect button.
    await expect(page.getByRole("button", { name: "Connect" })).toHaveCount(1);
  });
});
