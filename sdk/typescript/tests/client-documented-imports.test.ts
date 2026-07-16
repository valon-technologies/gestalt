import { expect, test } from "bun:test";

import { createGestaltClient, session } from "../src/client/index.ts";
import { rest } from "../src/client/rest.ts";

test("documented public client imports compile and construct a REST client", async () => {
  const previousLocation = (globalThis as { location?: { origin?: string } }).location;
  (globalThis as { location?: { origin?: string } }).location = {
    origin: "https://gestalt.example",
  };
  try {
    const gestalt = await createGestaltClient({
      transport: rest(),
      auth: session(),
      fetch: (async () => new Response("{}", { status: 200 })) as unknown as typeof fetch,
    });

    expect(gestalt.app).toBeDefined();
    expect(gestalt.identity).toBeDefined();
  } finally {
    if (previousLocation === undefined) {
      delete (globalThis as { location?: { origin?: string } }).location;
    } else {
      (globalThis as { location?: { origin?: string } }).location = previousLocation;
    }
  }
});
