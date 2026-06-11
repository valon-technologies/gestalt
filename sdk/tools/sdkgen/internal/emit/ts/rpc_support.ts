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

/**
 * Atomic leaf types that sparse construction passes through unchanged.
 */
type InitAtom =
  | Date
  | Uint8Array
  | bigint
  | boolean
  | number
  | string
  | null
  | undefined;

/**
 * Sparse-construction input for a native request value: every field is
 * optional, unset fields read as their defaults, and nested message values
 * recurse. Atoms (`Date`, `Uint8Array`, `bigint`, and primitives) are never
 * recursed, arrays accept sparse elements, and map values stay required.
 * Variant unions distribute across their members with `case` kept required
 * and the payload made sparse, so a chosen variant stays well-formed.
 */
export type Init<T> = T extends InitAtom
  ? T
  : T extends ReadonlyArray<infer E>
    ? Init<E>[]
    : T extends { case: string | undefined }
      ? T extends { case: infer C; value: infer V }
        ? { case: C; value: Init<V> }
        : T
      : string extends keyof T
        ? { [K in keyof T]: Init<T[K]> }
        : number extends keyof T
          ? { [K in keyof T]: Init<T[K]> }
          : T extends object
            ? { [K in keyof T]?: Init<T[K]> }
            : T;

/**
 * Byte payload stream of a framed read. The transforms buffer the remaining
 * stream — mirroring the AWS SDK's `SdkStreamMixin` — and consume it, so each
 * stream supports one buffering call.
 */
export interface ByteStream extends AsyncIterable<Uint8Array> {
  /** Buffers the remaining payload chunks into one byte array. */
  transformToByteArray(): Promise<Uint8Array>;
  /** Buffers the remaining payload chunks and decodes them as text. */
  transformToString(encoding?: string): Promise<string>;
}
