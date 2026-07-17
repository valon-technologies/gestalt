import { expect, test } from "bun:test";

import { bearer, createGestaltClient, rest } from "@valon-technologies/gestalt/client";

test("client subpath exports documented server-side API", () => {
  expect(typeof createGestaltClient).toBe("function");
  expect(typeof rest).toBe("function");
  expect(typeof bearer).toBe("function");
});
