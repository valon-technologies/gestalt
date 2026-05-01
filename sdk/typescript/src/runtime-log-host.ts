import { connect } from "node:net";
import { Writable } from "node:stream";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AppendPluginRuntimeLogsRequestSchema,
  type AppendPluginRuntimeLogsResponse,
  PluginRuntimeLogHost as PluginRuntimeLogHostService,
  PluginRuntimeLogStream,
} from "../gen/v1/pluginruntime_pb.ts";

export const ENV_RUNTIME_LOG_HOST_SOCKET = "GESTALT_RUNTIME_LOG_SOCKET";
export const ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN = `${ENV_RUNTIME_LOG_HOST_SOCKET}_TOKEN`;
export const ENV_RUNTIME_SESSION_ID = "GESTALT_RUNTIME_SESSION_ID";

const RUNTIME_LOG_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export type RuntimeLogStreamName = "stdout" | "stderr" | "runtime";
export type RuntimeLogStreamInput =
  | RuntimeLogStreamName
  | PluginRuntimeLogStream;
export type RuntimeLogAppendLogsInput = MessageInitShape<
  typeof AppendPluginRuntimeLogsRequestSchema
>;
export type RuntimeLogAppendResponseMessage = AppendPluginRuntimeLogsResponse;

export interface RuntimeLogAppendInput {
  sessionId?: string;
  message: string | Uint8Array;
  stream?: RuntimeLogStreamInput;
  observedAt?: Date;
  sourceSeq?: number | bigint;
}

export interface RuntimeLogWriterOptions {
  sessionId?: string;
  stream?: RuntimeLogStreamInput;
  sourceSeqStart?: number | bigint;
}

export class RuntimeLogHost {
  private readonly client: Client<typeof PluginRuntimeLogHostService>;
  private sourceSeq = 0n;

  constructor() {
    const target = process.env[ENV_RUNTIME_LOG_HOST_SOCKET];
    if (!target) {
      throw new Error(
        `runtime log host: ${ENV_RUNTIME_LOG_HOST_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN]?.trim() ?? "";
    const transportOptions = runtimeLogTransportOptions(target);
    const transport = createGrpcTransport({
      ...transportOptions,
      ...(transportOptions.nodeOptions
        ? {
            nodeOptions: {
              createConnection: () =>
                connect(transportOptions.nodeOptions!.path),
            },
          }
        : {}),
      interceptors: relayToken
        ? [runtimeLogRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(PluginRuntimeLogHostService, transport);
  }

  async appendLogs(
    request: RuntimeLogAppendLogsInput,
  ): Promise<RuntimeLogAppendResponseMessage> {
    return await this.client.appendLogs(request);
  }

  async append(
    input: RuntimeLogAppendInput,
  ): Promise<RuntimeLogAppendResponseMessage> {
    const sourceSeq =
      input.sourceSeq === undefined
        ? (this.sourceSeq += 1n)
        : BigInt(input.sourceSeq);
    if (sourceSeq > this.sourceSeq) {
      this.sourceSeq = sourceSeq;
    }
    return await this.appendLogs({
      sessionId: runtimeSessionId(input.sessionId),
      logs: [
        {
          stream: runtimeLogStream(input.stream ?? "runtime"),
          message: runtimeLogMessage(input.message),
          observedAt: toProtoTimestamp(input.observedAt ?? new Date()),
          sourceSeq,
        },
      ],
    });
  }

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
}

function runtimeSessionId(sessionId?: string): string {
  const value = (sessionId ?? process.env[ENV_RUNTIME_SESSION_ID] ?? "").trim();
  if (!value) {
    throw new Error(`runtime session: ${ENV_RUNTIME_SESSION_ID} is not set`);
  }
  return value;
}

function runtimeLogTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("runtime log host: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `runtime log host: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `runtime log host: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `runtime log host: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `runtime log host: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function runtimeLogRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(RUNTIME_LOG_RELAY_TOKEN_HEADER, token);
    return await next(req);
  };
}

function runtimeLogStream(stream: RuntimeLogStreamInput): PluginRuntimeLogStream {
  if (typeof stream === "number") {
    return stream;
  }
  switch (stream.trim().toLowerCase()) {
    case "stdout":
      return PluginRuntimeLogStream.STDOUT;
    case "stderr":
      return PluginRuntimeLogStream.STDERR;
    case "runtime":
      return PluginRuntimeLogStream.RUNTIME;
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

function toProtoTimestamp(value: Date): { seconds: bigint; nanos: number } {
  const millis = value.getTime();
  const seconds = Math.floor(millis / 1000);
  const nanos = Math.trunc((millis - (seconds * 1000)) * 1_000_000);
  return {
    seconds: BigInt(seconds),
    nanos,
  };
}

function toError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}
