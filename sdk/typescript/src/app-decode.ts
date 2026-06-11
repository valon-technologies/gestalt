import type { OperationResult } from "./api.ts";
import { InvokeError } from "./invoke-error.ts";

/**
 * The slice of an app invocation result the decode helpers read. Both the
 * provider-side {@link OperationResult} and the generated App client's result
 * satisfy it.
 */
export interface DecodableOperationResult {
  status: number;
  body: Uint8Array;
}

export function operationResult(
  input: Omit<OperationResult, "ok" | "bytes" | "text" | "json" | "requireOk">,
): OperationResult {
  const body = new Uint8Array(input.body);
  return {
    ...input,
    body,
    ok: input.status >= 200 && input.status < 300,
    bytes(): Uint8Array {
      return new Uint8Array(body);
    },
    text(): string {
      return decodeBodyText(body);
    },
    json<T = unknown>(): T {
      return parseOperationResultJson(body) as T;
    },
    requireOk(): OperationResult {
      if (input.status >= 200 && input.status < 300) {
        return this;
      }
      throw new InvokeError({
        status: input.status,
        body: parseOperationResultJsonIfPossible(body),
        rawBody: body,
        message: `app invoke failed with status ${input.status}`,
      });
    },
  };
}

/**
 * Decodes one app operation result with the standard envelope semantics:
 * `{status:"success",data}` envelopes return `data`, `{status:"error"}`
 * envelopes and HTTP-error statuses throw {@link InvokeError}, and any other
 * JSON body is returned as-is.
 */
export function decodeAppResult<T = unknown>(
  app: string,
  operation: string,
  result: DecodableOperationResult,
): T {
  return decodeAppBody(app, operation, result.status, result.body) as T;
}

/**
 * Decodes one GraphQL invocation result like {@link decodeAppResult} and also
 * throws {@link InvokeError} when the response carries a GraphQL `errors`
 * array.
 */
export function decodeGraphQLResult<T = unknown>(
  app: string,
  result: DecodableOperationResult,
): T {
  const decoded = decodeAppResult<unknown>(app, "graphql", result);
  throwGraphQLErrors(app, result.body, parseOperationResultJson(result.body));
  throwGraphQLErrors(app, result.body, decoded);
  return decoded as T;
}

function throwGraphQLErrors(app: string, rawBody: Uint8Array, value: unknown): void {
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
  body: Uint8Array,
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

function parseOperationResultJson(body: Uint8Array): unknown {
  const text = decodeBodyText(body);
  if (text.trim() === "") {
    return {};
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new InvokeError({
      message: "operation result body is not valid JSON",
      rawBody: body,
    });
  }
}

function parseOperationResultJsonIfPossible(body: Uint8Array): unknown {
  try {
    return parseOperationResultJson(body);
  } catch {
    return undefined;
  }
}

function decodeBodyText(body: Uint8Array): string {
  return new TextDecoder("utf-8", { fatal: false }).decode(body);
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
