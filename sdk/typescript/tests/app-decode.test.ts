import { readFileSync } from "node:fs";
import { join } from "node:path";

import { expect, test } from "bun:test";

import {
  decodeAppResult,
  decodeGraphQLResult,
  operationResult,
} from "../src/app-decode.ts";
import { InvokeError } from "../src/invoke-error.ts";

const fixtureRoot = join(import.meta.dir, "..", "..", "testdata", "app_invoke");

function fixture(name: string): string {
  return readFileSync(join(fixtureRoot, name), "utf8");
}

function expectJsonEqual(actual: unknown, expected: unknown): void {
  expect(JSON.stringify(actual)).toBe(JSON.stringify(expected));
}

test("app decode fixture behavior", () => {
  const result = (name: string, status = 200) =>
    operationResult({ status, headers: {}, body: fixture(name) });

  expectJsonEqual(decodeAppResult("github", "get_issue", result("success_envelope.json")), { id: 1 });
  expectJsonEqual(decodeAppResult("github", "get_issue", result("plain_ok.json")), {
    pull_request: { id: 123, title: "Fix transport" },
  });
  expectJsonEqual(decodeAppResult("github", "get_issue", result("empty_body.json")), {});
  expectJsonEqual(decodeAppResult("github", "get_issue", result("success_missing_data.json")), {
    status: "success",
    ok: true,
  });
  expect(decodeAppResult("github", "get_issue", result("success_null_data.json"))).toBeNull();
  expectJsonEqual(decodeAppResult("github", "get_issue", result("unknown_status.json")), {
    status: "pending",
    data: { id: 2 },
  });
  expectJsonEqual(decodeAppResult("github", "get_issue", result("non_string_status.json")), {
    status: true,
    data: { id: 3 },
  });
  expectJsonEqual(decodeAppResult("github", "get_issue", result("array_ok.json")), [1, 2, 3]);
  expect(String(decodeAppResult("github", "get_issue", result("primitive_ok.json")))).toBe("ok");
  expect(() => decodeAppResult("github", "get_issue", result("error_envelope.json"))).toThrow(InvokeError);
  expect(() => decodeAppResult("github", "get_issue", result("http_401.json", 401))).toThrow(InvokeError);
  expect(() => decodeAppResult("github", "get_issue", result("invalid_json.txt"))).toThrow(InvokeError);
  expectJsonEqual(decodeGraphQLResult("linear", result("graphql_ok.json")), {
    data: { viewer: { id: "user-1" } },
    errors: [],
  });
  expectJsonEqual(decodeGraphQLResult("linear", result("graphql_malformed_errors.json")), {
    data: { viewer: null },
    errors: { message: "not an array" },
  });
  expect(() => decodeGraphQLResult("linear", result("graphql_errors.json"))).toThrow(InvokeError);
  expect(() => decodeGraphQLResult("linear", result("graphql_success_envelope_errors.json"))).toThrow(
    InvokeError,
  );
});

test("OperationResult json helper does not unwrap envelopes", () => {
  const raw = operationResult({ status: 200, headers: {}, body: fixture("success_envelope.json") });
  expectJsonEqual(raw.json(), { status: "success", data: { id: 1 } });
});

test("InvokeError is exported and catchable", () => {
  const error = new InvokeError({
    app: "github",
    operation: "get_issue",
    status: 401,
    message: "unauthorized",
    rawBody: fixture("http_401.json"),
  });
  expect(error).toBeInstanceOf(Error);
  expect(error).toBeInstanceOf(InvokeError);
  expect(error.status).toBe(401);
});
