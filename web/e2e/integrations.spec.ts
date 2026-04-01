import { test, expect, mockIntegrations, mockManualConnect, mockTokens } from "./fixtures";
import type { Integration } from "../src/lib/api";

const OAUTH_INTEGRATION: Integration = {
  name: "oauth-svc", display_name: "OAuth Service", description: "Example OAuth integration",
};

const MANUAL_INTEGRATION: Integration = {
  name: "manual-svc", display_name: "Manual Service", description: "Example manual integration", auth_types: ["manual"],
  credential_fields: [{ name: "token", label: "API Token" }],
};

const MANUAL_WITH_LINKED_DESC: Integration = {
  name: "linked-svc", display_name: "Linked Service", auth_types: ["manual"],
  credential_fields: [{ name: "api_key", label: "API Key", description: "Find yours in [Account Settings](https://example.com/settings)" }],
};

const SINGLE_CONNECTION_DUAL_AUTH_INTEGRATION: Integration = {
  name: "dual-svc",
  display_name: "Dual Service",
  auth_types: ["oauth", "manual"],
  credential_fields: [{ name: "api_token", label: "API Token" }],
  connections: [{
    name: "plugin",
    auth_types: ["oauth", "manual"],
    credential_fields: [{ name: "api_token", label: "API Token" }],
  }],
};

const MULTI_CONNECTION_DUAL_AUTH_INTEGRATION: Integration = {
  name: "workspace-svc",
  display_name: "Workspace Service",
  auth_types: ["oauth", "manual"],
  credential_fields: [{ name: "api_token", label: "API Token" }],
  connections: [
    {
      name: "workspace",
      auth_types: ["oauth", "manual"],
      credential_fields: [{ name: "api_token", label: "API Token" }],
    },
    {
      name: "personal",
      auth_types: ["manual"],
      credential_fields: [{ name: "personal_token", label: "Personal Token" }],
    },
  ],
};

const MULTI_CONNECTION_MULTI_OAUTH_INTEGRATION: Integration = {
  name: "team-svc",
  display_name: "Team Service",
  auth_types: ["oauth", "manual"],
  credential_fields: [{ name: "api_token", label: "API Token" }],
  connections: [
    {
      name: "workspace",
      auth_types: ["oauth", "manual"],
      credential_fields: [{ name: "workspace_token", label: "Workspace Token" }],
    },
    {
      name: "personal",
      auth_types: ["oauth", "manual"],
      credential_fields: [{ name: "personal_token", label: "Personal Token" }],
    },
  ],
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

  test("each card has a settings button", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, sampleIntegrations);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Manual Service settings" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Another Service settings" })).toBeVisible();
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
      { ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] },
      MANUAL_INTEGRATION,
    ]);

    await page.goto("/integrations");
    await expect(page.getByText(OAUTH_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByText(MANUAL_INTEGRATION.display_name!)).toBeVisible();
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Manual Service settings" })).toBeVisible();

    // Connected integration's settings shows Reconnect/Disconnect
    await page.getByRole("button", { name: "OAuth Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("default")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Add Connection" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Disconnect" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();

    // Non-connected integration's settings shows Connect
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    await expect(page.getByRole("dialog").getByRole("button", { name: "Connect" })).toBeVisible();
  });

  test("settings modal opens and shows connected status", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("OAuth Service")).toBeVisible();
    await expect(dialog.getByText("default")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Add Connection" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Disconnect" })).toBeVisible();
  });

  test("settings modal closes on backdrop click", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [
      { ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] },
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
      { ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] },
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
      { ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] },
    ]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "OAuth Service settings" }).click();

    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Disconnect" }).click();

    await expect(dialog.getByText("Disconnect OAuth Service?")).toBeVisible();
    await expect(dialog.getByText("This will remove your OAuth Service integration.")).toBeVisible();

    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.getByRole("button", { name: "Add Connection" })).toBeVisible();
  });

  test("disconnect calls API and refreshes list", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let disconnected = false;

    const connectedList = [{ ...OAUTH_INTEGRATION, connected: true, instances: [{ name: "default" }] }];
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
    // Settings gear is still visible (always shown), but integration is now disconnected
    await expect(page.getByRole("button", { name: "OAuth Service settings" })).toBeVisible();
    await page.getByRole("button", { name: "OAuth Service settings" }).click();
    await expect(page.getByRole("dialog").getByText("Not connected")).toBeVisible();
  });

  test("manual auth shows token input on Connect click", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MANUAL_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Connect" }).click();
    await expect(dialog.getByLabel(/API token/i)).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Submit" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
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
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Connect" }).click();
    await dialog.getByLabel(/API token/i).fill("test-api-key-123");

    await page.route("**/api/v1/integrations", (route, request) => {
      if (request.method() === "GET") {
        route.fulfill({ json: connected ? connectedList : disconnectedList });
      } else {
        route.fallback();
      }
    });

    await dialog.getByRole("button", { name: "Submit" }).click();
    await expect(page.getByRole("button", { name: "Manual Service settings" })).toBeVisible();
    expect(receivedCredential).toBe("test-api-key-123");
  });

  test("manual auth Cancel hides the form", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MANUAL_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Connect" }).click();
    await expect(dialog.getByLabel(/API token/i)).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.getByText("Not connected")).toBeVisible();
  });

  test("single-connection dual auth shows both auth actions", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [SINGLE_CONNECTION_DUAL_AUTH_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Dual Service settings" }).click();
    const dialog = page.getByRole("dialog");

    await expect(dialog.getByRole("button", { name: "Connect with OAuth" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Use API Token" })).toBeVisible();
  });

  test("multi-connection dual auth renders actions per connection", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MULTI_CONNECTION_DUAL_AUTH_INTEGRATION]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Workspace Service settings" }).click();
    const dialog = page.getByRole("dialog");

    await expect(dialog.getByRole("button", { name: "Connect with workspace" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Use API Token for workspace" })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Connect with personal" })).toBeVisible();
  });

  test("multi-connection loading state stays on the clicked oauth action", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    let releaseOAuthRequest: (() => void) | undefined;
    const oauthRequestReleased = new Promise<void>((resolve) => {
      releaseOAuthRequest = resolve;
    });

    await mockIntegrations(page, [MULTI_CONNECTION_MULTI_OAUTH_INTEGRATION]);
    await page.route("**/api/v1/auth/start-oauth", async (route) => {
      await oauthRequestReleased;
      await route.fulfill({ status: 500, body: "oauth failed" });
    });

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Team Service settings" }).click();
    const dialog = page.getByRole("dialog");

    await dialog.getByRole("button", { name: "Connect with personal" }).click();
    await expect(dialog.getByRole("button", { name: "Connecting..." })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Connect with workspace" })).toBeVisible();

    releaseOAuthRequest?.();
    await expect(dialog.getByText("oauth failed")).toBeVisible();
  });

  test("manual auth reconnect opens token form via settings modal", async ({
    authenticatedPage,
  }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [{ ...MANUAL_INTEGRATION, connected: true, instances: [{ name: "default" }] }]);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Manual Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("default")).toBeVisible();
    await dialog.getByRole("button", { name: "Add Connection" }).click();
    await expect(dialog.getByLabel("Connection name")).toBeVisible();
    await dialog.getByLabel("Connection name").fill("second");
    await dialog.getByRole("button", { name: "Continue" }).click();
    await expect(dialog.getByLabel(/API token/i)).toBeVisible();
  });

  test("credential field description renders inline links", async ({ authenticatedPage }) => {
    const page = authenticatedPage;
    await mockIntegrations(page, [MANUAL_WITH_LINKED_DESC]);
    await mockTokens(page, []);

    await page.goto("/integrations");
    await page.getByRole("button", { name: "Linked Service settings" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Connect" }).click();

    const link = dialog.getByRole("link", { name: "Account Settings" });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", "https://example.com/settings");
    await expect(link).toHaveAttribute("target", "_blank");
    await expect(dialog.getByText("Find yours in")).toBeVisible();
  });
});
