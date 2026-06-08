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
    super(input.message ?? defaultInvokeErrorMessage(input.status), { cause: input.cause });
    this.name = "InvokeError";
    this.app = input.app ?? "";
    this.operation = input.operation ?? "";
    this.status = input.status;
    this.code = input.code;
    this.body = input.body;
    this.rawBody = typeof input.rawBody === "string"
      ? new TextEncoder().encode(input.rawBody)
      : new Uint8Array(input.rawBody ?? new Uint8Array());
  }

  rawText(): string {
    return new TextDecoder("utf-8", { fatal: false }).decode(this.rawBody);
  }
}

function defaultInvokeErrorMessage(status?: number | undefined): string {
  return status === undefined
    ? "app invoke failed"
    : `app invoke failed with status ${status}`;
}
