import {
  create,
  fromJson,
  isMessage,
  type JsonValue,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import { ValueSchema, type Value } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type Interceptor,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentExecutionStatus,
  AgentHost as AgentHostService,
  AgentInteractionSchema,
  AgentInteractionState,
  AgentInteractionType,
  AgentMessagePartType,
  AgentProvider as AgentProviderService,
  AgentProviderCapabilitiesSchema,
  AgentSessionSchema,
  AgentSessionState,
  AgentToolSourceMode,
  AgentTurnDisplaySchema,
  AgentTurnEventSchema,
  AgentTurnSchema,
  GetAgentProviderCapabilitiesRequestSchema,
  ListAgentProviderInteractionsResponseSchema,
  ListAgentProviderSessionsResponseSchema,
  ListAgentProviderTurnEventsResponseSchema,
  ListAgentProviderTurnsResponseSchema,
  type AgentActor,
  type AgentInteraction,
  type AgentMessage,
  type AgentMessagePart,
  type AgentMessagePartImageRef,
  type AgentMessagePartToolCall,
  type AgentMessagePartToolResult,
  type AgentProviderCapabilities,
  type AgentSession,
  type AgentToolRef,
  type AgentTurn,
  type AgentTurnDisplay,
  type AgentTurnEvent,
  type CancelAgentProviderTurnRequest,
  type CreateAgentProviderSessionRequest,
  type CreateAgentProviderTurnRequest,
  type ExecuteAgentToolRequest,
  type ExecuteAgentToolResponse,
  type GetAgentProviderCapabilitiesRequest,
  type GetAgentProviderInteractionRequest,
  type GetAgentProviderSessionRequest,
  type GetAgentProviderTurnRequest,
  type ListAgentProviderInteractionsRequest,
  type ListAgentProviderSessionsRequest,
  type ListAgentProviderTurnEventsRequest,
  type ListAgentProviderTurnsRequest,
  type ResolveAgentProviderInteractionRequest,
  type ResolvedAgentTool,
  type SearchAgentToolsRequest,
  type SearchAgentToolsResponse,
  type UpdateAgentProviderSessionRequest,
} from "../gen/v1/agent_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

export const ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET";
export const ENV_AGENT_HOST_SOCKET_TOKEN = `${ENV_AGENT_HOST_SOCKET}_TOKEN`;
const AGENT_HOST_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export type {
  AgentActor,
  AgentInteraction,
  AgentMessage,
  AgentMessagePart,
  AgentMessagePartImageRef,
  AgentMessagePartToolCall,
  AgentMessagePartToolResult,
  AgentProviderCapabilities,
  AgentSession,
  AgentToolRef,
  AgentTurn,
  AgentTurnDisplay,
  AgentTurnEvent,
  CancelAgentProviderTurnRequest,
  CreateAgentProviderSessionRequest,
  CreateAgentProviderTurnRequest,
  ExecuteAgentToolRequest,
  ExecuteAgentToolResponse,
  GetAgentProviderCapabilitiesRequest,
  GetAgentProviderInteractionRequest,
  GetAgentProviderSessionRequest,
  GetAgentProviderTurnRequest,
  ListAgentProviderInteractionsRequest,
  ListAgentProviderSessionsRequest,
  ListAgentProviderTurnEventsRequest,
  ListAgentProviderTurnsRequest,
  ResolveAgentProviderInteractionRequest,
  ResolvedAgentTool,
  SearchAgentToolsRequest,
  SearchAgentToolsResponse,
  UpdateAgentProviderSessionRequest,
};
export {
  AgentExecutionStatus,
  AgentInteractionState,
  AgentInteractionType,
  AgentMessagePartType,
  AgentSessionState,
  AgentToolSourceMode,
};

export type AgentTurnDisplayValue =
  | JsonValue
  | Value
  | MessageInitShape<typeof ValueSchema>;

export type AgentTurnDisplayInit = Omit<
  MessageInitShape<typeof AgentTurnDisplaySchema>,
  "$typeName" | "input" | "output" | "error"
> & {
  input?: AgentTurnDisplayValue | undefined;
  output?: AgentTurnDisplayValue | undefined;
  error?: AgentTurnDisplayValue | undefined;
};

export type AgentTurnEventInit = Omit<
  MessageInitShape<typeof AgentTurnEventSchema>,
  "$typeName" | "display"
> & {
  display?: AgentTurnDisplayInit | undefined;
};

export interface AgentProviderOptions extends RuntimeProviderOptions {
  createSession?: (
    request: CreateAgentProviderSessionRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentSessionSchema>>;
  getSession?: (
    request: GetAgentProviderSessionRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentSessionSchema>>;
  listSessions?: (
    request: ListAgentProviderSessionsRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentSessionSchema>[]>;
  updateSession?: (
    request: UpdateAgentProviderSessionRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentSessionSchema>>;
  createTurn?: (
    request: CreateAgentProviderTurnRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentTurnSchema>>;
  getTurn?: (
    request: GetAgentProviderTurnRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentTurnSchema>>;
  listTurns?: (
    request: ListAgentProviderTurnsRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentTurnSchema>[]>;
  cancelTurn?: (
    request: CancelAgentProviderTurnRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentTurnSchema>>;
  listTurnEvents?: (
    request: ListAgentProviderTurnEventsRequest,
  ) => MaybePromise<AgentTurnEventInit[]>;
  getInteraction?: (
    request: GetAgentProviderInteractionRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentInteractionSchema>>;
  listInteractions?: (
    request: ListAgentProviderInteractionsRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentInteractionSchema>[]>;
  resolveInteraction?: (
    request: ResolveAgentProviderInteractionRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentInteractionSchema>>;
  getCapabilities?: (
    request: GetAgentProviderCapabilitiesRequest,
  ) => MaybePromise<MessageInitShape<typeof AgentProviderCapabilitiesSchema>>;
}

export class AgentProvider extends RuntimeProvider {
  readonly kind = "agent" as const;

  private readonly createSessionHandler: AgentProviderOptions["createSession"];
  private readonly getSessionHandler: AgentProviderOptions["getSession"];
  private readonly listSessionsHandler: AgentProviderOptions["listSessions"];
  private readonly updateSessionHandler: AgentProviderOptions["updateSession"];
  private readonly createTurnHandler: AgentProviderOptions["createTurn"];
  private readonly getTurnHandler: AgentProviderOptions["getTurn"];
  private readonly listTurnsHandler: AgentProviderOptions["listTurns"];
  private readonly cancelTurnHandler: AgentProviderOptions["cancelTurn"];
  private readonly listTurnEventsHandler: AgentProviderOptions["listTurnEvents"];
  private readonly getInteractionHandler: AgentProviderOptions["getInteraction"];
  private readonly listInteractionsHandler: AgentProviderOptions["listInteractions"];
  private readonly resolveInteractionHandler: AgentProviderOptions["resolveInteraction"];
  private readonly getCapabilitiesHandler: AgentProviderOptions["getCapabilities"];

  constructor(options: AgentProviderOptions) {
    super(options);
    this.createSessionHandler = options.createSession;
    this.getSessionHandler = options.getSession;
    this.listSessionsHandler = options.listSessions;
    this.updateSessionHandler = options.updateSession;
    this.createTurnHandler = options.createTurn;
    this.getTurnHandler = options.getTurn;
    this.listTurnsHandler = options.listTurns;
    this.cancelTurnHandler = options.cancelTurn;
    this.listTurnEventsHandler = options.listTurnEvents;
    this.getInteractionHandler = options.getInteraction;
    this.listInteractionsHandler = options.listInteractions;
    this.resolveInteractionHandler = options.resolveInteraction;
    this.getCapabilitiesHandler = options.getCapabilities;
  }

  async createSession(
    request: CreateAgentProviderSessionRequest,
  ): Promise<MessageInitShape<typeof AgentSessionSchema>> {
    return await requireAgentProviderHandler(
      "create session",
      this.createSessionHandler,
      request,
    );
  }

  async getSession(
    request: GetAgentProviderSessionRequest,
  ): Promise<MessageInitShape<typeof AgentSessionSchema>> {
    return await requireAgentProviderHandler(
      "get session",
      this.getSessionHandler,
      request,
    );
  }

  async listSessions(
    request: ListAgentProviderSessionsRequest,
  ): Promise<MessageInitShape<typeof AgentSessionSchema>[]> {
    return await requireAgentProviderHandler(
      "list sessions",
      this.listSessionsHandler,
      request,
    );
  }

  async updateSession(
    request: UpdateAgentProviderSessionRequest,
  ): Promise<MessageInitShape<typeof AgentSessionSchema>> {
    return await requireAgentProviderHandler(
      "update session",
      this.updateSessionHandler,
      request,
    );
  }

  async createTurn(
    request: CreateAgentProviderTurnRequest,
  ): Promise<MessageInitShape<typeof AgentTurnSchema>> {
    return await requireAgentProviderHandler(
      "create turn",
      this.createTurnHandler,
      request,
    );
  }

  async getTurn(
    request: GetAgentProviderTurnRequest,
  ): Promise<MessageInitShape<typeof AgentTurnSchema>> {
    return await requireAgentProviderHandler(
      "get turn",
      this.getTurnHandler,
      request,
    );
  }

  async listTurns(
    request: ListAgentProviderTurnsRequest,
  ): Promise<MessageInitShape<typeof AgentTurnSchema>[]> {
    return await requireAgentProviderHandler(
      "list turns",
      this.listTurnsHandler,
      request,
    );
  }

  async cancelTurn(
    request: CancelAgentProviderTurnRequest,
  ): Promise<MessageInitShape<typeof AgentTurnSchema>> {
    return await requireAgentProviderHandler(
      "cancel turn",
      this.cancelTurnHandler,
      request,
    );
  }

  async listTurnEvents(
    request: ListAgentProviderTurnEventsRequest,
  ): Promise<AgentTurnEventInit[]> {
    return await requireAgentProviderHandler(
      "list turn events",
      this.listTurnEventsHandler,
      request,
    );
  }

  async getInteraction(
    request: GetAgentProviderInteractionRequest,
  ): Promise<MessageInitShape<typeof AgentInteractionSchema>> {
    return await requireAgentProviderHandler(
      "get interaction",
      this.getInteractionHandler,
      request,
    );
  }

  async listInteractions(
    request: ListAgentProviderInteractionsRequest,
  ): Promise<MessageInitShape<typeof AgentInteractionSchema>[]> {
    return await requireAgentProviderHandler(
      "list interactions",
      this.listInteractionsHandler,
      request,
    );
  }

  async resolveInteraction(
    request: ResolveAgentProviderInteractionRequest,
  ): Promise<MessageInitShape<typeof AgentInteractionSchema>> {
    return await requireAgentProviderHandler(
      "resolve interaction",
      this.resolveInteractionHandler,
      request,
    );
  }

  async getCapabilities(
    request: GetAgentProviderCapabilitiesRequest = create(
      GetAgentProviderCapabilitiesRequestSchema,
      {},
    ),
  ): Promise<MessageInitShape<typeof AgentProviderCapabilitiesSchema>> {
    return await requireAgentProviderHandler(
      "get capabilities",
      this.getCapabilitiesHandler,
      request,
    );
  }
}

export function defineAgentProvider(options: AgentProviderOptions): AgentProvider {
  return new AgentProvider(options);
}

function normalizeAgentTurnEvents(
  events: AgentTurnEventInit[],
): MessageInitShape<typeof AgentTurnEventSchema>[] {
  return events.map((event) => normalizeAgentTurnEvent(event));
}

function normalizeAgentTurnEvent(
  event: AgentTurnEventInit,
): MessageInitShape<typeof AgentTurnEventSchema> {
  const display = event.display;
  if (!display) {
    return event as MessageInitShape<typeof AgentTurnEventSchema>;
  }
  return {
    ...event,
    display: {
      ...display,
      input: normalizeAgentTurnDisplayValue(display.input),
      output: normalizeAgentTurnDisplayValue(display.output),
      error: normalizeAgentTurnDisplayValue(display.error),
    },
  } as MessageInitShape<typeof AgentTurnEventSchema>;
}

function normalizeAgentTurnDisplayValue(value: unknown): Value | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (isMessage(value, ValueSchema)) {
    return value;
  }
  if (isValueInit(value)) {
    return create(ValueSchema, value);
  }
  return fromJson(ValueSchema, value as JsonValue);
}

function isValueInit(value: unknown): value is MessageInitShape<typeof ValueSchema> {
  if (value === null || typeof value !== "object") {
    return false;
  }
  const kind = (value as { kind?: unknown }).kind;
  return (
    kind !== null &&
    typeof kind === "object" &&
    typeof (kind as { case?: unknown }).case === "string" &&
    "value" in kind
  );
}

export function isAgentProvider(value: unknown): value is AgentProvider {
  return (
    value instanceof AgentProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "agent" &&
      "createSession" in value &&
      "createTurn" in value)
  );
}

export class AgentHost {
  private readonly client: Client<typeof AgentHostService>;

  constructor() {
    const target = process.env[ENV_AGENT_HOST_SOCKET];
    if (!target) {
      throw new Error(`agent host: ${ENV_AGENT_HOST_SOCKET} is not set`);
    }
    const relayToken = process.env[ENV_AGENT_HOST_SOCKET_TOKEN]?.trim() ?? "";
    const transport = createGrpcTransport({
      ...agentHostTransportOptions(target),
      interceptors: relayToken ? [agentHostRelayTokenInterceptor(relayToken)] : [],
    });
    this.client = createClient(AgentHostService, transport);
  }

  async executeTool(
    request: ExecuteAgentToolRequest,
  ): Promise<ExecuteAgentToolResponse> {
    return await this.client.executeTool(request);
  }

  async searchTools(
    request: SearchAgentToolsRequest,
  ): Promise<SearchAgentToolsResponse> {
    return await this.client.searchTools(request);
  }
}

function agentHostTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("agent host: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `agent host: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `agent host: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `agent host: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `agent host: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function agentHostRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(AGENT_HOST_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}

export function createAgentProviderService(
  provider: AgentProvider,
): Partial<ServiceImpl<typeof AgentProviderService>> {
  return {
    async createSession(request) {
      return create(
        AgentSessionSchema,
        await invokeAgentProvider("create session", () =>
          provider.createSession(request),
        ),
      );
    },
    async getSession(request) {
      return create(
        AgentSessionSchema,
        await invokeAgentProvider("get session", () =>
          provider.getSession(request),
        ),
      );
    },
    async listSessions(request) {
      return create(ListAgentProviderSessionsResponseSchema, {
        sessions: await invokeAgentProvider("list sessions", () =>
          provider.listSessions(request),
        ),
      });
    },
    async updateSession(request) {
      return create(
        AgentSessionSchema,
        await invokeAgentProvider("update session", () =>
          provider.updateSession(request),
        ),
      );
    },
    async createTurn(request) {
      return create(
        AgentTurnSchema,
        await invokeAgentProvider("create turn", () =>
          provider.createTurn(request),
        ),
      );
    },
    async getTurn(request) {
      return create(
        AgentTurnSchema,
        await invokeAgentProvider("get turn", () => provider.getTurn(request)),
      );
    },
    async listTurns(request) {
      return create(ListAgentProviderTurnsResponseSchema, {
        turns: await invokeAgentProvider("list turns", () =>
          provider.listTurns(request),
        ),
      });
    },
    async cancelTurn(request) {
      return create(
        AgentTurnSchema,
        await invokeAgentProvider("cancel turn", () =>
          provider.cancelTurn(request),
        ),
      );
    },
    async listTurnEvents(request) {
      return create(ListAgentProviderTurnEventsResponseSchema, {
        events: normalizeAgentTurnEvents(
          await invokeAgentProvider("list turn events", () =>
            provider.listTurnEvents(request),
          ),
        ),
      });
    },
    async getInteraction(request) {
      return create(
        AgentInteractionSchema,
        await invokeAgentProvider("get interaction", () =>
          provider.getInteraction(request),
        ),
      );
    },
    async listInteractions(request) {
      return create(ListAgentProviderInteractionsResponseSchema, {
        interactions: await invokeAgentProvider("list interactions", () =>
          provider.listInteractions(request),
        ),
      });
    },
    async resolveInteraction(request) {
      return create(
        AgentInteractionSchema,
        await invokeAgentProvider("resolve interaction", () =>
          provider.resolveInteraction(request),
        ),
      );
    },
    async getCapabilities(request) {
      return create(
        AgentProviderCapabilitiesSchema,
        await invokeAgentProvider("get capabilities", () =>
          provider.getCapabilities(request),
        ),
      );
    },
  };
}

async function requireAgentProviderHandler<Request, Response>(
  action: string,
  fn: ((request: Request) => MaybePromise<Response>) | undefined,
  request: Request,
): Promise<Response> {
  if (!fn) {
    throw new ConnectError(
      `agent provider ${action} is not implemented`,
      Code.Unimplemented,
    );
  }
  return await fn(request);
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
