export class InvokeError extends Error {
  readonly app: string;
  readonly operation: string;
  readonly status?: number | undefined;
  readonly code?: string | undefined;
  readonly body: unknown;
  readonly rawBody: string;

  constructor(input: {
    app?: string | undefined;
    operation?: string | undefined;
    status?: number | undefined;
    code?: string | undefined;
    message?: string | undefined;
    body?: unknown;
    rawBody?: string | undefined;
  }) {
    super(input.message ?? defaultInvokeErrorMessage(input.status));
    this.name = "InvokeError";
    this.app = input.app ?? "";
    this.operation = input.operation ?? "";
    this.status = input.status;
    this.code = input.code;
    this.body = input.body;
    this.rawBody = input.rawBody ?? "";
  }
}

function defaultInvokeErrorMessage(status?: number | undefined): string {
  return status === undefined
    ? "app invoke failed"
    : `app invoke failed with status ${status}`;
}
