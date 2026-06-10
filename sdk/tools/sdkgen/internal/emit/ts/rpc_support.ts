import { ConnectError } from "@connectrpc/connect";

/**
 * Milliseconds, the SDK's native representation of a duration.
 */
export type DurationMs = number;

/**
 * The native representation of an empty oneof variant: an object with no
 * fields.
 */
export type Unit = Record<string, never>;

/**
 * A status carried in response payloads, mirroring the canonical error
 * model.
 */
export interface RpcStatus {
  code: GestaltErrorCode;
  message: string;
}

/**
 * Canonical SDK error codes, drawn from the standard gRPC status codes.
 */
export const GestaltErrorCode = {
  Canceled: 1,
  Unknown: 2,
  InvalidArgument: 3,
  DeadlineExceeded: 4,
  NotFound: 5,
  AlreadyExists: 6,
  PermissionDenied: 7,
  ResourceExhausted: 8,
  FailedPrecondition: 9,
  Aborted: 10,
  OutOfRange: 11,
  Unimplemented: 12,
  Internal: 13,
  Unavailable: 14,
  DataLoss: 15,
  Unauthenticated: 16,
} as const;

export type GestaltErrorCode = number;

/**
 * Canonical SDK error: one code, a message, and the underlying cause.
 * Transport error types never appear in the public SDK surface.
 */
export class GestaltError extends Error {
  readonly code: GestaltErrorCode;

  constructor(code: GestaltErrorCode, message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "GestaltError";
    this.code = code;
  }
}

export function toGestaltError(error: unknown): GestaltError {
  if (error instanceof GestaltError) {
    return error;
  }
  if (error instanceof ConnectError) {
    return new GestaltError(error.code as number, error.rawMessage, { cause: error });
  }
  return new GestaltError(GestaltErrorCode.Unknown, String(error), { cause: error });
}
