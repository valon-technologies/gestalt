import { connect } from "node:net";

import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
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
  AgentProvider as AgentProviderService,
  AgentRunStatus,
  AgentToolSourceMode,
  BoundAgentRunSchema,
  ListAgentProviderRunsResponseSchema,
  type AgentActor,
  type AgentMessage,
  type AgentToolRef,
  type BoundAgentRun,
  type BoundAgentToolTarget,
  type CancelAgentProviderRunRequest,
  type EmitAgentEventRequest,
  type ExecuteAgentToolRequest,
  type ExecuteAgentToolResponse,
  type GetAgentProviderRunRequest,
  type ListAgentProviderRunsRequest,
  type ResolvedAgentTool,
  type StartAgentProviderRunRequest,
} from "../gen/v1/agent_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

export const ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET";

export type {
  AgentActor,
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
  ResolvedAgentTool,
  StartAgentProviderRunRequest,
};
export { AgentRunStatus, AgentToolSourceMode };

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
}

export class AgentProvider extends RuntimeProvider {
  readonly kind = "agent" as const;

  private readonly startRunHandler: AgentProviderOptions["startRun"];
  private readonly getRunHandler: AgentProviderOptions["getRun"];
  private readonly listRunsHandler: AgentProviderOptions["listRuns"];
  private readonly cancelRunHandler: AgentProviderOptions["cancelRun"];

  constructor(options: AgentProviderOptions) {
    super(options);
    this.startRunHandler = options.startRun;
    this.getRunHandler = options.getRun;
    this.listRunsHandler = options.listRuns;
    this.cancelRunHandler = options.cancelRun;
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
