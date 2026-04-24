import { connect } from "node:net";

import { create, type MessageInitShape } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentHost as AgentHostService,
  AgentInteractionState,
  AgentInteractionType,
  AgentMessagePartType,
  AgentInteractionSchema,
  AgentProviderCapabilitiesSchema,
  AgentProvider as AgentProviderService,
  AgentRunStatus,
  AgentToolSourceMode,
  BoundAgentRunSchema,
  GetAgentProviderCapabilitiesRequestSchema,
  ListAgentProviderRunsResponseSchema,
  type AgentActor,
  type AgentMessagePart,
  type AgentMessagePartImageRef,
  type AgentMessagePartToolCall,
  type AgentMessagePartToolResult,
  type AgentInteraction,
  type AgentMessage,
  type AgentToolRef,
  type BoundAgentRun,
  type BoundAgentToolTarget,
  type CancelAgentProviderRunRequest,
  type EmitAgentEventRequest,
  type ExecuteAgentToolRequest,
  type ExecuteAgentToolResponse,
  type GetAgentProviderCapabilitiesRequest,
  type GetAgentProviderRunRequest,
  type ListAgentProviderRunsRequest,
  type RequestAgentInteractionRequest,
  type ResolvedAgentTool,
  type ResumeAgentProviderRunRequest,
  type StartAgentProviderRunRequest,
} from "../gen/v1/agent_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

export const ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET";

export type {
  AgentActor,
  AgentInteraction,
  AgentMessagePart,
  AgentMessagePartImageRef,
  AgentMessagePartToolCall,
  AgentMessagePartToolResult,
  AgentMessage,
  AgentToolRef,
  BoundAgentRun,
  BoundAgentToolTarget,
  CancelAgentProviderRunRequest,
  EmitAgentEventRequest,
  ExecuteAgentToolRequest,
  ExecuteAgentToolResponse,
  GetAgentProviderRunRequest,
  ListAgentProviderRunsRequest,
  RequestAgentInteractionRequest,
  ResolvedAgentTool,
  ResumeAgentProviderRunRequest,
  StartAgentProviderRunRequest,
};
export {
  AgentInteractionState,
  AgentInteractionType,
  AgentMessagePartType,
  AgentRunStatus,
  AgentToolSourceMode,
};

export interface AgentProviderOptions extends RuntimeProviderOptions {
  startRun: (
    request: StartAgentProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundAgentRunSchema>>;
  getRun: (
    request: GetAgentProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundAgentRunSchema>>;
  listRuns: (
    request: ListAgentProviderRunsRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundAgentRunSchema>[]>;
  cancelRun: (
    request: CancelAgentProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundAgentRunSchema>>;
  getCapabilities?: (
    request: GetAgentProviderCapabilitiesRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentProviderCapabilitiesSchema>>;
  resumeRun?: (
    request: ResumeAgentProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundAgentRunSchema>>;
}

export class AgentProvider extends RuntimeProvider {
  readonly kind = "agent" as const;

  private readonly startRunHandler: AgentProviderOptions["startRun"];
  private readonly getRunHandler: AgentProviderOptions["getRun"];
  private readonly listRunsHandler: AgentProviderOptions["listRuns"];
  private readonly cancelRunHandler: AgentProviderOptions["cancelRun"];
  private readonly getCapabilitiesHandler: AgentProviderOptions["getCapabilities"];
  private readonly resumeRunHandler: AgentProviderOptions["resumeRun"];

  constructor(options: AgentProviderOptions) {
    super(options);
    this.startRunHandler = options.startRun;
    this.getRunHandler = options.getRun;
    this.listRunsHandler = options.listRuns;
    this.cancelRunHandler = options.cancelRun;
    this.getCapabilitiesHandler = options.getCapabilities;
    this.resumeRunHandler = options.resumeRun;
  }

  async startRun(
    request: StartAgentProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundAgentRunSchema>> {
    return await this.startRunHandler(request);
  }

  async getRun(
    request: GetAgentProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundAgentRunSchema>> {
    return await this.getRunHandler(request);
  }

  async listRuns(
    request: ListAgentProviderRunsRequest,
  ): Promise<MessageInitShape<typeof BoundAgentRunSchema>[]> {
    return await this.listRunsHandler(request);
  }

  async cancelRun(
    request: CancelAgentProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundAgentRunSchema>> {
    return await this.cancelRunHandler(request);
  }

  async getCapabilities(): Promise<MessageInitShape<typeof AgentProviderCapabilitiesSchema>> {
    if (!this.getCapabilitiesHandler) {
      throw new ConnectError("agent provider get capabilities is not implemented", Code.Unimplemented);
    }
    return await this.getCapabilitiesHandler(create(GetAgentProviderCapabilitiesRequestSchema, {}));
  }

  async resumeRun(
    request: ResumeAgentProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundAgentRunSchema>> {
    if (!this.resumeRunHandler) {
      throw new ConnectError("agent provider resume run is not implemented", Code.Unimplemented);
    }
    return await this.resumeRunHandler(request);
  }
}

export function defineAgentProvider(options: AgentProviderOptions): AgentProvider {
  return new AgentProvider(options);
}

export function isAgentProvider(value: unknown): value is AgentProvider {
  return (
    value instanceof AgentProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "agent" &&
      "startRun" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "cancelRun" in value)
  );
}

export class AgentHost {
  private readonly client: Client<typeof AgentHostService>;

  constructor() {
    const socketPath = process.env[ENV_AGENT_HOST_SOCKET];
    if (!socketPath) {
      throw new Error(`agent host: ${ENV_AGENT_HOST_SOCKET} is not set`);
    }
    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
    });
    this.client = createClient(AgentHostService, transport);
  }

  async executeTool(
    request: ExecuteAgentToolRequest,
  ): Promise<ExecuteAgentToolResponse> {
    return await this.client.executeTool(request);
  }

  async emitEvent(request: EmitAgentEventRequest): Promise<void> {
    await this.client.emitEvent(request);
  }

  async requestInteraction(
    request: RequestAgentInteractionRequest,
  ): Promise<AgentInteraction> {
    return await this.client.requestInteraction(request);
  }
}

export function createAgentProviderService(
  provider: AgentProvider,
): Partial<ServiceImpl<typeof AgentProviderService>> {
  return {
    async startRun(request) {
      return create(
        BoundAgentRunSchema,
        await invokeAgentProvider("start run", () => provider.startRun(request)),
      );
    },
    async getRun(request) {
      return create(
        BoundAgentRunSchema,
        await invokeAgentProvider("get run", () => provider.getRun(request)),
      );
    },
    async listRuns(request) {
      return create(ListAgentProviderRunsResponseSchema, {
        runs: await invokeAgentProvider("list runs", () => provider.listRuns(request)),
      });
    },
    async cancelRun(request) {
      return create(
        BoundAgentRunSchema,
        await invokeAgentProvider("cancel run", () => provider.cancelRun(request)),
      );
    },
    async getCapabilities() {
      return create(
        AgentProviderCapabilitiesSchema,
        await invokeAgentProvider("get capabilities", () => provider.getCapabilities()),
      );
    },
    async resumeRun(request) {
      return create(
        BoundAgentRunSchema,
        await invokeAgentProvider("resume run", () => provider.resumeRun(request)),
      );
    },
  };
}

async function invokeAgentProvider<T>(
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
      `agent provider ${action}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
