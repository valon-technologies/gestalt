import { expect, test } from "bun:test";

import {
  bearer,
  createGestaltClient,
  grpc,
  rest,
  unauthenticated,
} from "../src/client/index.ts";

test("server client auth and transport helpers are available", () => {
  expect(bearer(() => "token").kind).toBe("bearer");
  expect(unauthenticated()).toEqual({ kind: "unauthenticated" });
  expect(rest()).toEqual({ kind: "rest" });
  expect(grpc()).toEqual({ kind: "grpc" });
});

test("createGestaltClient wires REST transport", async () => {
  const client = await createGestaltClient({
    address: "https://gestalt.example.test",
    transport: rest(),
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(
        JSON.stringify({ status: 418, body: "", headers: {} }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });

  try {
    expect(client.app).toBeDefined();
    expect(typeof client.close).toBe("function");
  } finally {
    await client.close();
  }
});
