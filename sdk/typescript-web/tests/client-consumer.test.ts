import { expect, test } from "bun:test";

import {
  AppClient,
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

test("generated AppClient is exported from gestalt-web", () => {
  expect(AppClient).toBeDefined();
});

test("createGestaltClient rejects until REST transport lands in SDK-4", async () => {
  await expect(
    createGestaltClient({
      address: "https://gestalt.example.test",
      auth: session(),
    }),
  ).rejects.toThrow("SDK-4");
});
