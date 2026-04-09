import { expect, test } from "bun:test";

import { connectionParam, ok, request, response } from "../src/api.ts";
import { definePlugin, operation } from "../src/plugin.ts";
import { s } from "../src/schema.ts";

test("plugin executes operations and exposes catalog metadata", async () => {
  const plugin = definePlugin({
    name: "@scope/demo provider",
    displayName: "Demo Provider",
    description: "Plugin used by unit tests",
    operations: [
      operation({
        id: "sum",
        method: "post",
        title: "Add",
        description: "Add two numbers",
        tags: ["math"],
        readOnly: false,
        visible: false,
        input: s.object({
          a: s.integer(),
          b: s.integer({ default: 1 }),
        }),
        output: s.object({
          total: s.integer(),
          token: s.string(),
        }),
        handler(input, req) {
          return ok({
            total: input.a + input.b,
            token: req.token,
          });
        },
      }),
      operation({
        id: "explode",
        handler() {
          throw new Error("boom");
        },
      }),
    ],
  });

  const result = await plugin.execute("sum", { a: "2" }, request("tok"));
  expect(result.status).toBe(200);
  expect(JSON.parse(result.body)).toEqual({
    total: 3,
    token: "tok",
  });
  expect(connectionParam(request("tok", { region: "iad" }), "region")).toBe("iad");
  expect(connectionParam(request(), "missing")).toBeUndefined();

  const invalid = await plugin.execute("sum", { a: "bad" }, request());
  expect(invalid.status).toBe(400);
  expect(JSON.parse(invalid.body)).toEqual({
    error: "$.a must be an integer",
  });

  const unknown = await plugin.execute("missing", {}, request());
  expect(unknown.status).toBe(404);

  const exploded = await plugin.execute("explode", {}, request());
  expect(exploded.status).toBe(500);
  expect(JSON.parse(exploded.body)).toEqual({
    error: "boom",
  });

  expect(plugin.staticCatalog()).toEqual({
    name: "demo-provider",
    displayName: "Demo Provider",
    description: "Plugin used by unit tests",
    operations: [
      {
        id: "sum",
        method: "POST",
        title: "Add",
        description: "Add two numbers",
        parameters: [
          { name: "a", type: "integer", required: true },
          { name: "b", type: "integer", default: 1 },
        ],
        inputSchema: {
          type: "object",
          properties: {
            a: { type: "integer" },
            b: { type: "integer", default: 1 },
          },
          required: ["a"],
        },
        outputSchema: {
          type: "object",
          properties: {
            total: { type: "integer" },
            token: { type: "string" },
          },
          required: ["total", "token"],
        },
        tags: ["math"],
        readOnly: false,
        visible: false,
      },
      {
        id: "explode",
        method: "POST",
      },
    ],
  });
});

test("plugin normalizes operation identifiers before storing and executing", async () => {
  const plugin = definePlugin({
    operations: [
      {
        id: "  ping  ",
        handler() {
          return {
            pong: true,
          };
        },
      },
    ],
  });

  const result = await plugin.execute("ping", {}, request());
  expect(result.status).toBe(200);
  expect(JSON.parse(result.body)).toEqual({
    pong: true,
  });
  expect(plugin.staticCatalog().operations).toEqual([
    {
      id: "ping",
      method: "POST",
    },
  ]);
});

test("plugin rejects duplicate operation identifiers after trimming", () => {
  expect(() =>
    definePlugin({
      operations: [
        {
          id: "ping",
          handler() {
            return { pong: true };
          },
        },
        {
          id: " ping ",
          handler() {
            return { pong: false };
          },
        },
      ],
    }),
  ).toThrow('duplicate operation id "ping"');
});

test("plugin treats raw outputs with a body field as plain output values", async () => {
  const plugin = definePlugin({
    operations: [
      operation({
        id: "echo",
        output: s.object({
          body: s.string(),
        }),
        handler() {
          return {
            body: "hello",
          };
        },
      }),
    ],
  });

  const result = await plugin.execute("echo", {}, request());
  expect(result.status).toBe(200);
  expect(JSON.parse(result.body)).toEqual({
    body: "hello",
  });
});

test("plugin treats raw outputs with status and body fields as plain output values", async () => {
  const plugin = definePlugin({
    operations: [
      operation({
        id: "echo",
        output: s.object({
          status: s.integer(),
          body: s.string(),
        }),
        handler() {
          return {
            status: 42,
            body: "payload",
          };
        },
      }),
    ],
  });

  const result = await plugin.execute("echo", {}, request());
  expect(result.status).toBe(200);
  expect(JSON.parse(result.body)).toEqual({
    status: 42,
    body: "payload",
  });
});

test("plugin accepts explicit branded response wrappers with a status field", async () => {
  const plugin = definePlugin({
    operations: [
      operation({
        id: "created",
        output: s.object({
          id: s.string(),
        }),
        handler() {
          return response(201, {
            id: "new-id",
          });
        },
      }),
    ],
  });

  const result = await plugin.execute("created", {}, request());
  expect(result.status).toBe(201);
  expect(JSON.parse(result.body)).toEqual({
    id: "new-id",
  });
});
