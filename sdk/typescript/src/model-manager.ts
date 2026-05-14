import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  ModelManagerHost as ModelManagerHostService,
} from "./internal/gen/v1/model_pb.ts";
import type { Request } from "./api.ts";
import {
  type GenerateModelResponse,
  type ModelMessage,
  generateModelResponseFromProto,
  modelMessageToProto,
} from "./model.ts";
import {
  type JsonObjectInput,
} from "./protocol.ts";
import { optionalStruct } from "./protocol-internal.ts";

export const ENV_MODEL_MANAGER_SOCKET = "GESTALT_MODEL_MANAGER_SOCKET";
export const ENV_MODEL_MANAGER_SOCKET_TOKEN =
  `${ENV_MODEL_MANAGER_SOCKET}_TOKEN`;
const MODEL_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export interface ModelManagerGenerate {
  providerName?: string | undefined;
  model?: string | undefined;
  messages: readonly ModelMessage[];
  responseSchema?: JsonObjectInput | undefined;
  modelOptions?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
}

export class ModelManager {
  private readonly client: Client<typeof ModelManagerHostService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const target = process.env[ENV_MODEL_MANAGER_SOCKET];
    if (!target) {
      throw new Error(`model manager: ${ENV_MODEL_MANAGER_SOCKET} is not set`);
    }
    const relayToken =
      process.env[ENV_MODEL_MANAGER_SOCKET_TOKEN]?.trim() ?? "";

    const transport = createGrpcTransport({
      ...modelManagerTransportOptions(target),
      interceptors: relayToken
        ? [modelManagerRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(ModelManagerHostService, transport);
  }

  async generate(request: ModelManagerGenerate): Promise<GenerateModelResponse> {
    return generateModelResponseFromProto(
      await this.client.generate({
        providerName: request.providerName ?? "",
        model: request.model ?? "",
        messages: request.messages.map(modelMessageToProto),
        responseSchema: optionalStruct(request.responseSchema),
        modelOptions: optionalStruct(request.modelOptions),
        metadata: optionalStruct(request.metadata),
        invocationToken: this.invocationToken,
      }),
    );
  }
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("model manager: invocation token is not available");
  }
  return trimmed;
}

function modelManagerTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("model manager: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `model manager: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `model manager: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `model manager: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `model manager: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function modelManagerRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(MODEL_MANAGER_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}
