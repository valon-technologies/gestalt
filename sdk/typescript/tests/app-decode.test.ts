import { readFileSync } from "node:fs";
import { join } from "node:path";

import { expect, test } from "bun:test";

import { operationResult } from "../src/app-decode.ts";
import {
  decodeAppResult,
  decodeGraphQLResult,
  InvokeError,
  isOk,
  requireOk,
} from "../src/invoke_support.ts";
import {
  GestaltError,
  GestaltErrorCode,
  httpStatusToGestaltCode,
  toGestaltError,
} from "../src/rpc_support.ts";

const fixtureRoot = join(import.meta.dir, "..", "..", "testdata", "app_invoke");

function fixture(name: string): string {
  return readFileSync(join(fixtureRoot, name), "utf8");
}

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

function expectJsonEqual(actual: unknown, expected: unknown): void {
  expect(JSON.stringify(actual)).toBe(JSON.stringify(expected));
}

test("app decode fixture behavior", () => {
  const result = (name: string, status = 200) => ({
    status,
    body: bytes(fixture(name)),
  });

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
  expect(() => decodeAppResult("github", "get_issue", result("http_302.json", 302))).toThrow(InvokeError);
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

test("decode errors carry the envelope fields", () => {
  const decode = (name: string, status = 200) =>
    decodeAppResult("github", "get_issue", { status, body: bytes(fixture(name)) });

  try {
    decode("error_envelope.json");
    throw new Error("expected InvokeError");
  } catch (error) {
    expect(error).toBeInstanceOf(InvokeError);
    expect(error).toBeInstanceOf(GestaltError);
    const invokeError = error as InvokeError;
    expect(invokeError.app).toBe("github");
    expect(invokeError.operation).toBe("get_issue");
    expect(invokeError.status).toBeUndefined();
    expect(invokeError.reason).toBe("missing_credential");
    expect(invokeError.code).toBe(GestaltErrorCode.Unknown);
    expect(invokeError.message).toBe("missing credential");
    expect(invokeError.rawText()).toBe(fixture("error_envelope.json"));
    expect(toGestaltError(invokeError)).toBe(invokeError);
  }

  try {
    decode("http_401.json", 401);
    throw new Error("expected InvokeError");
  } catch (error) {
    expect(error).toBeInstanceOf(InvokeError);
    const invokeError = error as InvokeError;
    expect(invokeError.status).toBe(401);
    expect(invokeError.reason).toBe("unauthorized");
    expect(invokeError.code).toBe(GestaltErrorCode.Unauthenticated);
    expect(invokeError.message).toBe("unauthorized");
  }

  try {
    decode("http_302.json", 302);
    throw new Error("expected InvokeError");
  } catch (error) {
    expect(error).toBeInstanceOf(InvokeError);
    const invokeError = error as InvokeError;
    expect(invokeError.status).toBe(302);
    expect(invokeError.code).toBe(GestaltErrorCode.Unknown);
  }

  try {
    decode("invalid_json.txt");
    throw new Error("expected InvokeError");
  } catch (error) {
    expect(error).toBeInstanceOf(InvokeError);
    const invokeError = error as InvokeError;
    expect(invokeError.message).toBe("app invoke response is not valid JSON");
    expect(invokeError.code).toBe(GestaltErrorCode.Internal);
  }
});

test("httpStatusToGestaltCode maps common HTTP statuses", () => {
  expect(httpStatusToGestaltCode(401)).toBe(GestaltErrorCode.Unauthenticated);
  expect(httpStatusToGestaltCode(418)).toBe(GestaltErrorCode.Unknown);
});

test("isOk reports 2xx statuses only", () => {
  expect(isOk(199)).toBe(false);
  expect(isOk(200)).toBe(true);
  expect(isOk(299)).toBe(true);
  expect(isOk(300)).toBe(false);
});

test("requireOk raises the canonical status error", () => {
  const ok = { status: 200, body: bytes(fixture("success_envelope.json")) };
  expect(requireOk("github", "get_issue", ok)).toBeUndefined();

  try {
    requireOk("github", "get_issue", {
      status: 401,
      body: bytes(fixture("http_401.json")),
    });
    throw new Error("expected InvokeError");
  } catch (error) {
    expect(error).toBeInstanceOf(InvokeError);
    const invokeError = error as InvokeError;
    expect(invokeError.app).toBe("github");
    expect(invokeError.operation).toBe("get_issue");
    expect(invokeError.status).toBe(401);
    expect(invokeError.reason).toBe("unauthorized");
    expect(invokeError.code).toBe(GestaltErrorCode.Unauthenticated);
    expect(invokeError.message).toBe("unauthorized");
    expect(invokeError.rawText()).toBe(fixture("http_401.json"));
  }
});

test("decodeAppResult accepts generated OperationResult values", () => {
  const result = {
    status: 200,
    body: bytes(fixture("success_envelope.json")),
    headers: {},
  };
  expectJsonEqual(decodeAppResult<{ id: number }>("github", "get_issue", result), { id: 1 });
});

test("OperationResult json helper does not unwrap envelopes", () => {
  const raw = operationResult({ status: 200, headers: {}, body: bytes(fixture("success_envelope.json")) });
  expectJsonEqual(raw.json(), { status: "success", data: { id: 1 } });
  expect(raw.ok).toBe(true);
  expect(raw.text()).toBe(fixture("success_envelope.json"));
  expect(raw.bytes()).toEqual(bytes(fixture("success_envelope.json")));
  expect(raw.requireOk()).toBe(raw);
});

test("OperationResult status helper raises InvokeError", () => {
  const raw = operationResult({ status: 503, headers: {}, body: bytes("not json") });
  expect(raw.ok).toBe(false);
  expect(() => raw.requireOk()).toThrow(InvokeError);
});

test("InvokeError is exported and catchable", () => {
  const error = new InvokeError({
    app: "github",
    operation: "get_issue",
    status: 401,
    message: "unauthorized",
    rawBody: bytes(fixture("http_401.json")),
  });
  expect(error).toBeInstanceOf(Error);
  expect(error).toBeInstanceOf(GestaltError);
  expect(error).toBeInstanceOf(InvokeError);
  expect(error.status).toBe(401);
  expect(error.code).toBe(GestaltErrorCode.Unauthenticated);
  expect(error.rawText()).toBe(fixture("http_401.json"));
});
