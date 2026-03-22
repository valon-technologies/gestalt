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
    const connectButtons = page.getByRole("button", { name: "Connect", exact: true });
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

  test("connected integration shows badge, Reconnect, and Disconnect", async ({
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
    await expect(page.getByRole("button", { name: "Connect", exact: true })).toHaveCount(1);
    await expect(page.getByRole("button", { name: "Reconnect", exact: true })).toHaveCount(1);
    await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toHaveCount(1);
  });

  test("disconnect calls API and refreshes list", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let disconnected = false;

    const connectedList = [
      { name: "slack", display_name: "Slack", description: "Messaging", connected: true },
    ];
    const disconnectedList = [
      { name: "slack", display_name: "Slack", description: "Messaging", connected: false },
    ];

    await mockIntegrations(page, connectedList, {
      onDisconnect: () => { disconnected = true; },
    });

    await page.goto("/integrations");
    await expect(page.getByText("Connected")).toBeVisible();
    await expect(page.getByRole("button", { name: "Disconnect" })).toBeVisible();

    // After disconnect, re-mock to return disconnected state
    await page.route("**/api/v1/integrations", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: disconnected ? disconnectedList : connectedList });
      } else {
        route.fallback();
      }
    });

    await page.getByRole("button", { name: "Disconnect" }).click();
    await expect(page.getByRole("button", { name: "Connect" })).toBeVisible();
    await expect(page.getByText("Connected")).not.toBeVisible();
  });
});
