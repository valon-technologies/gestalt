import { expect, test } from "bun:test";

test("gestalt-web root import does not throw during SSR", async () => {
  const previousLocation = globalThis.location;
  // @ts-expect-error simulate SSR without a browser location.
  delete globalThis.location;
  try {
    const mod = await import("../src/index.ts");
    expect(mod.createGestaltClient).toBeTypeOf("function");
    expect(mod.GestaltError).toBeDefined();
    expect(mod.InvokeError).toBeDefined();
  } finally {
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: previousLocation,
    });
  }
});

test("createGestaltClient requires explicit address when location is unavailable", async () => {
  const previousLocation = globalThis.location;
  // @ts-expect-error simulate SSR without a browser location.
  delete globalThis.location;
  try {
    const { createGestaltClient, session } = await import("../src/index.ts");
    expect(() => createGestaltClient({ auth: session() })).toThrow(
      /explicit address/,
    );
  } finally {
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: previousLocation,
    });
  }
});
