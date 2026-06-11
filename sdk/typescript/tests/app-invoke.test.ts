import { expect, test } from "bun:test";

import { invokeGraphQLJson, invokeJson } from "../src/app-invoke.ts";
import type { App, OperationResult } from "../src/app.ts";
import { InvokeError } from "../src/invoke-error.ts";

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
    invoke: (...args: unknown[]) => {
      recorded.args = args;
      return Promise.resolve(response);
    },
    invokeGraphQL: (...args: unknown[]) => {
      recorded.args = args;
      return Promise.resolve(response);
    },
  } as unknown as App;
}

test("invokeJson flattens options and unwraps the success envelope", async () => {
  const recorded: RecordedInvoke = { args: [] };
  const app = fakeApp(result(`{"status":"success","data":{"id":1}}`), recorded);
  const decoded = await invokeJson(app, "github", "get_issue", { id: 1 }, {
    connection: "conn-1",
    idempotencyKey: " key-1 ",
    credentialMode: " user ",
  });
  expect(JSON.stringify(decoded)).toBe(`{"id":1}`);
  expect(recorded.args.slice(0, 6)).toEqual([
    "github",
    "get_issue",
    "conn-1",
    "",
    "key-1",
    "user",
  ]);
  expect(JSON.stringify(recorded.args[6])).toBe(`{"id":1}`);
});

test("invokeJson defaults params to an empty object and throws on error envelopes", async () => {
  const recorded: RecordedInvoke = { args: [] };
  const ok = fakeApp(result(`{"ok":true}`), recorded);
  await invokeJson(ok, "github", "ping");
  expect(JSON.stringify(recorded.args[6])).toBe(`{}`);

  const failing = fakeApp(result(`{"status":"error","error":{"code":"nope","message":"denied"}}`), {
    args: [],
  });
  expect(invokeJson(failing, "github", "ping")).rejects.toThrow(InvokeError);

  const http = fakeApp(result(`{"message":"unauthorized"}`, 401), { args: [] });
  expect(invokeJson(http, "github", "ping")).rejects.toThrow(InvokeError);
});

test("invokeGraphQLJson validates the document and surfaces GraphQL errors", async () => {
  const recorded: RecordedInvoke = { args: [] };
  const app = fakeApp(result(`{"data":{"viewer":{"id":"user-1"}},"errors":[]}`), recorded);
  const decoded = await invokeGraphQLJson(app, "linear", " query { viewer { id } } ", {
    variables: { first: 1 },
    idempotencyKey: " gq-1 ",
  });
  expect(JSON.stringify(decoded)).toBe(`{"data":{"viewer":{"id":"user-1"}},"errors":[]}`);
  expect(recorded.args.slice(0, 5)).toEqual([
    "linear",
    "query { viewer { id } }",
    "",
    "",
    "gq-1",
  ]);
  expect(JSON.stringify(recorded.args[5])).toBe(`{"first":1}`);

  expect(invokeGraphQLJson(app, "linear", "   ")).rejects.toThrow(InvokeError);

  const errors = fakeApp(result(`{"data":null,"errors":[{"message":"boom"}]}`), { args: [] });
  expect(invokeGraphQLJson(errors, "linear", "query { x }")).rejects.toThrow(InvokeError);
});
