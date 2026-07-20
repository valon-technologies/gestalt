import { describe, expect, test } from "bun:test";

import {
  defineApp,
  operation,
  s,
  stream,
  encoders,
  type StreamOutput,
} from "../src/index.ts";

describe("stream()", () => {
  test("typed item stream records media type and item schema", () => {
    const EventSchema = s.object({
      type: s.string(),
      timestamp: s.string(),
    });
    const out = stream({ item: EventSchema, encoder: encoders.ndjson });
    expect(out.kind).toBe("stream");
    expect(out.mediaType).toBe("application/x-ndjson");
    expect(out.itemSchema).toBe(EventSchema);
    expect(out.encoder).toBe(encoders.ndjson);
  });

  test("raw byte stream defaults to application/octet-stream", () => {
    const out = stream();
    expect(out.kind).toBe("stream");
    expect(out.mediaType).toBe("application/octet-stream");
    expect(out.encoder).toBeUndefined();
  });

  test("raw byte stream accepts a precise media type", () => {
    const out = stream({ mediaType: "image/png" });
    expect(out.mediaType).toBe("image/png");
  });
});

describe("encoders", () => {
  test("ndjson encodes each item as JSON + LF", async () => {
    const items = async function* () {
      yield { a: 1 };
      yield { b: 2 };
    };
    const chunks: string[] = [];
    for await (const chunk of encoders.ndjson.encode(items())) {
      chunks.push(new TextDecoder().decode(chunk));
    }
    expect(chunks).toEqual(['{"a":1}\n', '{"b":2}\n']);
  });

  test("json-seq uses record separator + LF", async () => {
    const items = async function* () {
      yield { x: 1 };
    };
    const chunks: string[] = [];
    for await (const chunk of encoders.jsonSequence.encode(items())) {
      chunks.push(new TextDecoder().decode(chunk));
    }
    expect(chunks).toEqual(["\x1E" + '{"x":1}\n']);
  });

  test("sse emits data fields with blank-line separators", async () => {
    const items = async function* () {
      yield { n: 1 };
    };
    const chunks: string[] = [];
    for await (const chunk of encoders.sse.encode(items())) {
      chunks.push(new TextDecoder().decode(chunk));
    }
    expect(chunks).toEqual(["data: {\"n\":1}\n\n"]);
  });
});

describe("streaming operation catalog", () => {
  test("staticCatalog emits response.stream for streaming operations", () => {
    const EventSchema = s.object({
      type: s.string(),
      timestamp: s.string(),
    });
    const app = defineApp({
      displayName: "Events",
      operations: [
        operation({
          id: "events.watch",
          method: "GET",
          readOnly: true,
          input: s.object({ since: s.string() }),
          output: stream({ item: EventSchema, encoder: encoders.ndjson }),
          async *handler() {
            yield { type: "started", timestamp: "2026-01-01T00:00:00Z" };
          },
        }),
        operation({
          id: "ping",
          output: s.object({ ok: s.boolean() }),
          async handler() {
            return { ok: true };
          },
        }),
      ],
    });
    const catalog = app.staticCatalog();
    const eventsOp = catalog.operations.find((op) => op.id === "events.watch")!;
    expect(eventsOp.response).toBeDefined();
    expect(eventsOp.response?.stream).toBeDefined();
    expect(eventsOp.response?.stream?.mediaType).toBe("application/x-ndjson");
    expect(eventsOp.response?.stream?.itemSchema).toBeDefined();

    const pingOp = catalog.operations.find((op) => op.id === "ping")!;
    expect(pingOp.response).toBeDefined();
    expect(pingOp.response?.unary).toBeDefined();
    expect(pingOp.response?.stream).toBeUndefined();
  });

  test("raw-byte streaming operation omits itemSchema", () => {
    const app = defineApp({
      displayName: "Images",
      operations: [
        operation({
          id: "image.watch",
          method: "GET",
          readOnly: true,
          output: stream({ mediaType: "image/png" }),
          async *handler() {
            yield new Uint8Array([0x89, 0x50, 0x4e, 0x47]);
          },
        }),
      ],
    });
    const catalog = app.staticCatalog();
    const op = catalog.operations[0]!;
    expect(op.response?.stream?.mediaType).toBe("image/png");
    expect(op.response?.stream?.itemSchema).toBeUndefined();
  });
});

describe("streaming item validation", () => {
  test("executeStream validates items against itemSchema", async () => {
    const EventSchema = s.object({
      type: s.string(),
      timestamp: s.string(),
    });
    const app = defineApp({
      displayName: "Events",
      operations: [
        operation({
          id: "events.watch",
          method: "GET",
          readOnly: true,
          output: stream({ item: EventSchema, encoder: encoders.ndjson }),
          async *handler() {
            yield { type: "started", timestamp: "2026-01-01T00:00:00Z" };
            // Invalid: missing timestamp
            yield { type: "bad" } as any;
          },
        }),
      ],
    });
    const reader = await app.executeStream("events.watch", {}, {} as any);
    // First frame: metadata (200) + first valid item's data.
    const first = await reader.recv();
    expect(first!.metadata?.status).toBe(200);
    expect(first!.data).toBeDefined();
    // Second item fails validation; the stream surfaces a single 500 metadata
    // frame with the error body (not a second 200-then-500 sequence).
    const errorFrame = await reader.recv();
    expect(errorFrame!.metadata?.status).toBe(500);
    expect(errorFrame!.data).toBeDefined();
    // Stream ends after the error.
    const done = await reader.recv();
    expect(done).toBeNull();
  });

  test("executeStream error on first item emits single 500 frame with no trailing success", async () => {
    const EventSchema = s.object({
      type: s.string(),
      timestamp: s.string(),
    });
    const app = defineApp({
      displayName: "Events",
      operations: [
        operation({
          id: "events.watch",
          method: "GET",
          readOnly: true,
          output: stream({ item: EventSchema, encoder: encoders.ndjson }),
          async *handler() {
            // Invalid from the start: missing timestamp
            yield { type: "bad" } as any;
          },
        }),
      ],
    });
    const reader = await app.executeStream("events.watch", {}, {} as any);
    // First item fails validation; the stream surfaces a single 500 metadata
    // frame with the error body. No 200 success frame should precede or follow.
    const errorFrame = await reader.recv();
    expect(errorFrame!.metadata?.status).toBe(500);
    expect(errorFrame!.data).toBeDefined();
    // Stream ends immediately after the error — no trailing 200 frame.
    const done = await reader.recv();
    expect(done).toBeNull();
    const stillDone = await reader.recv();
    expect(stillDone).toBeNull();
  });

  test("raw byte streams skip item validation", async () => {
    const app = defineApp({
      displayName: "Images",
      operations: [
        operation({
          id: "image.watch",
          method: "GET",
          readOnly: true,
          output: stream({ mediaType: "image/png" }),
          async *handler() {
            yield new Uint8Array([0x89, 0x50]);
            yield new Uint8Array([0x4e, 0x47]);
          },
        }),
      ],
    });
    const reader = await app.executeStream("image.watch", {}, {} as any);
    // The first frame carries metadata and the first data chunk together
    // (metadata is deferred until the first item is available).
    const first = await reader.recv();
    expect(first!.metadata?.mediaType).toBe("image/png");
    expect(Array.from(first!.data!)).toEqual([0x89, 0x50]);
    const second = await reader.recv();
    expect(second!.metadata).toBeUndefined();
    expect(Array.from(second!.data!)).toEqual([0x4e, 0x47]);
    const done = await reader.recv();
    expect(done).toBeNull();
  });
});
