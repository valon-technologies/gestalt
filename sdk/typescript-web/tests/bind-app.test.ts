import { expect, test } from "bun:test";

import { bindApp, bearer, createGestaltClient } from "../src/client.ts";

test("bindApp scopes invoke requests to one app name", async () => {
  let requestUrl = "";
  let body = "";
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
      requestUrl = String(input);
      body = typeof init?.body === "string" ? init.body : "";
      return new Response(
        JSON.stringify({
          status: 200,
          body: btoa(JSON.stringify({ status: "success", data: { ok: true } })),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  const helloWorld = bindApp(client, "helloWorld");
  await expect(
    helloWorld.invoke({ operation: "greet", params: { name: "Ada" } }),
  ).resolves.toEqual({ ok: true });
  expect(requestUrl).toBe(
    "https://gestalt.test/api/v2/app/helloWorld/operations/greet",
  );
  expect(JSON.parse(body)).toEqual({ params: { name: "Ada" } });
});

test("bindApp keeps the bound app when request carries an app field", async () => {
  let requestUrl = "";
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async (input: RequestInfo | URL) => {
      requestUrl = String(input);
      return new Response(
        JSON.stringify({
          status: 200,
          body: btoa(JSON.stringify({ status: "success", data: true })),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  const bound = bindApp(client, "correctApp");
  await bound.invoke({
    app: "wrongApp",
    operation: "sync",
  } as Parameters<typeof bound.invoke>[0]);
  expect(requestUrl).toBe(
    "https://gestalt.test/api/v2/app/correctApp/operations/sync",
  );
});

test("PublicAppInvokeRequest omits optional transport metadata fields", async () => {
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(
        JSON.stringify({
          status: 200,
          body: btoa(JSON.stringify({ status: "success", data: true })),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });

  await expect(
    client.app.invoke({
      app: "example",
      operation: "sync",
      params: { ok: true },
    }),
  ).resolves.toBe(true);
});
