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

test("createGestaltClient rejects until transport wiring lands in SDK-5", async () => {
  await expect(
    createGestaltClient({
      address: "https://gestalt.example.test",
      transport: rest(),
      auth: bearer(() => "token"),
    }),
  ).rejects.toThrow("SDK-5");
});
