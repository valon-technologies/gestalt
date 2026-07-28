export type AgentRunStatus =
  | "PENDING"
  | "RUNNING"
  | "WAITING_FOR_INPUT"
  | "SUCCEEDED"
  | "FAILED"
  | "CANCELED";

export type AgentRunEventType =
  | "turn_created"
  | "turn_started"
  | "text_delta"
  | "tool_call_requested"
  | "tool_call_completed"
  | "interaction_requested"
  | "interaction_resolved"
  | "history_compacted"
  | "turn_completed"
  | "turn_failed"
  | "turn_canceled";

export interface AgentToolRef {
  app?: string | undefined;
  operation?: string | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
  system?: string | undefined;
}

export interface AgentToolConfig {
  disabled?: boolean | undefined;
  refs?: readonly AgentToolRef[] | undefined;
}

export interface AgentSkillRef {
  marketplace?: string | undefined;
  package: string;
  skill: string;
  version?: string | undefined;
}

export interface AgentSkillConfig {
  refs?: readonly AgentSkillRef[] | undefined;
}

export interface AgentWorkspaceGitCheckout {
  url: string;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface AgentWorkspace {
  checkouts?: readonly AgentWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

export interface AgentConfig {
  providerName?: string | undefined;
  model?: string | undefined;
  instructions?: string | undefined;
  tools?: AgentToolConfig | undefined;
  skills?: AgentSkillConfig | undefined;
  workspace?: AgentWorkspace | undefined;
}

export interface AgentCreateInput extends AgentConfig {
  idempotencyKey?: string | undefined;
}

export type AgentInit = AgentCreateInput | { id: string };

export interface AgentToolUpdate {
  replace?: AgentToolConfig | undefined;
  add?: readonly AgentToolRef[] | undefined;
  remove?: readonly AgentToolRef[] | undefined;
}

export interface AgentSkillUpdate {
  replace?: AgentSkillConfig | undefined;
  add?: readonly AgentSkillRef[] | undefined;
  remove?: readonly AgentSkillRef[] | undefined;
}

export interface AgentConfigUpdate {
  model?: string | undefined;
  instructions?: string | undefined;
  tools?: AgentToolUpdate | undefined;
  skills?: AgentSkillUpdate | undefined;
  expectedRevision?: string | undefined;
  idempotencyKey?: string | undefined;
}

export interface AgentConfigRevision {
  id: string;
  parentRevision?: string | undefined;
  model?: string | undefined;
  instructions?: string | undefined;
  createdAt?: Date | undefined;
}

export interface AgentSendMessageOptions {
  idempotencyKey?: string | undefined;
}

export interface AgentTurn {
  id: string;
  agentId: string;
  configRevision: string;
  status: AgentRunStatus;
  statusMessage?: string | undefined;
  output?: {
    text: string;
    structured?: unknown;
  } | undefined;
  createdAt?: Date | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
}

export interface AgentTurnEvent {
  id: string;
  cursor: string;
  sequence: number;
  agentId: string;
  runId: string;
  type: AgentRunEventType;
  occurredAt?: Date | undefined;
  display?: {
    text?: string | undefined;
    label?: string | undefined;
    phase?: string | undefined;
  } | undefined;
  payloadRef?: string | undefined;
}

export interface AgentResult {
  turn: AgentTurn;
  text: string;
  structured?: unknown;
}

export type AgentInteractionResolution =
  | { decision: "approve" | "deny"; reason?: string | undefined }
  | { value: unknown };

export type AgentInteraction =
  & {
    id: string;
    agentId: string;
    runId: string;
    state: "PENDING" | "RESOLVED" | "CANCELED";
    title?: string | undefined;
    createdAt?: Date | undefined;
    resolvedAt?: Date | undefined;
  }
  & (
    | {
      kind: "approval";
      action: string;
      description?: string | undefined;
      argumentsSummary?: unknown;
    }
    | {
      kind: "input";
      prompt: string;
      input?: unknown;
    }
  );

export interface AgentRun extends AsyncIterable<AgentTurnEvent> {
  readonly id: string;
  readonly status: AgentRunStatus;
  readonly result: Promise<AgentResult>;

  cancel(reason?: string): Promise<void>;
  listPendingInteractions(): Promise<AgentInteraction[]>;
  respond(
    interactionId: string,
    resolution: AgentInteractionResolution,
  ): Promise<void>;
}

export interface Agent {
  readonly id: string;
  readonly configRevision: string;

  sendMessage(
    message: string,
    options?: AgentSendMessageOptions,
  ): Promise<AgentRun>;
  getRun(runId: string): Promise<AgentRun>;
  updateConfig(request: AgentConfigUpdate): Promise<AgentConfigRevision>;
}

export interface AgentClientOptions {
  baseUrl?: string | undefined;
  token?: string | undefined;
  headers?: Readonly<Record<string, string>> | undefined;
  fetch?: AgentFetch | undefined;
  pollIntervalMs?: number | undefined;
}

export type AgentFetch = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;

export class AgentClientError extends Error {
  readonly status: number;
  readonly code?: string | undefined;

  constructor(message: string, status: number, code?: string | undefined) {
    super(message);
    this.name = "AgentClientError";
    this.status = status;
    this.code = code;
  }
}

/**
 * Creates a durable agent conversation or resumes one by opaque ID.
 */
export async function Agent(
  input: AgentInit,
  options: AgentClientOptions = {},
): Promise<Agent> {
  const client = new AgentHTTPClient(options);
  if ("id" in input) {
    const resource = await client.getAgent(input.id);
    return new AgentHandle(client, resource);
  }
  const resource = await client.createAgent(input);
  return new AgentHandle(client, resource);
}

interface AgentResourceJSON {
  id?: string;
  configRevision?: string;
}

interface AgentRunJSON {
  id?: string;
  agentId?: string;
  configRevision?: string;
  status?: string;
  statusMessage?: string;
  output?: {
    text?: string;
    structured?: unknown;
  };
  createdAt?: string;
  startedAt?: string;
  completedAt?: string;
}

interface AgentRunEventJSON {
  id?: string;
  cursor?: string;
  sequence?: string | number;
  agentId?: string;
  runId?: string;
  type?: string;
  occurredAt?: string;
  display?: {
    text?: string;
    label?: string;
    phase?: string;
  };
  payloadRef?: string;
}

interface AgentInteractionJSON {
  id?: string;
  agentId?: string;
  runId?: string;
  kind?: string;
  state?: string;
  title?: string;
  approval?: {
    action?: string;
    description?: string;
    argumentsSummary?: unknown;
  };
  input?: {
    prompt?: string;
    input?: unknown;
  };
  createdAt?: string;
  resolvedAt?: string;
}

class AgentHandle implements Agent {
  readonly id: string;
  private revision: string;
  private readonly client: AgentHTTPClient;

  constructor(client: AgentHTTPClient, resource: AgentResourceJSON) {
    this.client = client;
    this.id = requiredString(resource.id, "agent id");
    this.revision = requiredString(
      resource.configRevision,
      "agent config revision",
    );
  }

  get configRevision(): string {
    return this.revision;
  }

  async sendMessage(
    message: string,
    options: AgentSendMessageOptions = {},
  ): Promise<AgentRun> {
    if (message.trim() === "") {
      throw new Error("agent message is required");
    }
    const resource = await this.client.request<AgentRunJSON>(
      "POST",
      `/api/v1/agents/${encodeURIComponent(this.id)}/runs`,
      {
        message,
        idempotencyKey: options.idempotencyKey,
      },
      idempotencyHeaders(options.idempotencyKey),
    );
    return new AgentRunHandle(this.client, parseTurn(resource));
  }

  async getRun(runId: string): Promise<AgentRun> {
    const resource = await this.client.getRun(this.id, runId);
    return new AgentRunHandle(this.client, resource);
  }

  async updateConfig(
    request: AgentConfigUpdate,
  ): Promise<AgentConfigRevision> {
    const expectedRevision =
      request.expectedRevision?.trim() || this.revision;
    const revision = await this.client.request<{
      id?: string;
      parentRevision?: string;
      model?: string;
      instructions?: string;
      createdAt?: string;
    }>(
      "PATCH",
      `/api/v1/agents/${encodeURIComponent(this.id)}/config`,
      configUpdateBody(request, expectedRevision),
      {
        ...idempotencyHeaders(request.idempotencyKey),
        "If-Match": expectedRevision,
      },
    );
    const id = requiredString(revision.id, "config revision id");
    this.revision = id;
    return {
      id,
      parentRevision: optionalString(revision.parentRevision),
      model: optionalString(revision.model),
      instructions: optionalString(revision.instructions),
      createdAt: optionalDate(revision.createdAt),
    };
  }
}

class AgentRunHandle implements AgentRun {
  readonly id: string;
  private turn: AgentTurn;
  private readonly client: AgentHTTPClient;
  private resultPromise?: Promise<AgentResult> | undefined;

  constructor(client: AgentHTTPClient, turn: AgentTurn) {
    this.client = client;
    this.turn = turn;
    this.id = turn.id;
  }

  get status(): AgentRunStatus {
    return this.turn.status;
  }

  get result(): Promise<AgentResult> {
    this.resultPromise ??= this.waitForResult();
    return this.resultPromise;
  }

  async cancel(reason?: string): Promise<void> {
    this.turn = parseTurn(
      await this.client.request<AgentRunJSON>(
        "POST",
        this.runPath("/cancel"),
        { reason },
      ),
    );
  }

  async listPendingInteractions(): Promise<AgentInteraction[]> {
    const response = await this.client.request<{
      interactions?: AgentInteractionJSON[];
    }>(
      "GET",
      this.runPath("/interactions?state=pending"),
    );
    return (response.interactions ?? []).map(parseInteraction);
  }

  async respond(
    interactionId: string,
    resolution: AgentInteractionResolution,
  ): Promise<void> {
    await this.client.request(
      "POST",
      this.runPath(
        `/interactions/${encodeURIComponent(interactionId)}/resolve`,
      ),
      interactionResolutionBody(resolution),
    );
  }

  async *[Symbol.asyncIterator](): AsyncIterator<AgentTurnEvent> {
    let cursor = "";
    const seen = new Set<string>();
    while (true) {
      const query = cursor === ""
        ? ""
        : `?after=${encodeURIComponent(cursor)}`;
      const response = await this.client.request<{
        events?: AgentRunEventJSON[];
        nextCursor?: string;
      }>("GET", this.runPath(`/events${query}`));
      for (const rawEvent of response.events ?? []) {
        const event = parseEvent(rawEvent);
        cursor = event.cursor;
        if (seen.has(event.id)) {
          continue;
        }
        seen.add(event.id);
        yield event;
        if (terminalEvent(event.type)) {
          this.turn = await this.client.getRun(
            this.turn.agentId,
            this.turn.id,
          );
          return;
        }
      }
      if ((response.events ?? []).length === 0) {
        this.turn = await this.client.getRun(
          this.turn.agentId,
          this.turn.id,
        );
        if (terminalStatus(this.turn.status)) {
          return;
        }
        await delay(this.client.pollIntervalMs);
      }
    }
  }

  private async waitForResult(): Promise<AgentResult> {
    while (!terminalStatus(this.turn.status)) {
      this.turn = await this.client.getRun(this.turn.agentId, this.turn.id);
      if (!terminalStatus(this.turn.status)) {
        await delay(this.client.pollIntervalMs);
      }
    }
    if (this.turn.status !== "SUCCEEDED") {
      throw new AgentClientError(
        this.turn.statusMessage ??
          `agent run ended with status ${this.turn.status}`,
        409,
        this.turn.status,
      );
    }
    return {
      turn: this.turn,
      text: this.turn.output?.text ?? "",
      structured: this.turn.output?.structured,
    };
  }

  private runPath(suffix = ""): string {
    return `/api/v1/agents/${encodeURIComponent(this.turn.agentId)}/runs/${
      encodeURIComponent(this.turn.id)
    }${suffix}`;
  }
}

class AgentHTTPClient {
  readonly pollIntervalMs: number;
  private readonly baseUrl: string;
  private readonly headers: Readonly<Record<string, string>>;
  private readonly fetchImpl: AgentFetch;

  constructor(options: AgentClientOptions) {
    this.baseUrl = (
      options.baseUrl ??
      environmentValue("GESTALT_BASE_URL") ??
      "http://localhost:8080"
    ).replace(/\/+$/, "");
    this.fetchImpl = options.fetch ?? globalThis.fetch;
    if (typeof this.fetchImpl !== "function") {
      throw new Error("agent client requires fetch");
    }
    const token =
      options.token ?? environmentValue("GESTALT_API_TOKEN");
    this.headers = {
      ...options.headers,
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    };
    const pollIntervalMs = options.pollIntervalMs ?? 250;
    if (!Number.isFinite(pollIntervalMs) || pollIntervalMs < 0) {
      throw new Error("agent pollIntervalMs must be non-negative");
    }
    this.pollIntervalMs = pollIntervalMs;
  }

  async createAgent(input: AgentCreateInput): Promise<AgentResourceJSON> {
    return await this.request<AgentResourceJSON>(
      "POST",
      "/api/v1/agents",
      {
        config: {
          providerName: input.providerName,
          model: input.model,
          instructions: input.instructions,
          tools: input.tools,
          skills: input.skills,
          workspace: input.workspace,
        },
        idempotencyKey: input.idempotencyKey,
      },
      idempotencyHeaders(input.idempotencyKey),
    );
  }

  async getAgent(id: string): Promise<AgentResourceJSON> {
    return await this.request<AgentResourceJSON>(
      "GET",
      `/api/v1/agents/${encodeURIComponent(id)}`,
    );
  }

  async getRun(agentId: string, runId: string): Promise<AgentTurn> {
    return parseTurn(
      await this.request<AgentRunJSON>(
        "GET",
        `/api/v1/agents/${encodeURIComponent(agentId)}/runs/${
          encodeURIComponent(runId)
        }`,
      ),
    );
  }

  async request<T = unknown>(
    method: string,
    path: string,
    body?: unknown,
    additionalHeaders: Readonly<Record<string, string>> = {},
  ): Promise<T> {
    const init: RequestInit = {
      method,
      headers: {
        Accept: "application/json",
        ...this.headers,
        ...additionalHeaders,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
    };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }
    const response = await this.fetchImpl(this.baseUrl + path, init);
    const text = await response.text();
    const decoded = text === "" ? {} : safeJSON(text);
    if (!response.ok) {
      const error = isRecord(decoded) && typeof decoded.error === "string"
        ? decoded.error
        : `agent request failed with status ${response.status}`;
      const code = isRecord(decoded) && typeof decoded.code === "string"
        ? decoded.code
        : undefined;
      throw new AgentClientError(error, response.status, code);
    }
    return decoded as T;
  }
}

function configUpdateBody(
  request: AgentConfigUpdate,
  expectedRevision: string,
): unknown {
  return {
    expectedRevision,
    idempotencyKey: request.idempotencyKey,
    update: {
      model: request.model,
      instructions: request.instructions,
      tools: collectionUpdate(request.tools),
      skills: collectionUpdate(request.skills),
    },
  };
}

function collectionUpdate(
  update: AgentToolUpdate | AgentSkillUpdate | undefined,
): unknown {
  if (update === undefined) {
    return undefined;
  }
  if (update.replace !== undefined) {
    if ((update.add?.length ?? 0) > 0 || (update.remove?.length ?? 0) > 0) {
      throw new Error("replace cannot be combined with add or remove");
    }
    return {
      mode: "AGENT_CONFIG_COLLECTION_UPDATE_MODE_REPLACE",
      replace: update.replace,
    };
  }
  return {
    mode: "AGENT_CONFIG_COLLECTION_UPDATE_MODE_PATCH",
    add: update.add,
    remove: update.remove,
  };
}

function interactionResolutionBody(
  resolution: AgentInteractionResolution,
): unknown {
  if ("decision" in resolution) {
    return {
      resolution: {
        approval: {
          decision: resolution.decision === "approve"
            ? "AGENT_APPROVAL_DECISION_APPROVE"
            : "AGENT_APPROVAL_DECISION_DENY",
          reason: resolution.reason,
        },
      },
    };
  }
  return { resolution: { input: { value: resolution.value } } };
}

function parseTurn(raw: AgentRunJSON): AgentTurn {
  return {
    id: requiredString(raw.id, "run id"),
    agentId: requiredString(raw.agentId, "run agent id"),
    configRevision: requiredString(
      raw.configRevision,
      "run config revision",
    ),
    status: parseStatus(raw.status),
    statusMessage: optionalString(raw.statusMessage),
    output: raw.output === undefined
      ? undefined
      : {
        text: raw.output.text ?? "",
        structured: raw.output.structured,
      },
    createdAt: optionalDate(raw.createdAt),
    startedAt: optionalDate(raw.startedAt),
    completedAt: optionalDate(raw.completedAt),
  };
}

function parseEvent(raw: AgentRunEventJSON): AgentTurnEvent {
  const sequence = typeof raw.sequence === "string"
    ? Number.parseInt(raw.sequence, 10)
    : raw.sequence ?? 0;
  return {
    id: requiredString(raw.id, "event id"),
    cursor: requiredString(raw.cursor, "event cursor"),
    sequence,
    agentId: requiredString(raw.agentId, "event agent id"),
    runId: requiredString(raw.runId, "event run id"),
    type: parseEventType(raw.type),
    occurredAt: optionalDate(raw.occurredAt),
    display: raw.display,
    payloadRef: optionalString(raw.payloadRef),
  };
}

function parseInteraction(raw: AgentInteractionJSON): AgentInteraction {
  const base = {
    id: requiredString(raw.id, "interaction id"),
    agentId: requiredString(raw.agentId, "interaction agent id"),
    runId: requiredString(raw.runId, "interaction run id"),
    state: stripEnum(raw.state, "AGENT_INTERACTION_STATE_") as
      AgentInteraction["state"],
    title: optionalString(raw.title),
    createdAt: optionalDate(raw.createdAt),
    resolvedAt: optionalDate(raw.resolvedAt),
  };
  const kind = stripEnum(raw.kind, "AGENT_INTERACTION_KIND_").toLowerCase();
  if (kind === "approval") {
    return {
      ...base,
      kind,
      action: raw.approval?.action ?? "",
      description: optionalString(raw.approval?.description),
      argumentsSummary: raw.approval?.argumentsSummary,
    };
  }
  if (kind === "input") {
    return {
      ...base,
      kind,
      prompt: raw.input?.prompt ?? "",
      input: raw.input?.input,
    };
  }
  throw new Error(`unsupported agent interaction kind ${raw.kind ?? ""}`);
}

function parseStatus(value: string | undefined): AgentRunStatus {
  return stripEnum(value, "AGENT_EXECUTION_STATUS_") as AgentRunStatus;
}

function parseEventType(value: string | undefined): AgentRunEventType {
  return stripEnum(value, "AGENT_RUN_EVENT_TYPE_").toLowerCase() as
    AgentRunEventType;
}

function stripEnum(value: string | undefined, prefix: string): string {
  return requiredString(value, "enum value").replace(prefix, "");
}

function terminalStatus(status: AgentRunStatus): boolean {
  return status === "SUCCEEDED" || status === "FAILED" ||
    status === "CANCELED";
}

function terminalEvent(type: AgentRunEventType): boolean {
  return type === "turn_completed" || type === "turn_failed" ||
    type === "turn_canceled";
}

function idempotencyHeaders(
  value: string | undefined,
): Readonly<Record<string, string>> {
  return value?.trim()
    ? { "Idempotency-Key": value.trim() }
    : {};
}

function optionalString(value: string | undefined): string | undefined {
  return value?.trim() ? value : undefined;
}

function requiredString(
  value: string | undefined,
  label: string,
): string {
  if (!value?.trim()) {
    throw new Error(`agent response is missing ${label}`);
  }
  return value;
}

function optionalDate(value: string | undefined): Date | undefined {
  return value ? new Date(value) : undefined;
}

function environmentValue(name: string): string | undefined {
  if (typeof process === "undefined") {
    return undefined;
  }
  return process.env[name]?.trim() || undefined;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function safeJSON(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return {};
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null &&
    !Array.isArray(value);
}
