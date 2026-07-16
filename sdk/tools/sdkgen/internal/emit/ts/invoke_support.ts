/**
 * The canonical app invocation payload error: an HTTP-error status, an error
 * envelope, or an undecodable result body. Transport failures stay
 * GestaltError; both surface from decoded invoke methods.
 */
export class InvokeError extends Error {
  readonly app: string;
  readonly operation: string;
  readonly status?: number | undefined;
  readonly code?: string | undefined;
  readonly body: unknown;
  readonly rawBody: Uint8Array;

  constructor(input: {
    app?: string | undefined;
    operation?: string | undefined;
    status?: number | undefined;
    code?: string | undefined;
    message?: string | undefined;
    body?: unknown;
    rawBody?: Uint8Array | string | undefined;
    cause?: unknown;
  }) {
    super(input.message ?? defaultInvokeErrorMessage(input.status), {
      cause: input.cause,
    });
    this.name = "InvokeError";
    this.app = input.app ?? "";
    this.operation = input.operation ?? "";
    this.status = input.status;
    this.code = input.code;
    this.body = input.body;
    this.rawBody =
      typeof input.rawBody === "string"
        ? new TextEncoder().encode(input.rawBody)
        : new Uint8Array(input.rawBody ?? new Uint8Array());
  }

  rawText(): string {
    return new TextDecoder("utf-8", { fatal: false }).decode(this.rawBody);
  }
}

/**
 * Decodes one app operation result with the standard JSON envelope semantics:
 * success envelopes return their data, error envelopes and HTTP-error
 * statuses throw InvokeError, and any other JSON body passes through
 * unchanged.
 */
export function decodeAppResult<T = unknown>(
  app: string,
  operation: string,
  result: { status: number; body: Uint8Array },
): T {
  return decodeAppBody(app, operation, result.status, result.body) as T;
}

/**
 * Decodes one GraphQL invocation result like decodeAppResult and additionally
 * throws InvokeError when the response carries a non-empty GraphQL errors
 * array.
 */
export function decodeGraphQLResult<T = unknown>(
  app: string,
  result: { status: number; body: Uint8Array },
): T {
  const decoded = decodeAppResult<unknown>(app, "graphql", result);
  throwGraphQLErrors(app, result.body, parseOperationResultJson(result.body));
  throwGraphQLErrors(app, result.body, decoded);
  return decoded as T;
}

/**
 * Reports whether an HTTP status is a 2xx success.
 */
export function isOk(status: number): boolean {
  return status >= 200 && status <= 299;
}

/**
 * Throws InvokeError when the result's HTTP status is not a 2xx success,
 * extracting the error envelope's message and code when the body carries
 * them. Successful results pass through untouched.
 */
export function requireOk(
  app: string,
  operation: string,
  result: { status: number; body: Uint8Array },
): void {
  if (!isOk(result.status)) {
    throw statusInvokeError(app, operation, result.status, result.body);
  }
}

function defaultInvokeErrorMessage(status?: number | undefined): string {
  return status === undefined
    ? "app invoke failed"
    : `app invoke failed with status ${status}`;
}

function throwGraphQLErrors(
  app: string,
  rawBody: Uint8Array,
  value: unknown,
): void {
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

// statusInvokeError builds the canonical InvokeError for an HTTP-error
// status, extracting the error envelope's message and code when the body
// parses as JSON.
function statusInvokeError(
  app: string,
  operation: string,
  status: number,
  body: Uint8Array,
): InvokeError {
  let parsed: unknown;
  try {
    parsed = parseOperationResultJson(body);
  } catch {
    return new InvokeError({
      app,
      operation,
      status,
      message: `app invoke failed with status ${status}`,
      rawBody: body,
    });
  }
  const error = new InvokeError({
    app,
    operation,
    status,
    message: `app invoke failed with status ${status}`,
    body: parsed,
    rawBody: body,
  });
  applyInvokeErrorFields(error, parsed);
  return error;
}

function decodeAppBody(
  app: string,
  operation: string,
  status: number,
  body: Uint8Array,
): unknown {
  if (!isOk(status)) {
    throw statusInvokeError(app, operation, status, body);
  }
  let parsed: unknown;
  try {
    parsed = parseOperationResultJson(body);
  } catch {
    throw new InvokeError({
      app,
      operation,
      message: "app invoke response is not valid JSON",
      rawBody: body,
    });
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
  if (
    isRecord(first) &&
    typeof first.message === "string" &&
    first.message.trim()
  ) {
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
