import { test as base, expect, type Page, type Route } from "@playwright/test";
import type {
  APIToken,
  Integration,
  OperationMetricsOverview,
} from "../src/lib/api";

export async function mockIntegrations(
  page: Page,
  integrations: Integration[],
  opts?: { onDisconnect?: (name: string) => void },
) {
  await page.route("**/api/v1/integrations", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: integrations });
    } else {
      route.fallback();
    }
  });

  await page.route("**/api/v1/integrations/*", (route: Route, request) => {
    if (request.method() === "DELETE") {
      const url = new URL(request.url());
      const name = url.pathname.split("/").pop() || "";
      opts?.onDisconnect?.(name);
      route.fulfill({ json: { status: "disconnected" } });
    } else {
      route.fallback();
    }
  });
}

export async function mockManualConnect(
  page: Page,
  opts?: { onConnect?: (integration: string, credential: string) => void },
) {
  await page.route(
    "**/api/v1/auth/connect-manual",
    async (route: Route, request) => {
      if (request.method() === "POST") {
        const body = request.postDataJSON() as {
          integration: string;
          credential: string;
        };
        opts?.onConnect?.(body.integration, body.credential);
        await route.fulfill({ json: { status: "connected" } });
      } else {
        await route.fallback();
      }
    },
  );
}

export async function mockAuthInfo(
  page: Page,
  info: { provider: string; display_name: string },
) {
  await page.route("**/api/v1/auth/info", (route: Route) => {
    route.fulfill({ json: info });
  });
}

export async function mockTokens(page: Page, tokens: APIToken[]) {
  await page.route("**/api/v1/tokens", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: tokens });
    } else {
      route.fallback();
    }
  });
}

export async function mockMetricsOverview(
  page: Page,
  metrics: OperationMetricsOverview,
) {
  await page.route("**/api/v1/metrics/overview**", (route: Route, request) => {
    if (request.method() === "GET") {
      route.fulfill({ json: metrics });
    } else {
      route.fallback();
    }
  });
}

export const sampleMetricsOverview: OperationMetricsOverview = {
  enabled: true,
  generated_at: "2026-04-03T15:30:00Z",
  window_seconds: 3600,
  bucket_seconds: 60,
  summary: {
    requests: 1240,
    errors: 9,
    error_rate: 0.007258064516129032,
    avg_latency_ms: 143.2,
    p95_latency_ms: 421.4,
    throughput_rps: 1.38,
  },
  series: [
    {
      start: "2026-04-03T15:15:00Z",
      requests: 82,
      errors: 0,
      error_rate: 0,
      p95_latency_ms: 188.2,
      throughput_rps: 1.37,
    },
    {
      start: "2026-04-03T15:16:00Z",
      requests: 91,
      errors: 1,
      error_rate: 0.01098901098901099,
      p95_latency_ms: 243.9,
      throughput_rps: 1.52,
    },
    {
      start: "2026-04-03T15:17:00Z",
      requests: 102,
      errors: 0,
      error_rate: 0,
      p95_latency_ms: 221.7,
      throughput_rps: 1.7,
    },
    {
      start: "2026-04-03T15:18:00Z",
      requests: 108,
      errors: 0,
      error_rate: 0,
      p95_latency_ms: 187.4,
      throughput_rps: 1.8,
    },
    {
      start: "2026-04-03T15:19:00Z",
      requests: 121,
      errors: 2,
      error_rate: 0.01652892561983471,
      p95_latency_ms: 311.8,
      throughput_rps: 2.02,
    },
  ],
  providers: [
    {
      provider: "github",
      requests: 640,
      errors: 4,
      error_rate: 0.00625,
      avg_latency_ms: 122.1,
      p95_latency_ms: 310.4,
      throughput_rps: 0.71,
    },
    {
      provider: "linear",
      requests: 420,
      errors: 3,
      error_rate: 0.007142857142857143,
      avg_latency_ms: 148.3,
      p95_latency_ms: 421.2,
      throughput_rps: 0.47,
    },
    {
      provider: "asana",
      requests: 180,
      errors: 2,
      error_rate: 0.011111111111111112,
      avg_latency_ms: 164.9,
      p95_latency_ms: 356.7,
      throughput_rps: 0.2,
    },
  ],
  operations: [
    {
      provider: "github",
      operation: "issues.list",
      requests: 280,
      errors: 1,
      error_rate: 0.0035714285714285713,
      avg_latency_ms: 98.4,
      p95_latency_ms: 201.2,
      throughput_rps: 0.31,
    },
    {
      provider: "linear",
      operation: "issues.create",
      requests: 214,
      errors: 2,
      error_rate: 0.009345794392523364,
      avg_latency_ms: 176.5,
      p95_latency_ms: 398.1,
      throughput_rps: 0.24,
    },
    {
      provider: "asana",
      operation: "tasks.list",
      requests: 132,
      errors: 1,
      error_rate: 0.007575757575757576,
      avg_latency_ms: 194.7,
      p95_latency_ms: 341.4,
      throughput_rps: 0.15,
    },
  ],
};

type CustomFixtures = {
  authenticatedPage: Page;
};

export const test = base.extend<CustomFixtures>({
  authenticatedPage: async ({ page }, use) => {
    await page.addInitScript(() => {
      localStorage.setItem("user_email", "test@gestalt.dev");
    });
    await use(page);
  },
});

export { expect };
