import {
  create,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";

import {
  AgentHost as AgentHostService,
  AgentInteractionSchema,
  AgentProvider as AgentProviderService,
  AgentProviderCapabilitiesSchema,
  AgentSessionSchema,
  AgentTurnEventSchema,
  AgentTurnSchema,
  AgentExecutionStatus as ProtoAgentExecutionStatus,
  AgentInteractionState as ProtoAgentInteractionState,
  AgentInteractionType as ProtoAgentInteractionType,
  AgentMessagePartType as ProtoAgentMessagePartType,
  AgentSessionState as ProtoAgentSessionState,
  AgentToolSourceMode as ProtoAgentToolSourceMode,
  ExecuteAgentToolRequestSchema,
  GetAgentProviderCapabilitiesRequestSchema,
  ListAgentToolsRequestSchema,
  ListAgentProviderInteractionsResponseSchema,
  ListAgentProviderSessionsResponseSchema,
  ListAgentProviderTurnEventsResponseSchema,
  ListAgentProviderTurnsResponseSchema,
  ResolveAgentConnectionRequestSchema,
  type AgentActor as ProtoAgentActor,
  type AgentInteraction as ProtoAgentInteraction,
  type AgentMessagePartImageRef as ProtoAgentMessagePartImageRef,
  type AgentMessagePartToolCall as ProtoAgentMessagePartToolCall,
  type AgentMessagePartToolResult as ProtoAgentMessagePartToolResult,
  type AgentSession as ProtoAgentSession,
  type AgentTurn as ProtoAgentTurn,
  type AgentTurnEvent as ProtoAgentTurnEvent,
  type CancelAgentProviderTurnRequest as ProtoCancelAgentProviderTurnRequest,
  type CreateAgentProviderSessionRequest as ProtoCreateAgentProviderSessionRequest,
  type CreateAgentProviderTurnRequest as ProtoCreateAgentProviderTurnRequest,
  type ExecuteAgentToolRequest as ProtoExecuteAgentToolRequest,
  type ExecuteAgentToolResponse as ProtoExecuteAgentToolResponse,
  type GetAgentProviderCapabilitiesRequest as ProtoGetAgentProviderCapabilitiesRequest,
  type GetAgentProviderInteractionRequest as ProtoGetAgentProviderInteractionRequest,
  type GetAgentProviderSessionRequest as ProtoGetAgentProviderSessionRequest,
  type GetAgentProviderTurnRequest as ProtoGetAgentProviderTurnRequest,
  type ListAgentToolsRequest as ProtoListAgentToolsRequest,
  type ListAgentToolsResponse as ProtoListAgentToolsResponse,
  type ListAgentProviderInteractionsRequest as ProtoListAgentProviderInteractionsRequest,
  type ListAgentProviderSessionsRequest as ProtoListAgentProviderSessionsRequest,
  type ListAgentProviderTurnEventsRequest as ProtoListAgentProviderTurnEventsRequest,
  type ListAgentProviderTurnsRequest as ProtoListAgentProviderTurnsRequest,
  type ListedAgentTool as ProtoListedAgentTool,
  type ResolveAgentConnectionRequest as ProtoResolveAgentConnectionRequest,
  type ResolveAgentProviderInteractionRequest as ProtoResolveAgentProviderInteractionRequest,
  type ResolvedAgentConnection as ProtoResolvedAgentConnection,
  type ResolvedAgentTool as ProtoResolvedAgentTool,
  type UpdateAgentProviderSessionRequest as ProtoUpdateAgentProviderSessionRequest,
} from "./internal/gen/v1/agent_pb.ts";
import {
  type SubjectContext as ProtoSubjectContext,
  type AgentToolRef as ProtoAgentToolRef,
} from "./internal/gen/v1/app_pb.ts";
import {
  errorMessage,
  type ExternalIdentity,
  type MaybePromise,
  type Subject,
  type SubjectInput,
} from "./api.ts";
import {
  agentActorFromProto,
  agentActorToProto,
  agentOutputFromProto,
  agentOutputToProto,
  agentMessageFromProto,
  agentMessageToProto,
  agentToolRefFromProto,
  agentTurnOutputFromProto,
  agentTurnOutputToProto,
  agentTurnDisplayToProto,
} from "./agent-conversions.ts";
import {
  dateFromTimestamp,
  timestampFromDate,
  type JsonInput,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

/** Native message-part type constants for authored agent messages. */
export const AgentMessagePartType = {
  UNSPECIFIED: ProtoAgentMessagePartType.UNSPECIFIED,
  TEXT: ProtoAgentMessagePartType.TEXT,
  JSON: ProtoAgentMessagePartType.JSON,
  TOOL_CALL: ProtoAgentMessagePartType.TOOL_CALL,
  TOOL_RESULT: ProtoAgentMessagePartType.TOOL_RESULT,
  IMAGE_REF: ProtoAgentMessagePartType.IMAGE_REF,
} as const;
export type AgentMessagePartType =
  (typeof AgentMessagePartType)[keyof typeof AgentMessagePartType];

/** Native tool-source constants for authored agent provider capabilities. */
export const AgentToolSourceMode = {
  UNSPECIFIED: ProtoAgentToolSourceMode.UNSPECIFIED,
  MCP_CATALOG: ProtoAgentToolSourceMode.MCP_CATALOG,
  NONE: ProtoAgentToolSourceMode.NONE,
} as const;
export type AgentToolSourceMode =
  (typeof AgentToolSourceMode)[keyof typeof AgentToolSourceMode];

/** Native execution-status constants for authored agent turns. */
export const AgentExecutionStatus = {
  UNSPECIFIED: ProtoAgentExecutionStatus.UNSPECIFIED,
  PENDING: ProtoAgentExecutionStatus.PENDING,
  RUNNING: ProtoAgentExecutionStatus.RUNNING,
  SUCCEEDED: ProtoAgentExecutionStatus.SUCCEEDED,
  FAILED: ProtoAgentExecutionStatus.FAILED,
  CANCELED: ProtoAgentExecutionStatus.CANCELED,
  WAITING_FOR_INPUT: ProtoAgentExecutionStatus.WAITING_FOR_INPUT,
} as const;
export type AgentExecutionStatus =
  (typeof AgentExecutionStatus)[keyof typeof AgentExecutionStatus];

/** Native session-state constants for authored agent sessions. */
export const AgentSessionState = {
  UNSPECIFIED: ProtoAgentSessionState.UNSPECIFIED,
  ACTIVE: ProtoAgentSessionState.ACTIVE,
  ARCHIVED: ProtoAgentSessionState.ARCHIVED,
} as const;
export type AgentSessionState =
  (typeof AgentSessionState)[keyof typeof AgentSessionState];

/** Native interaction-type constants for authored agent interactions. */
export const AgentInteractionType = {
  UNSPECIFIED: ProtoAgentInteractionType.UNSPECIFIED,
  APPROVAL: ProtoAgentInteractionType.APPROVAL,
  CLARIFICATION: ProtoAgentInteractionType.CLARIFICATION,
  INPUT: ProtoAgentInteractionType.INPUT,
} as const;
export type AgentInteractionType =
  (typeof AgentInteractionType)[keyof typeof AgentInteractionType];

/** Native interaction-state constants for authored agent interactions. */
export const AgentInteractionState = {
  UNSPECIFIED: ProtoAgentInteractionState.UNSPECIFIED,
  PENDING: ProtoAgentInteractionState.PENDING,
  RESOLVED: ProtoAgentInteractionState.RESOLVED,
  CANCELED: ProtoAgentInteractionState.CANCELED,
} as const;
export type AgentInteractionState =
  (typeof AgentInteractionState)[keyof typeof AgentInteractionState];

export interface AgentMessagePartToolCall {
  id?: string | undefined;
  toolId?: string | undefined;
  arguments?: JsonObjectInput | undefined;
}

export interface AgentMessagePartToolResult {
  toolCallId?: string | undefined;
  status?: number | undefined;
  content?: string | undefined;
  output?: JsonObjectInput | undefined;
}

export interface AgentMessagePartImageRef {
  uri?: string | undefined;
  mimeType?: string | undefined;
}

export interface AgentMessagePart {
  type?: AgentMessagePartType | undefined;
  text?: string | undefined;
  json?: JsonObjectInput | undefined;
  toolCall?: AgentMessagePartToolCall | undefined;
  toolResult?: AgentMessagePartToolResult | undefined;
  imageRef?: AgentMessagePartImageRef | undefined;
}

export interface AgentMessage {
  role?: string | undefined;
  text?: string | undefined;
  parts?: readonly AgentMessagePart[] | undefined;
  metadata?: JsonObjectInput | undefined;
}

export interface AgentActor {
  subjectId?: string | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

export interface AgentToolRef {
  app?: string | undefined;
  operation?: string | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  title?: string | undefined;
  description?: string | undefined;
  system?: string | undefined;
  runAs?: SubjectInput | undefined;
  runAsExternalIdentity?: ExternalIdentity | undefined;
}

export interface ResolvedAgentTool {
  id?: string | undefined;
  name?: string | undefined;
  description?: string | undefined;
  parametersSchema?: JsonObjectInput | undefined;
}

export interface AgentProviderCapabilities {
  streamingText?: boolean | undefined;
  toolCalls?: boolean | undefined;
  parallelToolCalls?: boolean | undefined;
  interactions?: boolean | undefined;
  resumableTurns?: boolean | undefined;
  reasoningSummaries?: boolean | undefined;
  boundedListHydration?: boolean | undefined;
  supportedToolSources?: readonly AgentToolSourceMode[] | undefined;
  supportsSessionStart?: boolean | undefined;
  supportsPreparedWorkspace?: boolean | undefined;
}

export interface AgentSession {
  id?: string | undefined;
  providerName?: string | undefined;
  model?: string | undefined;
  clientRef?: string | undefined;
  state?: AgentSessionState | undefined;
  metadata?: JsonObjectInput | undefined;
  createdBy?: AgentActor | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  lastTurnAt?: Date | undefined;
}

export interface AgentSessionStartHookOutput {
  additionalContext?: boolean | undefined;
  metadata?: boolean | undefined;
}

export interface AgentSessionStartHook {
  id?: string | undefined;
  type?: string | undefined;
  command?: readonly string[] | undefined;
  cwd?: string | undefined;
  timeout?: string | undefined;
  env?: Record<string, string> | undefined;
  output?: AgentSessionStartHookOutput | undefined;
}

export interface AgentSessionStartConfig {
  hooks?: readonly AgentSessionStartHook[] | undefined;
}

export interface AgentPreparedWorkspace {
  root?: string | undefined;
  cwd?: string | undefined;
}

export interface CreateAgentProviderSessionRequest {
  sessionId: string;
  idempotencyKey: string;
  model: string;
  clientRef: string;
  metadata?: JsonObjectInput | undefined;
  createdBy?: AgentActor | undefined;
  subject?: Subject | undefined;
  sessionStart?: AgentSessionStartConfig | undefined;
  preparedWorkspace?: AgentPreparedWorkspace | undefined;
  invocationToken: string;
}

export interface GetAgentProviderSessionRequest {
  sessionId: string;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface ListAgentProviderSessionsRequest {
  subject?: Subject | undefined;
  sessionIds: readonly string[];
  state: AgentSessionState;
  limit: number;
  summaryOnly: boolean;
  invocationToken: string;
}

export interface ListAgentProviderSessionsResponse {
  sessions: readonly AgentSession[];
}

export interface UpdateAgentProviderSessionRequest {
  sessionId: string;
  clientRef: string;
  state: AgentSessionState;
  metadata?: JsonObjectInput | undefined;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface AgentTurn {
  id?: string | undefined;
  sessionId?: string | undefined;
  providerName?: string | undefined;
  model?: string | undefined;
  status?: AgentExecutionStatus | undefined;
  messages?: readonly AgentMessage[] | undefined;
  output?: AgentTurnOutput | undefined;
  statusMessage?: string | undefined;
  createdBy?: AgentActor | undefined;
  createdAt?: Date | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
  executionRef?: string | undefined;
}

export interface AgentTurnStructuredOutput {
  text?: string | undefined;
  value?: JsonObjectInput | undefined;
}

export type AgentTurnOutput =
  | { text: string; structured?: undefined }
  | { text?: undefined; structured: AgentTurnStructuredOutput };

export interface AgentTurnDisplay {
  kind?: string | undefined;
  phase?: string | undefined;
  text?: string | undefined;
  label?: string | undefined;
  ref?: string | undefined;
  parentRef?: string | undefined;
  input?: JsonInput | undefined;
  output?: JsonInput | undefined;
  error?: JsonInput | undefined;
  action?: string | undefined;
  format?: string | undefined;
  language?: string | undefined;
}

export interface CreateAgentProviderTurnRequest {
  turnId: string;
  sessionId: string;
  idempotencyKey: string;
  model: string;
  messages: readonly AgentMessage[];
  tools: readonly ResolvedAgentTool[];
  output: AgentOutput;
  metadata?: JsonObjectInput | undefined;
  createdBy?: AgentActor | undefined;
  executionRef: string;
  toolRefs: readonly AgentToolRef[];
  toolSource: AgentToolSourceMode;
  subject?: Subject | undefined;
  modelOptions?: JsonObjectInput | undefined;
  runGrant: string;
  timeoutSeconds: number;
  invocationToken: string;
}

export interface AgentTextOutput {
}

export interface AgentStructuredOutput {
  schema: JsonObjectInput;
}

export type AgentOutput =
  | { text: AgentTextOutput; structured?: undefined }
  | { text?: undefined; structured: AgentStructuredOutput };

export interface GetAgentProviderTurnRequest {
  turnId: string;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface ListAgentProviderTurnsRequest {
  sessionId: string;
  subject?: Subject | undefined;
  turnIds: readonly string[];
  status: AgentExecutionStatus;
  limit: number;
  summaryOnly: boolean;
  invocationToken: string;
}

export interface ListAgentProviderTurnsResponse {
  turns: readonly AgentTurn[];
}

export interface CancelAgentProviderTurnRequest {
  turnId: string;
  reason: string;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface AgentTurnEvent {
  id?: string | undefined;
  turnId?: string | undefined;
  seq?: bigint | number | undefined;
  type?: string | undefined;
  source?: string | undefined;
  visibility?: string | undefined;
  data?: JsonObjectInput | undefined;
  createdAt?: Date | undefined;
  display?: AgentTurnDisplay | undefined;
}

export interface ListAgentProviderTurnEventsRequest {
  turnId: string;
  afterSeq: bigint;
  limit: number;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface ListAgentProviderTurnEventsResponse {
  events: readonly AgentTurnEvent[];
}

export interface AgentInteraction {
  id?: string | undefined;
  type?: AgentInteractionType | undefined;
  state?: AgentInteractionState | undefined;
  title?: string | undefined;
  prompt?: string | undefined;
  request?: JsonObjectInput | undefined;
  resolution?: JsonObjectInput | undefined;
  createdAt?: Date | undefined;
  resolvedAt?: Date | undefined;
  turnId?: string | undefined;
  sessionId?: string | undefined;
}

export interface GetAgentProviderInteractionRequest {
  interactionId: string;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface ListAgentProviderInteractionsRequest {
  turnId: string;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface ListAgentProviderInteractionsResponse {
  interactions: readonly AgentInteraction[];
}

export interface ResolveAgentProviderInteractionRequest {
  interactionId: string;
  resolution?: JsonObjectInput | undefined;
  subject?: Subject | undefined;
  invocationToken: string;
}

export interface GetAgentProviderCapabilitiesRequest {}

export interface ExecuteAgentToolRequest {
  sessionId: string;
  turnId: string;
  toolCallId: string;
  toolId: string;
  arguments?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  runGrant?: string | undefined;
}

export interface ExecuteAgentToolResponse {
  status: number;
  body: string;
}

export interface AgentToolAnnotations {
  readOnlyHint?: boolean | undefined;
  idempotentHint?: boolean | undefined;
  destructiveHint?: boolean | undefined;
  openWorldHint?: boolean | undefined;
}

export interface ListedAgentTool {
  id: string;
  mcpName: string;
  title: string;
  description: string;
  inputSchema: string;
  outputSchema: string;
  annotations?: AgentToolAnnotations | undefined;
  ref?: AgentToolRef | undefined;
  tags: readonly string[];
  searchText: string;
}

export interface ListAgentToolsRequest {
  sessionId: string;
  turnId: string;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  runGrant?: string | undefined;
  query?: string | undefined;
}

export interface ListAgentToolsResponse {
  tools: readonly ListedAgentTool[];
  nextPageToken: string;
}

export interface ResolveAgentConnectionRequest {
  sessionId: string;
  turnId: string;
  connection: string;
  instance?: string | undefined;
  runGrant?: string | undefined;
}

export interface ResolvedAgentConnection {
  connectionId: string;
  connection: string;
  instance: string;
  mode: string;
  headers: Record<string, string>;
  params: Record<string, string>;
  expiresAt?: Date | undefined;
}
/** Plain-object input for listing tools available to one agent turn. */
export interface AgentHostListToolsInput {
  sessionId: string;
  turnId: string;
  runGrant?: string | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  query?: string | undefined;
}

/** Plain-object input for executing a tool during one agent turn. */
export interface AgentHostExecuteToolInput {
  sessionId: string;
  turnId: string;
  toolCallId: string;
  toolId: string;
  arguments?: JsonObjectInput | undefined;
  runGrant?: string | undefined;
  idempotencyKey?: string | undefined;
}

/** Plain-object input for resolving a configured connection during one turn. */
export interface AgentHostResolveConnectionInput {
  sessionId: string;
  turnId: string;
  connection: string;
  instance?: string | undefined;
  runGrant?: string | undefined;
}

/** Fakeable client contract for agent host calls. */
export interface AgentHost {
  executeTool(
    request: ExecuteAgentToolRequest,
  ): Promise<ExecuteAgentToolResponse>;
  executeToolForTurn(
    input: AgentHostExecuteToolInput,
  ): Promise<ExecuteAgentToolResponse>;
  listTools(request: ListAgentToolsRequest): Promise<ListAgentToolsResponse>;
  listToolsForTurn(
    input: AgentHostListToolsInput,
  ): Promise<ListAgentToolsResponse>;
  resolveConnection(
    request: ResolveAgentConnectionRequest,
  ): Promise<ResolvedAgentConnection>;
  resolveConnectionForTurn(
    input: AgentHostResolveConnectionInput,
  ): Promise<ResolvedAgentConnection>;
}

/** Handlers and runtime metadata for an agent provider. */
export interface AgentProviderOptions extends ProviderBaseOptions {
  createSession?: (
    request: CreateAgentProviderSessionRequest,
  ) => MaybePromise<AgentSession>;
  getSession?: (
    request: GetAgentProviderSessionRequest,
  ) => MaybePromise<AgentSession>;
  listSessions?: (
    request: ListAgentProviderSessionsRequest,
  ) => MaybePromise<readonly AgentSession[] | ListAgentProviderSessionsResponse>;
  updateSession?: (
    request: UpdateAgentProviderSessionRequest,
  ) => MaybePromise<AgentSession>;
  createTurn?: (
    request: CreateAgentProviderTurnRequest,
  ) => MaybePromise<AgentTurn>;
  getTurn?: (
    request: GetAgentProviderTurnRequest,
  ) => MaybePromise<AgentTurn>;
  listTurns?: (
    request: ListAgentProviderTurnsRequest,
  ) => MaybePromise<readonly AgentTurn[] | ListAgentProviderTurnsResponse>;
  cancelTurn?: (
    request: CancelAgentProviderTurnRequest,
  ) => MaybePromise<AgentTurn>;
  listTurnEvents?: (
    request: ListAgentProviderTurnEventsRequest,
  ) => MaybePromise<readonly AgentTurnEvent[] | ListAgentProviderTurnEventsResponse>;
  getInteraction?: (
    request: GetAgentProviderInteractionRequest,
  ) => MaybePromise<AgentInteraction>;
  listInteractions?: (
    request: ListAgentProviderInteractionsRequest,
  ) => MaybePromise<readonly AgentInteraction[] | ListAgentProviderInteractionsResponse>;
  resolveInteraction?: (
    request: ResolveAgentProviderInteractionRequest,
  ) => MaybePromise<AgentInteraction>;
  getCapabilities?: (
    request: GetAgentProviderCapabilitiesRequest,
  ) => MaybePromise<AgentProviderCapabilities>;
}

/** Runtime provider implementation for the Gestalt agent host contract. */
export class AgentProvider extends ProviderBase {
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
  ): Promise<AgentSession> {
    return await requireAgentProviderHandler(
      "create session",
      this.createSessionHandler,
      request,
    );
  }

  async getSession(
    request: GetAgentProviderSessionRequest,
  ): Promise<AgentSession> {
    return await requireAgentProviderHandler(
      "get session",
      this.getSessionHandler,
      request,
    );
  }

  async listSessions(
    request: ListAgentProviderSessionsRequest,
  ): Promise<readonly AgentSession[] | ListAgentProviderSessionsResponse> {
    return await requireAgentProviderHandler(
      "list sessions",
      this.listSessionsHandler,
      request,
    );
  }

  async updateSession(
    request: UpdateAgentProviderSessionRequest,
  ): Promise<AgentSession> {
    return await requireAgentProviderHandler(
      "update session",
      this.updateSessionHandler,
      request,
    );
  }

  async createTurn(
    request: CreateAgentProviderTurnRequest,
  ): Promise<AgentTurn> {
    return await requireAgentProviderHandler(
      "create turn",
      this.createTurnHandler,
      request,
    );
  }

  async getTurn(
    request: GetAgentProviderTurnRequest,
  ): Promise<AgentTurn> {
    return await requireAgentProviderHandler(
      "get turn",
      this.getTurnHandler,
      request,
    );
  }

  async listTurns(
    request: ListAgentProviderTurnsRequest,
  ): Promise<readonly AgentTurn[] | ListAgentProviderTurnsResponse> {
    return await requireAgentProviderHandler(
      "list turns",
      this.listTurnsHandler,
      request,
    );
  }

  async cancelTurn(
    request: CancelAgentProviderTurnRequest,
  ): Promise<AgentTurn> {
    return await requireAgentProviderHandler(
      "cancel turn",
      this.cancelTurnHandler,
      request,
    );
  }

  async listTurnEvents(
    request: ListAgentProviderTurnEventsRequest,
  ): Promise<readonly AgentTurnEvent[] | ListAgentProviderTurnEventsResponse> {
    return await requireAgentProviderHandler(
      "list turn events",
      this.listTurnEventsHandler,
      request,
    );
  }

  async getInteraction(
    request: GetAgentProviderInteractionRequest,
  ): Promise<AgentInteraction> {
    return await requireAgentProviderHandler(
      "get interaction",
      this.getInteractionHandler,
      request,
    );
  }

  async listInteractions(
    request: ListAgentProviderInteractionsRequest,
  ): Promise<readonly AgentInteraction[] | ListAgentProviderInteractionsResponse> {
    return await requireAgentProviderHandler(
      "list interactions",
      this.listInteractionsHandler,
      request,
    );
  }

  async resolveInteraction(
    request: ResolveAgentProviderInteractionRequest,
  ): Promise<AgentInteraction> {
    return await requireAgentProviderHandler(
      "resolve interaction",
      this.resolveInteractionHandler,
      request,
    );
  }

  async getCapabilities(
    request: GetAgentProviderCapabilitiesRequest = {},
  ): Promise<AgentProviderCapabilities> {
    return await requireAgentProviderHandler(
      "get capabilities",
      this.getCapabilitiesHandler,
      request,
    );
  }
}

/** Creates an agent provider for export from a provider module. */
export function defineAgentProvider(options: AgentProviderOptions): AgentProvider {
  return new AgentProvider(options);
}

function createAgentProviderSessionRequestFromProto(
  request: ProtoCreateAgentProviderSessionRequest,
): CreateAgentProviderSessionRequest {
  return {
    sessionId: request.sessionId,
    idempotencyKey: request.idempotencyKey,
    model: request.model,
    clientRef: request.clientRef,
    metadata: optionalObjectFromStruct(request.metadata),
    createdBy: agentActorFromProto(request.createdBy),
    subject: agentSubjectFromProto(request.subject),
    sessionStart: request.sessionStart === undefined ? undefined : {
      hooks: request.sessionStart.hooks.map((hook) => ({
        id: hook.id,
        type: hook.type,
        command: [...hook.command],
        cwd: hook.cwd,
        timeout: hook.timeout,
        env: { ...hook.env },
        output: hook.output === undefined ? undefined : {
          additionalContext: hook.output.additionalContext,
          metadata: hook.output.metadata,
        },
      })),
    },
    preparedWorkspace: request.preparedWorkspace === undefined ? undefined : {
      root: request.preparedWorkspace.root,
      cwd: request.preparedWorkspace.cwd,
    },
    invocationToken: request.invocationToken,
  };
}

function getAgentProviderSessionRequestFromProto(
  request: ProtoGetAgentProviderSessionRequest,
): GetAgentProviderSessionRequest {
  return {
    sessionId: request.sessionId,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function listAgentProviderSessionsRequestFromProto(
  request: ProtoListAgentProviderSessionsRequest,
): ListAgentProviderSessionsRequest {
  return {
    subject: agentSubjectFromProto(request.subject),
    sessionIds: [...request.sessionIds],
    state: request.state as AgentSessionState,
    limit: request.limit,
    summaryOnly: request.summaryOnly,
    invocationToken: request.invocationToken,
  };
}

function updateAgentProviderSessionRequestFromProto(
  request: ProtoUpdateAgentProviderSessionRequest,
): UpdateAgentProviderSessionRequest {
  return {
    sessionId: request.sessionId,
    clientRef: request.clientRef,
    state: request.state as AgentSessionState,
    metadata: optionalObjectFromStruct(request.metadata),
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function createAgentProviderTurnRequestFromProto(
  request: ProtoCreateAgentProviderTurnRequest,
): CreateAgentProviderTurnRequest {
  let output: AgentOutput | undefined;
  try {
    output = agentOutputFromProto(request.output);
  } catch (error) {
    throw new ConnectError(errorMessage(error), Code.InvalidArgument);
  }
  if (output === undefined) {
    throw new ConnectError("create turn output is required", Code.InvalidArgument);
  }
  return {
    turnId: request.turnId,
    sessionId: request.sessionId,
    idempotencyKey: request.idempotencyKey,
    model: request.model,
    messages: request.messages.map(agentMessageFromProto),
    tools: request.tools.map(resolvedAgentToolFromProto),
    output,
    metadata: optionalObjectFromStruct(request.metadata),
    createdBy: agentActorFromProto(request.createdBy),
    executionRef: request.executionRef,
    toolRefs: request.toolRefs.map(agentToolRefFromProto),
    toolSource: request.toolSource as AgentToolSourceMode,
    subject: agentSubjectFromProto(request.subject),
    modelOptions: optionalObjectFromStruct(request.modelOptions),
    runGrant: request.runGrant,
    timeoutSeconds: request.timeoutSeconds,
    invocationToken: request.invocationToken,
  };
}

function getAgentProviderTurnRequestFromProto(
  request: ProtoGetAgentProviderTurnRequest,
): GetAgentProviderTurnRequest {
  return {
    turnId: request.turnId,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function listAgentProviderTurnsRequestFromProto(
  request: ProtoListAgentProviderTurnsRequest,
): ListAgentProviderTurnsRequest {
  return {
    sessionId: request.sessionId,
    subject: agentSubjectFromProto(request.subject),
    turnIds: [...request.turnIds],
    status: request.status as AgentExecutionStatus,
    limit: request.limit,
    summaryOnly: request.summaryOnly,
    invocationToken: request.invocationToken,
  };
}

function cancelAgentProviderTurnRequestFromProto(
  request: ProtoCancelAgentProviderTurnRequest,
): CancelAgentProviderTurnRequest {
  return {
    turnId: request.turnId,
    reason: request.reason,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function listAgentProviderTurnEventsRequestFromProto(
  request: ProtoListAgentProviderTurnEventsRequest,
): ListAgentProviderTurnEventsRequest {
  return {
    turnId: request.turnId,
    afterSeq: request.afterSeq,
    limit: request.limit,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function getAgentProviderInteractionRequestFromProto(
  request: ProtoGetAgentProviderInteractionRequest,
): GetAgentProviderInteractionRequest {
  return {
    interactionId: request.interactionId,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function listAgentProviderInteractionsRequestFromProto(
  request: ProtoListAgentProviderInteractionsRequest,
): ListAgentProviderInteractionsRequest {
  return {
    turnId: request.turnId,
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function resolveAgentProviderInteractionRequestFromProto(
  request: ProtoResolveAgentProviderInteractionRequest,
): ResolveAgentProviderInteractionRequest {
  return {
    interactionId: request.interactionId,
    resolution: optionalObjectFromStruct(request.resolution),
    subject: agentSubjectFromProto(request.subject),
    invocationToken: request.invocationToken,
  };
}

function agentSessionToProto(
  session: AgentSession,
): MessageInitShape<typeof AgentSessionSchema> {
  return {
    id: session.id ?? "",
    providerName: session.providerName ?? "",
    model: session.model ?? "",
    clientRef: session.clientRef ?? "",
    state: session.state ?? AgentSessionState.UNSPECIFIED,
    metadata: optionalStruct(session.metadata),
    createdBy: agentActorToProto(session.createdBy),
    createdAt: optionalTimestamp(session.createdAt),
    updatedAt: optionalTimestamp(session.updatedAt),
    lastTurnAt: optionalTimestamp(session.lastTurnAt),
  };
}

function agentTurnToProto(turn: AgentTurn): MessageInitShape<typeof AgentTurnSchema> {
  return {
    id: turn.id ?? "",
    sessionId: turn.sessionId ?? "",
    providerName: turn.providerName ?? "",
    model: turn.model ?? "",
    status: turn.status ?? AgentExecutionStatus.UNSPECIFIED,
    messages: turn.messages?.map(agentMessageToProto) ?? [],
    output: agentTurnOutputToProto(turn.output),
    statusMessage: turn.statusMessage ?? "",
    createdBy: agentActorToProto(turn.createdBy),
    createdAt: optionalTimestamp(turn.createdAt),
    startedAt: optionalTimestamp(turn.startedAt),
    completedAt: optionalTimestamp(turn.completedAt),
    executionRef: turn.executionRef ?? "",
  };
}

function agentTurnEventToProto(
  event: AgentTurnEvent,
): MessageInitShape<typeof AgentTurnEventSchema> {
  return {
    id: event.id ?? "",
    turnId: event.turnId ?? "",
    seq: typeof event.seq === "number" ? BigInt(event.seq) : (event.seq ?? 0n),
    type: event.type ?? "",
    source: event.source ?? "",
    visibility: event.visibility ?? "",
    data: optionalStruct(event.data),
    createdAt: optionalTimestamp(event.createdAt),
    display: agentTurnDisplayToProto(event.display),
  };
}

function agentInteractionToProto(
  interaction: AgentInteraction,
): MessageInitShape<typeof AgentInteractionSchema> {
  return {
    id: interaction.id ?? "",
    type: interaction.type ?? AgentInteractionType.UNSPECIFIED,
    state: interaction.state ?? AgentInteractionState.UNSPECIFIED,
    title: interaction.title ?? "",
    prompt: interaction.prompt ?? "",
    request: optionalStruct(interaction.request),
    resolution: optionalStruct(interaction.resolution),
    createdAt: optionalTimestamp(interaction.createdAt),
    resolvedAt: optionalTimestamp(interaction.resolvedAt),
    turnId: interaction.turnId ?? "",
    sessionId: interaction.sessionId ?? "",
  };
}

function capabilitiesToProto(
  capabilities: AgentProviderCapabilities,
): MessageInitShape<typeof AgentProviderCapabilitiesSchema> {
  return {
    streamingText: capabilities.streamingText ?? false,
    toolCalls: capabilities.toolCalls ?? false,
    parallelToolCalls: capabilities.parallelToolCalls ?? false,
    interactions: capabilities.interactions ?? false,
    resumableTurns: capabilities.resumableTurns ?? false,
    reasoningSummaries: capabilities.reasoningSummaries ?? false,
    boundedListHydration: capabilities.boundedListHydration ?? false,
    supportedToolSources: [...(capabilities.supportedToolSources ?? [])],
    supportsSessionStart: capabilities.supportsSessionStart ?? false,
    supportsPreparedWorkspace: capabilities.supportsPreparedWorkspace ?? false,
  };
}

function resolvedAgentToolFromProto(tool: ProtoResolvedAgentTool): ResolvedAgentTool {
  return {
    id: tool.id,
    name: tool.name,
    description: tool.description,
    parametersSchema: optionalObjectFromStruct(tool.parametersSchema),
  };
}

function agentSubjectFromProto(
  subject?: ProtoSubjectContext | undefined,
): Subject | undefined {
  if (subject === undefined) {
    return undefined;
  }
  return {
    id: subject.id,
    kind: subject.kind,
    credentialSubjectId: subject.credentialSubjectId,
    displayName: subject.displayName,
    authSource: subject.authSource,
    email: subject.email,
  };
}

function optionalTimestamp(value?: Date | undefined) {
  return value === undefined ? undefined : timestampFromDate(value);
}

function executeToolRequestToProto(
  request: ExecuteAgentToolRequest,
): ProtoExecuteAgentToolRequest {
  return create(ExecuteAgentToolRequestSchema, {
    sessionId: request.sessionId,
    turnId: request.turnId,
    toolCallId: request.toolCallId,
    toolId: request.toolId,
    arguments: optionalStruct(request.arguments),
    idempotencyKey: request.idempotencyKey ?? "",
    runGrant: request.runGrant ?? "",
  });
}

function executeToolResponseFromProto(
  response: ProtoExecuteAgentToolResponse,
): ExecuteAgentToolResponse {
  return {
    status: response.status,
    body: response.body,
  };
}

function listToolsRequestToProto(request: ListAgentToolsRequest): ProtoListAgentToolsRequest {
  return create(ListAgentToolsRequestSchema, {
    sessionId: request.sessionId,
    turnId: request.turnId,
    pageSize: request.pageSize ?? 0,
    pageToken: request.pageToken ?? "",
    runGrant: request.runGrant ?? "",
    query: request.query ?? "",
  });
}

function listToolsResponseFromProto(
  response: ProtoListAgentToolsResponse,
): ListAgentToolsResponse {
  return {
    tools: response.tools.map(listedToolFromProto),
    nextPageToken: response.nextPageToken,
  };
}

function listedToolFromProto(tool: ProtoListedAgentTool): ListedAgentTool {
  return {
    id: tool.id,
    mcpName: tool.mcpName,
    title: tool.title,
    description: tool.description,
    inputSchema: tool.inputSchema,
    outputSchema: tool.outputSchema,
    annotations: tool.annotations === undefined ? undefined : {
      readOnlyHint: tool.annotations.readOnlyHint,
      idempotentHint: tool.annotations.idempotentHint,
      destructiveHint: tool.annotations.destructiveHint,
      openWorldHint: tool.annotations.openWorldHint,
    },
    ref: tool.ref === undefined ? undefined : agentToolRefFromProto(tool.ref),
    tags: [...tool.tags],
    searchText: tool.searchText,
  };
}

function resolveConnectionRequestToProto(
  request: ResolveAgentConnectionRequest,
): ProtoResolveAgentConnectionRequest {
  return create(ResolveAgentConnectionRequestSchema, {
    sessionId: request.sessionId,
    turnId: request.turnId,
    connection: request.connection,
    instance: request.instance ?? "",
    runGrant: request.runGrant ?? "",
  });
}

function resolvedConnectionFromProto(
  connection: ProtoResolvedAgentConnection,
): ResolvedAgentConnection {
  return {
    connectionId: connection.connectionId,
    connection: connection.connection,
    instance: connection.instance,
    mode: connection.mode,
    headers: { ...connection.headers },
    params: { ...connection.params },
    expiresAt: connection.expiresAt === undefined
      ? undefined
      : dateFromTimestamp(connection.expiresAt),
  };
}

/** Runtime type guard for agent providers loaded from user modules. */
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

/** Client for the agent host service available inside agent providers. */
class AgentHostImpl implements AgentHost {
  private readonly client: Client<typeof AgentHostService>;

  constructor() {
    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(`agent host: ${ENV_HOST_SERVICE_SOCKET} is not set`);
    }
    const relayToken = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("agent host", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(AgentHostService, transport);
  }

  /** Executes a host tool using an agent protocol request message. */
  async executeTool(
    request: ExecuteAgentToolRequest,
  ): Promise<ExecuteAgentToolResponse> {
    return executeToolResponseFromProto(
      await this.client.executeTool(executeToolRequestToProto(request)),
    );
  }

  /** Executes a host tool using plain TypeScript request fields. */
  async executeToolForTurn(
    input: AgentHostExecuteToolInput,
  ): Promise<ExecuteAgentToolResponse> {
    return await this.executeTool(
      {
        sessionId: input.sessionId,
        turnId: input.turnId,
        toolCallId: input.toolCallId,
        toolId: input.toolId,
        arguments: input.arguments,
        runGrant: input.runGrant ?? "",
        idempotencyKey: input.idempotencyKey ?? "",
      },
    );
  }

  /** Lists host tools visible to the current agent request. */
  async listTools(
    request: ListAgentToolsRequest,
  ): Promise<ListAgentToolsResponse> {
    return listToolsResponseFromProto(
      await this.client.listTools(listToolsRequestToProto(request)),
    );
  }

  /** Lists host tools using plain TypeScript request fields. */
  async listToolsForTurn(
    input: AgentHostListToolsInput,
  ): Promise<ListAgentToolsResponse> {
    return await this.listTools(
      {
        sessionId: input.sessionId,
        turnId: input.turnId,
        runGrant: input.runGrant ?? "",
        pageSize: input.pageSize ?? 0,
        pageToken: input.pageToken ?? "",
        query: input.query ?? "",
      },
    );
  }

  /** Resolves a configured agent connection for the current turn. */
  async resolveConnection(
    request: ResolveAgentConnectionRequest,
  ): Promise<ResolvedAgentConnection> {
    return resolvedConnectionFromProto(
      await this.client.resolveConnection(resolveConnectionRequestToProto(request)),
    );
  }

  /** Resolves an agent connection using plain TypeScript request fields. */
  async resolveConnectionForTurn(
    input: AgentHostResolveConnectionInput,
  ): Promise<ResolvedAgentConnection> {
    return await this.resolveConnection(
      {
        sessionId: input.sessionId,
        turnId: input.turnId,
        connection: input.connection,
        instance: input.instance ?? "",
        runGrant: input.runGrant ?? "",
      },
    );
  }
}

/** Builds the Connect service implementation used by the TypeScript runtime. */
export function createAgentProviderService(
  provider: AgentProvider,
): Partial<ServiceImpl<typeof AgentProviderService>> {
  return {
    async createSession(request) {
      return create(
        AgentSessionSchema,
        agentSessionToProto(
          await invokeAgentProvider("create session", () =>
            provider.createSession(createAgentProviderSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async getSession(request) {
      return create(
        AgentSessionSchema,
        agentSessionToProto(
          await invokeAgentProvider("get session", () =>
            provider.getSession(getAgentProviderSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async listSessions(request) {
      const response = await invokeAgentProvider("list sessions", () =>
        provider.listSessions(listAgentProviderSessionsRequestFromProto(request)),
      );
      return create(ListAgentProviderSessionsResponseSchema, {
        sessions: listSessionsResult(response).map(agentSessionToProto),
      });
    },
    async updateSession(request) {
      return create(
        AgentSessionSchema,
        agentSessionToProto(
          await invokeAgentProvider("update session", () =>
            provider.updateSession(updateAgentProviderSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async createTurn(request) {
      return create(
        AgentTurnSchema,
        agentTurnToProto(
          await invokeAgentProvider("create turn", () =>
            provider.createTurn(createAgentProviderTurnRequestFromProto(request)),
          ),
        ),
      );
    },
    async getTurn(request) {
      return create(
        AgentTurnSchema,
        agentTurnToProto(
          await invokeAgentProvider("get turn", () =>
            provider.getTurn(getAgentProviderTurnRequestFromProto(request)),
          ),
        ),
      );
    },
    async listTurns(request) {
      const response = await invokeAgentProvider("list turns", () =>
        provider.listTurns(listAgentProviderTurnsRequestFromProto(request)),
      );
      return create(ListAgentProviderTurnsResponseSchema, {
        turns: listTurnsResult(response).map(agentTurnToProto),
      });
    },
    async cancelTurn(request) {
      return create(
        AgentTurnSchema,
        agentTurnToProto(
          await invokeAgentProvider("cancel turn", () =>
            provider.cancelTurn(cancelAgentProviderTurnRequestFromProto(request)),
          ),
        ),
      );
    },
    async listTurnEvents(request) {
      const response = await invokeAgentProvider("list turn events", () =>
        provider.listTurnEvents(listAgentProviderTurnEventsRequestFromProto(request)),
      );
      return create(ListAgentProviderTurnEventsResponseSchema, {
        events: listTurnEventsResult(response).map(agentTurnEventToProto),
      });
    },
    async getInteraction(request) {
      return create(
        AgentInteractionSchema,
        agentInteractionToProto(
          await invokeAgentProvider("get interaction", () =>
            provider.getInteraction(getAgentProviderInteractionRequestFromProto(request)),
          ),
        ),
      );
    },
    async listInteractions(request) {
      const response = await invokeAgentProvider("list interactions", () =>
        provider.listInteractions(
          listAgentProviderInteractionsRequestFromProto(request),
        ),
      );
      return create(ListAgentProviderInteractionsResponseSchema, {
        interactions: listInteractionsResult(response).map(agentInteractionToProto),
      });
    },
    async resolveInteraction(request) {
      return create(
        AgentInteractionSchema,
        agentInteractionToProto(
          await invokeAgentProvider("resolve interaction", () =>
            provider.resolveInteraction(
              resolveAgentProviderInteractionRequestFromProto(request),
            ),
          ),
        ),
      );
    },
    async getCapabilities(request) {
      return create(
        AgentProviderCapabilitiesSchema,
        capabilitiesToProto(
          await invokeAgentProvider("get capabilities", () =>
            provider.getCapabilities({}),
          ),
        ),
      );
    },
  };
}

function listSessionsResult(
  value: readonly AgentSession[] | ListAgentProviderSessionsResponse,
): readonly AgentSession[] {
  return "sessions" in value ? value.sessions : value;
}

function listTurnsResult(
  value: readonly AgentTurn[] | ListAgentProviderTurnsResponse,
): readonly AgentTurn[] {
  return "turns" in value ? value.turns : value;
}

function listTurnEventsResult(
  value: readonly AgentTurnEvent[] | ListAgentProviderTurnEventsResponse,
): readonly AgentTurnEvent[] {
  return "events" in value ? value.events : value;
}

function listInteractionsResult(
  value: readonly AgentInteraction[] | ListAgentProviderInteractionsResponse,
): readonly AgentInteraction[] {
  return "interactions" in value ? value.interactions : value;
}

export const AgentHost = AgentHostImpl;

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
