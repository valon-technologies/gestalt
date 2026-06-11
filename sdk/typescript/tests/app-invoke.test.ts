import { expect, test } from "bun:test";

import { invokeGraphQL } from "../src/app-invoke.ts";
import type { App, OperationResult } from "../src/app.ts";
import { InvokeError } from "../src/invoke_support.ts";

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

function result(body: string, status = 200): OperationResult {
  return { status, body: bytes(body), headers: {} };
}

interface RecordedInvoke {
  args: unknown[];
}

function fakeApp(response: OperationResult, recorded: RecordedInvoke): App {
  return {
    invokeGraphQL: (...args: unknown[]) => {
      recorded.args = args;
      return Promise.resolve(response);
    },
  } as unknown as App;
}

test("invokeGraphQL validates the document and decodes through the generated pieces", async () => {
  const recorded: RecordedInvoke = { args: [] };
  const app = fakeApp(result(`{"data":{"viewer":{"id":"user-1"}},"errors":[]}`), recorded);
  const decoded = await invokeGraphQL(app, "linear", " query { viewer { id } } ", {
    variables: { first: 1 },
    idempotencyKey: " gq-1 ",
  });
  expect(JSON.stringify(decoded)).toBe(`{"data":{"viewer":{"id":"user-1"}},"errors":[]}`);
  expect(recorded.args.slice(0, 2)).toEqual(["linear", "query { viewer { id } }"]);
  expect(recorded.args[2]).toEqual({ idempotencyKey: "gq-1", variables: { first: 1 } });

  expect(invokeGraphQL(app, "linear", "   ")).rejects.toThrow(InvokeError);

  const errors = fakeApp(result(`{"data":null,"errors":[{"message":"boom"}]}`), { args: [] });
  expect(invokeGraphQL(errors, "linear", "query { x }")).rejects.toThrow(InvokeError);
});
