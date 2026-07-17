import { expect, test } from "bun:test";

import {
  AppClient,
  GestaltError,
  InvokeError,
  bearer,
  createGestaltClient,
  session,
  unauthenticated,
} from "../src/index.ts";

test("web client auth helpers are available", () => {
  expect(session()).toEqual({ kind: "session" });
  expect(bearer(() => "token").kind).toBe("bearer");
  expect(unauthenticated()).toEqual({ kind: "unauthenticated" });
});

test("package.json declares Vite 8 in peerDependencies", async () => {
  const pkg = await Bun.file(
    new URL("../package.json", import.meta.url),
  ).json();
  expect(pkg.peerDependencies.vite).toContain("^8.0.0");
});

test("generated AppClient and error types are exported from gestalt-web", () => {
  expect(AppClient).toBeDefined();
  expect(GestaltError).toBeDefined();
  expect(InvokeError).toBeDefined();
});

test("createGestaltClient returns a browser REST client", async () => {
  const client = createGestaltClient({
    address: "https://gestalt.example.test",
    auth: session(),
    fetch: (async () =>
      new Response(
        JSON.stringify({
          status: 200,
          body: btoa(JSON.stringify({ status: "success", data: {} })),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });
  expect(client.address).toBe("https://gestalt.example.test");
  expect(client.app).toBeInstanceOf(AppClient);
});
