import { expect, test } from "@playwright/test";

test("getting started covers local UI, in-product docs, and plugin CLI usage", async ({
  page,
}) => {
  await page.goto("/getting-started.html");

  const body = page.locator("body");

  await expect(page).toHaveTitle(/Getting Started/);
  await expect(body).toContainText("http://localhost:8080/");
  await expect(body).toContainText("http://localhost:8080/docs");
  await expect(body).toContainText("gestalt plugins list");
  await expect(body).toContainText(
    "claude mcp add --transport http --scope project gestalt http://localhost:8080/mcp",
  );
  await expect(body).toContainText(
    "codex mcp add gestalt --url http://localhost:8080/mcp",
  );
});

test("client index distinguishes in-product docs from the full docs site", async ({
  page,
}) => {
  await page.goto("/client.html");

  const body = page.locator("body");

  await expect(page).toHaveTitle(/Clients/);
  await expect(body).toContainText("in-product user guide at /docs");
  await expect(body).toContainText(
    "cover the full product, including local setup, configuration, and deployment",
  );
});

test("web UI docs use Plugins terminology and the correct UI source key", async ({
  page,
}) => {
  await page.goto("/client/web.html");

  const body = page.locator("body");

  await expect(page).toHaveTitle(/Web UI/);
  await expect(body).toContainText("The Plugins page lists all configured plugins.");
  await expect(body).toContainText("in-product user guide at /docs");
  await expect(body).toContainText("providers.ui.source");
  await expect(body).not.toContainText("ui.provider.source");
});
