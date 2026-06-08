import type { OperationResult } from "./api.ts";
import { InvokeError } from "./invoke-error.ts";

export function operationResult(
  input: Omit<OperationResult, "json">,
): OperationResult {
  return {
    ...input,
    json<T = unknown>(): T {
      return parseOperationResultJson(input.body) as T;
    },
  };
}

export function decodeAppResult<T = unknown>(
  app: string,
  operation: string,
  result: OperationResult,
): T {
  return decodeAppBody(app, operation, result.status, result.body) as T;
}

export function decodeGraphQLResult<T = unknown>(
  app: string,
  result: OperationResult,
): T {
  const decoded = decodeAppResult<unknown>(app, "graphql", result);
  throwGraphQLErrors(app, result.body, parseOperationResultJson(result.body));
  throwGraphQLErrors(app, result.body, decoded);
  return decoded as T;
}

function throwGraphQLErrors(app: string, rawBody: string, value: unknown): void {
  if (!isRecord(value)) {
    return;
  }
  const errors = value.errors;
  if (Array.isArray(errors) && errors.length > 0) {
    throw new InvokeError({
      app,
      operation: "graphql",
      code: "graphql_errors",
      message: graphqlErrorMessage(errors),
      body: value,
      rawBody,
    });
  }
}

function decodeAppBody(
  app: string,
  operation: string,
  status: number,
  body: string,
): unknown {
  let parsed: unknown;
  try {
    parsed = parseOperationResultJson(body);
  } catch {
    if (status >= 400) {
      throw new InvokeError({
        app,
        operation,
        status,
        message: `app invoke failed with status ${status}`,
        rawBody: body,
      });
    }
    throw new InvokeError({
      app,
      operation,
      message: "app invoke response is not valid JSON",
      rawBody: body,
    });
  }

  if (status >= 400) {
    const error = new InvokeError({
      app,
      operation,
      status,
      message: `app invoke failed with status ${status}`,
      body: parsed,
      rawBody: body,
    });
    applyInvokeErrorFields(error, parsed);
    throw error;
  }

  if (isRecord(parsed) && typeof parsed.status === "string") {
    if (parsed.status === "error") {
      const error = new InvokeError({
        app,
        operation,
        message: "app invoke failed",
        body: parsed,
        rawBody: body,
      });
      applyInvokeErrorFields(error, parsed);
      throw error;
    }
    if (parsed.status === "success" && Object.hasOwn(parsed, "data")) {
      return parsed.data;
    }
  }

  return parsed;
}

function parseOperationResultJson(body: string): unknown {
  if (body.trim() === "") {
    return {};
  }
  try {
    return JSON.parse(body) as unknown;
  } catch {
    throw new InvokeError({
      message: "operation result body is not valid JSON",
      rawBody: body,
    });
  }
}

function applyInvokeErrorFields(error: InvokeError, parsed: unknown): void {
  if (!isRecord(parsed)) {
    return;
  }
  const nested = isRecord(parsed.error) ? parsed.error : undefined;
  const message = stringValue(nested?.message) ?? stringValue(parsed.message);
  const code = stringValue(nested?.code) ?? stringValue(parsed.code);
  if (message) {
    Object.defineProperty(error, "message", {
      configurable: true,
      value: message,
    });
  }
  if (code) {
    Object.defineProperty(error, "code", {
      configurable: true,
      value: code,
    });
  }
}

function graphqlErrorMessage(errors: readonly unknown[]): string {
  const first = errors[0];
  if (isRecord(first) && typeof first.message === "string" && first.message.trim()) {
    return first.message;
  }
  return "GraphQL returned errors";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}
