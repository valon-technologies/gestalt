import {
  create,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import { errorMessage, type MaybePromise } from "./api.ts";
import {
  GenerateModelResponseSchema,
  ModelMessageSchema,
  ModelMessagePartType,
  ModelProvider as ModelProviderService,
  ModelProviderCapabilitiesSchema,
  type GenerateModelRequest as ProtoGenerateModelRequest,
  type GenerateModelResponse as ProtoGenerateModelResponse,
  type ModelMessage as ProtoModelMessage,
} from "./internal/gen/v1/model_pb.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  type JsonObjectInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";

export type ModelMessageRole = "system" | "user" | "assistant";

export interface ModelMessagePart {
  type?: "text" | undefined;
  text: string;
}

export interface ModelMessage {
  role: ModelMessageRole;
  text?: string | undefined;
  parts?: readonly ModelMessagePart[] | undefined;
  metadata?: JsonObjectInput | undefined;
}

export interface ModelUsage {
  inputTokens: bigint;
  outputTokens: bigint;
  totalTokens: bigint;
}

export interface ModelSubjectContext {
  subjectId: string;
  subjectKind: string;
  credentialSubjectId: string;
  displayName: string;
  authSource: string;
}

export interface GenerateModelRequest {
  providerName: string;
  model: string;
  messages: readonly ModelMessage[];
  responseSchema?: JsonObjectInput | undefined;
  modelOptions?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  subject?: ModelSubjectContext | undefined;
  callerPluginName: string;
}

export interface GenerateModelResponse {
  message?: ModelMessage | undefined;
  outputText: string;
  structuredOutput?: JsonObjectInput | undefined;
  finishReason: string;
  usage?: ModelUsage | undefined;
  providerMetadata?: JsonObjectInput | undefined;
}

export interface GetModelProviderCapabilitiesRequest {}

export interface ModelProviderCapabilities {
  textOutput: boolean;
  structuredOutput: boolean;
  usage: boolean;
  parallelRequests: boolean;
}

export interface ModelProviderOptions extends ProviderBaseOptions {
  generate?: (
    request: GenerateModelRequest,
  ) => MaybePromise<GenerateModelResponse>;
  getCapabilities?: (
    request: GetModelProviderCapabilitiesRequest,
  ) => MaybePromise<ModelProviderCapabilities>;
}

export class ModelProvider extends ProviderBase {
  readonly kind = "model" as const;

  private readonly generateHandler: ModelProviderOptions["generate"];
  private readonly getCapabilitiesHandler: ModelProviderOptions["getCapabilities"];

  constructor(options: ModelProviderOptions) {
    super(options);
    this.generateHandler = options.generate;
    this.getCapabilitiesHandler = options.getCapabilities;
  }

  async generate(request: GenerateModelRequest): Promise<GenerateModelResponse> {
    return await requireModelProviderHandler(
      "generate",
      this.generateHandler,
      request,
    );
  }

  async getCapabilities(
    request: GetModelProviderCapabilitiesRequest = {},
  ): Promise<ModelProviderCapabilities> {
    return await requireModelProviderHandler(
      "get capabilities",
      this.getCapabilitiesHandler,
      request,
    );
  }
}

export function defineModelProvider(options: ModelProviderOptions): ModelProvider {
  return new ModelProvider(options);
}

export function isModelProvider(value: unknown): value is ModelProvider {
  return (
    value instanceof ModelProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "model" &&
      "generate" in value)
  );
}

export function createModelProviderService(
  provider: ModelProvider,
): Partial<ServiceImpl<typeof ModelProviderService>> {
  return {
    async generate(request) {
      return create(
        GenerateModelResponseSchema,
        generateModelResponseToProto(
          await invokeModelProvider("generate", () =>
            provider.generate(generateModelRequestFromProto(request)),
          ),
        ),
      );
    },
    async getCapabilities() {
      return create(
        ModelProviderCapabilitiesSchema,
        modelProviderCapabilitiesToProto(
          await invokeModelProvider("get capabilities", () =>
            provider.getCapabilities({}),
          ),
        ),
      );
    },
  };
}

function generateModelRequestFromProto(
  request: ProtoGenerateModelRequest,
): GenerateModelRequest {
  return {
    providerName: request.providerName,
    model: request.model,
    messages: request.messages.map(modelMessageFromProto),
    responseSchema: optionalObjectFromStruct(request.responseSchema),
    modelOptions: optionalObjectFromStruct(request.modelOptions),
    metadata: optionalObjectFromStruct(request.metadata),
    subject: request.subject
      ? {
        subjectId: request.subject.subjectId,
        subjectKind: request.subject.subjectKind,
        credentialSubjectId: request.subject.credentialSubjectId,
        displayName: request.subject.displayName,
        authSource: request.subject.authSource,
      }
      : undefined,
    callerPluginName: request.callerPluginName,
  };
}

export function generateModelResponseFromProto(
  response: ProtoGenerateModelResponse,
): GenerateModelResponse {
  return {
    message: response.message === undefined
      ? undefined
      : modelMessageFromProto(response.message),
    outputText: response.outputText,
    structuredOutput: optionalObjectFromStruct(response.structuredOutput),
    finishReason: response.finishReason,
    usage: response.usage
      ? {
        inputTokens: response.usage.inputTokens,
        outputTokens: response.usage.outputTokens,
        totalTokens: response.usage.totalTokens,
      }
      : undefined,
    providerMetadata: optionalObjectFromStruct(response.providerMetadata),
  };
}

function generateModelResponseToProto(
  response: GenerateModelResponse,
): MessageInitShape<typeof GenerateModelResponseSchema> {
  return {
    message: response.message ? modelMessageToProto(response.message) : undefined,
    outputText: response.outputText,
    structuredOutput: optionalStruct(response.structuredOutput),
    finishReason: response.finishReason,
    usage: response.usage
      ? {
        $typeName: "gestalt.provider.v1.ModelUsage",
        inputTokens: response.usage.inputTokens,
        outputTokens: response.usage.outputTokens,
        totalTokens: response.usage.totalTokens,
      }
      : undefined,
    providerMetadata: optionalStruct(response.providerMetadata),
  };
}

export function modelMessageFromProto(
  message: ProtoModelMessage,
): ModelMessage {
  return {
    role: message.role as ModelMessageRole,
    text: message.text || undefined,
    parts: message.parts.map((part) => ({
      type: "text",
      text: part.text,
    })),
    metadata: optionalObjectFromStruct(message.metadata),
  };
}

export function modelMessageToProto(
  message: ModelMessage,
): MessageInitShape<typeof ModelMessageSchema> {
  return {
    role: message.role,
    text: message.text ?? "",
    parts: message.parts?.map((part) => ({
      type: ModelMessagePartType.TEXT,
      text: part.text,
    })) ?? [],
    metadata: optionalStruct(message.metadata),
  };
}

function modelProviderCapabilitiesToProto(
  capabilities: ModelProviderCapabilities,
) {
  return {
    textOutput: capabilities.textOutput,
    structuredOutput: capabilities.structuredOutput,
    usage: capabilities.usage,
    parallelRequests: capabilities.parallelRequests,
  };
}

async function requireModelProviderHandler<Request, Response>(
  action: string,
  fn: ((request: Request) => MaybePromise<Response>) | undefined,
  request: Request,
): Promise<Response> {
  if (!fn) {
    throw new ConnectError(
      `model provider ${action} is not implemented`,
      Code.Unimplemented,
    );
  }
  return await fn(request);
}

async function invokeModelProvider<T>(
  action: string,
  fn: () => Promise<T>,
): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    if (error instanceof ConnectError) {
      throw error;
    }
    throw new ConnectError(
      `model provider ${action}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
