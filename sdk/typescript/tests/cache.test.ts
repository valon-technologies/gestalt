import { expect, test } from "bun:test";

import { Cache, cacheSocketEnv } from "../src/cache.ts";

test("cache socket env normalizes unicode bindings by code point", () => {
  expect(cacheSocketEnv("café")).toBe("GESTALT_CACHE_SOCKET_CAF_");
  expect(cacheSocketEnv("😀")).toBe("GESTALT_CACHE_SOCKET__");
});

test("Cache resolves emoji binding env names with host-compatible normalization", () => {
  const envName = cacheSocketEnv("😀");
  const previous = process.env[envName];
  process.env[envName] = "/tmp/gestalt-cache.sock";
  try {
    expect(() => new Cache("😀")).not.toThrow();
  } finally {
    if (previous === undefined) {
      delete process.env[envName];
    } else {
      process.env[envName] = previous;
    }
  }
});
