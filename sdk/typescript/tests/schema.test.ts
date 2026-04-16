import { expect, test } from "bun:test";

import { array, definePlugin, PluginProvider } from "../src/index.ts";
import { s } from "../src/schema.ts";

test("schema parses defaults and optional fields", () => {
  const input = s.object({
    name: s.string({ default: "World" }),
    count: s.integer(),
    verbose: s.optional(s.boolean()),
  });

  const parsedDefault = input.parse({ count: "3" }, "$") as Record<string, unknown>;
  expect(parsedDefault).toEqual({
    name: "World",
    count: 3,
  });
  expect("verbose" in parsedDefault).toBe(false);

  expect(input.parse({ count: 5, verbose: "true" }, "$")).toEqual({
    name: "World",
    count: 5,
    verbose: true,
  });
});

test("schema raises clear validation errors", () => {
  expect(() =>
    s.object({
      enabled: s.boolean(),
    }).parse({}, "$"),
  ).toThrow("$.enabled is required");

  expect(() => s.array(s.number()).parse("bad", "$")).toThrow("$ must be an array");
  expect(() => s.integer().parse("12.5", "$")).toThrow("$ must be an integer");
  expect(() => s.integer().parse("12px", "$")).toThrow("$ must be an integer");
  expect(() => s.number().parse("Infinity", "$")).toThrow("$ must be a number");
  expect(() => s.number().parse("12px", "$")).toThrow("$ must be a number");
  expect(() => s.number().parse("123abc", "$")).toThrow("$ must be a number");
});

test("number schema accepts full finite numeric strings", () => {
  expect(s.number().parse("12.5", "$")).toBe(12.5);
  expect(s.number().parse("1e3", "$")).toBe(1000);
  expect(s.number().parse(".25", "$")).toBe(0.25);
});

test("schema builders are exported from the package entrypoint", () => {
  expect(array(s.integer()).parse(["1", 2], "$")).toEqual([1, 2]);

  const plugin = definePlugin({
    operations: [
      {
        id: "ping",
        handler() {
          return {
            ok: true,
          };
        },
      },
    ],
  });
  expect(plugin).toBeInstanceOf(PluginProvider);
});
