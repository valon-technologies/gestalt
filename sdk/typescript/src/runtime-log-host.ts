import { Writable } from "node:stream";

import {
  createClient,
  type Client,
} from "@connectrpc/connect";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";

import {
  RuntimeLogHost as RuntimeLogHostService,
  RuntimeLogStream as ProtoRuntimeLogStream,
} from "./internal/gen/v1/runtime_provider_pb.ts";
import { timestampFromDate } from "./protocol.ts";

/** Environment variable containing the current runtime session id. */
export const ENV_RUNTIME_SESSION_ID = "GESTALT_RUNTIME_SESSION_ID";

/** Named runtime log streams accepted by the authored SDK. */
export type RuntimeLogStreamName = "stdout" | "stderr" | "runtime";
/** Runtime log stream input accepted by authored APIs. */
export type RuntimeLogStreamInput = RuntimeLogStreamName;

/** One runtime log entry to append through `RuntimeLogHost.append`. */
export interface RuntimeLogAppendInput {
  /** Runtime session id. Defaults to `GESTALT_RUNTIME_SESSION_ID`. */
  sessionId?: string;
  /** Log message bytes or text. */
  message: string | Uint8Array;
  /** Destination stream. Defaults to `runtime`. */
  stream?: RuntimeLogStreamInput;
  /** Observation timestamp. Defaults to the current time. */
  observedAt?: Date;
  /** Monotonic source sequence number. Auto-increments when omitted. */
  sourceSeq?: number | bigint;
}

/** Batch of runtime log entries to append through `RuntimeLogHost.appendLogs`. */
export interface RuntimeLogAppendLogsInput {
  /** Runtime session id. Defaults to `GESTALT_RUNTIME_SESSION_ID`. */
  sessionId?: string | undefined;
  /** Log entries to append. */
  logs: readonly RuntimeLogAppendInput[];
}

/** Response returned after appending runtime logs. */
export interface RuntimeLogAppendResponse {
  /** Last source sequence accepted by the host. */
  lastSeq: bigint;
}

/** Options for the `Writable` returned by `RuntimeLogHost.writer`. */
export interface RuntimeLogWriterOptions {
  /** Runtime session id. Defaults to `GESTALT_RUNTIME_SESSION_ID`. */
  sessionId?: string;
  /** Destination stream. Defaults to `stdout`. */
  stream?: RuntimeLogStreamInput;
  /** Initial sequence number for writes. */
  sourceSeqStart?: number | bigint;
}

/**
 * Client for appending runtime logs to the host.
 *
 * Use `append` for a single entry, `appendLogs` for a native batch, or `writer`
 * to bridge Node streams into the runtime log host.
 */
export class RuntimeLogHost {
  private readonly client: Client<typeof RuntimeLogHostService>;
  private sourceSeq = 0n;

  constructor() {
    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(
        `runtime log host: ${ENV_HOST_SERVICE_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("runtime log host", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(RuntimeLogHostService, transport);
  }

  async appendLogs(
    request: RuntimeLogAppendLogsInput,
  ): Promise<RuntimeLogAppendResponse> {
    const sessionId = runtimeLogBatchSessionId(request);
    return runtimeLogAppendResponseFromProto(
      await this.client.appendLogs({
        sessionId,
        logs: request.logs.map((log) => this.runtimeLogEntryToProto(log)),
      }),
    );
  }

  /** Appends one runtime log entry. */
  async append(
    input: RuntimeLogAppendInput,
  ): Promise<RuntimeLogAppendResponse> {
    return await this.appendLogs({
      sessionId: input.sessionId,
      logs: [input],
    });
  }

  /** Returns a `Writable` that appends chunks to a runtime log stream. */
  writer(options?: RuntimeLogWriterOptions): Writable;
  writer(sessionId: string, options?: RuntimeLogWriterOptions): Writable;
  writer(
    sessionIdOrOptions: string | RuntimeLogWriterOptions = {},
    options: RuntimeLogWriterOptions = {},
  ): Writable {
    const writerOptions =
      typeof sessionIdOrOptions === "string"
        ? options
        : sessionIdOrOptions;
    const sessionId = runtimeSessionId(
      typeof sessionIdOrOptions === "string"
        ? sessionIdOrOptions
        : writerOptions.sessionId,
    );
    const stream = writerOptions.stream ?? "stdout";
    let sourceSeq = BigInt(writerOptions.sourceSeqStart ?? 0);

    return new Writable({
      write: (chunk: Buffer | string, encoding, callback) => {
        const actualEncoding = (
          String(encoding) === "buffer" ? "utf8" : encoding
        ) as BufferEncoding;
        const message =
          typeof chunk === "string"
            ? chunk
            : Buffer.from(chunk).toString(actualEncoding);
        sourceSeq += 1n;
        this.append({
          sessionId,
          stream,
          message,
          sourceSeq,
        }).then(
          () => callback(),
          (error: unknown) => callback(toError(error)),
        );
      },
    });
  }

  private runtimeLogEntryToProto(input: RuntimeLogAppendInput) {
    const sourceSeq =
      input.sourceSeq === undefined
        ? (this.sourceSeq += 1n)
        : BigInt(input.sourceSeq);
    if (sourceSeq > this.sourceSeq) {
      this.sourceSeq = sourceSeq;
    }
    return {
      stream: runtimeLogStream(input.stream ?? "runtime"),
      message: runtimeLogMessage(input.message),
      observedAt: timestampFromDate(input.observedAt ?? new Date()),
      sourceSeq,
    };
  }
}

function runtimeSessionId(sessionId?: string): string {
  const value = (sessionId ?? process.env[ENV_RUNTIME_SESSION_ID] ?? "").trim();
  if (!value) {
    throw new Error(`runtime session: ${ENV_RUNTIME_SESSION_ID} is not set`);
  }
  return value;
}

function runtimeLogBatchSessionId(request: RuntimeLogAppendLogsInput): string {
  const explicitSessionIds = new Set<string>();
  addRuntimeLogSessionId(explicitSessionIds, request.sessionId);
  for (const log of request.logs) {
    addRuntimeLogSessionId(explicitSessionIds, log.sessionId);
  }
  if (explicitSessionIds.size > 1) {
    throw new Error("runtime log host: appendLogs entries must use one session id");
  }
  return runtimeSessionId(
    request.sessionId ?? request.logs.find((log) => log.sessionId)?.sessionId,
  );
}

function addRuntimeLogSessionId(values: Set<string>, sessionId?: string): void {
  const value = sessionId?.trim();
  if (value) {
    values.add(value);
  }
}

function runtimeLogAppendResponseFromProto(
  response: { lastSeq: bigint },
): RuntimeLogAppendResponse {
  return { lastSeq: response.lastSeq };
}

function runtimeLogStream(
  stream: RuntimeLogStreamInput,
): ProtoRuntimeLogStream {
  switch (stream.trim().toLowerCase()) {
    case "stdout":
      return ProtoRuntimeLogStream.STDOUT;
    case "stderr":
      return ProtoRuntimeLogStream.STDERR;
    case "runtime":
      return ProtoRuntimeLogStream.RUNTIME;
    default:
      throw new Error(`unsupported runtime log stream ${JSON.stringify(stream)}`);
  }
}

function runtimeLogMessage(message: string | Uint8Array): string {
  if (typeof message === "string") {
    return message;
  }
  return Buffer.from(message).toString("utf8");
}

function toError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}
