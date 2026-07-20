/**
 * Streaming operation response primitives for app providers.
 *
 * An operation declares a streaming response with {@link stream}. The catalog
 * records the encoder's media type and item schema; the encoder itself
 * transforms typed items into byte chunks at execution time.
 *
 * @module providers/stream
 */

import type { Schema } from "../schema.ts";

/**
 * `StreamEncoder` supplies the representation media type and incrementally
 * transforms typed items into byte chunks. It does not control REST or gRPC.
 */
export interface StreamEncoder<T> {
  mediaType: string;
  encode(items: AsyncIterable<T>): AsyncIterable<Uint8Array>;
}

/**
 * `StreamOutput` is the streaming response declaration attached to an
 * operation via `output: stream(...)`. The `kind` discriminator lets the
 * runtime distinguish `Schema<Out>` from `StreamOutput<Out>`.
 */
export interface StreamOutput<T> {
  readonly kind: "stream";
  readonly mediaType: string;
  readonly itemSchema?: Schema<T>;
  readonly encoder?: StreamEncoder<T>;
}

function utf8(value: string): Uint8Array {
  return new TextEncoder().encode(value);
}

/**
 * Declares a typed item stream. The catalog records the encoder's media type
 * and the item schema; the SDK validates yielded items, passes them through
 * the encoder, and exposes the resulting byte stream to the transport.
 */
export function stream<T>(
  options: { item: Schema<T>; encoder: StreamEncoder<T> },
): StreamOutput<T>;

/**
 * Declares a raw byte stream whose format the application owns. Defaults to
 * `application/octet-stream` when no media type is supplied; a more precise
 * media type should be used when known.
 */
export function stream(options?: { mediaType?: string }): StreamOutput<Uint8Array>;

export function stream<T>(
  options?:
    | { item: Schema<T>; encoder: StreamEncoder<T> }
    | { mediaType?: string },
): StreamOutput<T> {
  if (options && "item" in options) {
    return {
      kind: "stream",
      mediaType: options.encoder.mediaType,
      itemSchema: options.item,
      encoder: options.encoder,
    } as unknown as StreamOutput<T>;
  }
  const mediaType = (options as { mediaType?: string } | undefined)?.mediaType ??
    "application/octet-stream";
  return {
    kind: "stream",
    mediaType,
  } as unknown as StreamOutput<T>;
}

/**
 * Built-in stream encoders for common incremental representations.
 */
export const encoders = {
  /**
   * `application/x-ndjson`: JSON-serializes each item and appends a newline.
   */
  ndjson: {
    mediaType: "application/x-ndjson",
    async *encode(items: AsyncIterable<unknown>): AsyncIterable<Uint8Array> {
      for await (const item of items) {
        yield utf8(JSON.stringify(item) + "\n");
      }
    },
  } satisfies StreamEncoder<unknown>,

  /**
   * `application/json-seq`: RFC 7464 record separator + JSON + line feed.
   */
  jsonSequence: {
    mediaType: "application/json-seq",
    async *encode(items: AsyncIterable<unknown>): AsyncIterable<Uint8Array> {
      for await (const item of items) {
        yield utf8("\x1E" + JSON.stringify(item) + "\n");
      }
    },
  } satisfies StreamEncoder<unknown>,

  /**
   * `text/event-stream`: Server-Sent Events fields with a blank-line
   * separator between events. Each item is emitted as a `data:` field.
   */
  sse: {
    mediaType: "text/event-stream",
    async *encode(items: AsyncIterable<unknown>): AsyncIterable<Uint8Array> {
      for await (const item of items) {
        const payload = JSON.stringify(item);
        const frame = `data: ${payload}\n\n`;
        yield utf8(frame);
      }
    },
  } satisfies StreamEncoder<unknown>,
} as const;

/**
 * Type guard for `StreamOutput`.
 */
export function isStreamOutput(value: unknown): value is StreamOutput<unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    (value as { kind?: unknown }).kind === "stream"
  );
}
