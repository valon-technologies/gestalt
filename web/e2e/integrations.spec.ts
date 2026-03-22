import { test, expect, mockIntegrations, mockManualConnect, mockTokens } from "./fixtures";
import type { Integration } from "../src/lib/api";

const OAUTH_INTEGRATION: Integration = {
  name: "oauth-svc", display_name: "OAuth Service", description: "Example OAuth integration",
};

const MANUAL_INTEGRATION: Integration = {
  name: "manual-svc", display_name: "Manual Service", description: "Example manual integration", auth_type: "manual",
};

const sampleIntegrations: Integration[] = [
  OAUTH_INTEGRATION,
  MANUAL_INTEGRATION,
  { name: "another-svc", display_name: "Another Service" },
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
    await expect(page.getByText(OAUTH_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByText(MANUAL_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByText("Another Service")).toBeVisible();
  });

  test("shows descriptions on cards", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, sampleIntegrations);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await expect(page.getByText(OAUTH_INTEGRATION.description!)).toBeVisible();
    await expect(page.getByText(MANUAL_INTEGRATION.description!)).toBeVisible();
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

  test("connected integration shows check icon and settings gear", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true },
      MANUAL_INTEGRATION,
    ]);

    await page.goto("/integrations");
    await expect(page.getByText(OAUTH_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByText(MANUAL_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Connect", exact: true })).toHaveCount(1);
    await expect(page.getByRole("button", { name: "Reconnect", exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Disconnect", exact: true })).toHaveCount(0);
  });

  test("settings modal opens and shows connected status", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("OAuth Service")).toBeVisible();
    await expect(dialog.getByText("Connected")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Reconnect" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Disconnect" })).toBeVisible();
  });

  test("settings modal closes on backdrop click", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();

    // Click outside the centered modal panel
    await page.mouse.click(10, 10);
    await expect(page.getByRole("dialog")).not.toBeVisible();
  });

  test("settings modal closes on ESC", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).not.toBeVisible();
  });

  test("disconnect confirmation shows warning and allows cancel", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();

    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Disconnect" }).click();

    await expect(dialog.getByText("Disconnect OAuth Service?")).toBeVisible();
    await expect(dialog.getByText("This will remove your OAuth Service integration.")).toBeVisible();

    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.getByRole("button", { name: "Reconnect" })).toBeVisible();
  });

  test("disconnect calls API and refreshes list", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let disconnected = false;

    const connectedList = [{ ...OAUTH_INTEGRATION, connected: true }];
    const disconnectedList = [{ ...OAUTH_INTEGRATION, connected: false }];

    await mockIntegrations(page, connectedList, {
      onDisconnect: () => { disconnected = true; },
    });

    await page.goto("/integrations");
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).toBeVisible();

    // Re-mock so GET returns disconnected state after DELETE fires
    await page.route("**/api/v1/integrations", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: disconnected ? disconnectedList : connectedList });
      } else {
        route.fallback();
      }
    });

    await page.getByRole("button", { name: "OAuth Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Disconnect" }).click();
    // Confirm the disconnect
    await dialog.getByRole("button", { name: "Disconnect" }).click();

    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Connect" })).toBeVisible();
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).not.toBeVisible();
  });

  test("manual auth shows token input on Connect click", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MANUAL_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Connect" }).click();
    await expect(page.getByLabel("API Token")).toBeVisible();
    await expect(page.getByRole("button", { name: "Submit" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
  });

  test("manual auth submits credential and refreshes", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let connected = false;
    let receivedCredential = "";

    const disconnectedList: Integration[] = [MANUAL_INTEGRATION];
    const connectedList: Integration[] = [{ ...MANUAL_INTEGRATION, connected: true }];

    await mockIntegrations(page, disconnectedList);
    await mockManualConnect(page, {
      onConnect: (_name, cred) => {
        connected = true;
        receivedCredential = cred;
      },
    });

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Connect" }).click();
    await page.getByLabel("API Token").fill("test-api-key-123");

    await page.route("**/api/v1/integrations", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: connected ? connectedList : disconnectedList });
      } else {
        route.fallback();
      }
    });

    await page.getByRole("button", { name: "Submit" }).click();
    await expect(page.getByRole("button", { name: "Manual Service settings" })).toBeVisible();
    expect(receivedCredential).toBe("test-api-key-123");
  });

  test("manual auth Cancel hides the form", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MANUAL_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Connect" }).click();
    await expect(page.getByLabel("API Token")).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByLabel("API Token")).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Connect" })).toBeVisible();
  });

  test("manual auth reconnect opens token form via settings modal", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [{ ...MANUAL_INTEGRATION, connected: true }]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Connected")).toBeVisible();
    await dialog.getByRole("button", { name: "Reconnect" }).click();
    // Modal must close so the token form isn't trapped behind the overlay
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(page.getByLabel("API Token")).toBeVisible();
  });
});
