import type {
  AgentRunStatus as AgentRunStatusType,
  ExecuteAgentToolResponse,
  ResolvedAgentTool,
  StartAgentProviderRunRequest,
} from "../../../sdk/typescript/src/index.ts";

type GestaltSDK = typeof import("../../../sdk/typescript/src/index.ts");

const packageSDK = "@valon-technologies/gestalt";
const localSDK = "../../../sdk/typescript/src/index.ts";
const { AgentHost, AgentRunStatus, ENV_AGENT_HOST_SOCKET, defineAgentProvider } =
  await loadGestaltSDK();

async function loadGestaltSDK(): Promise<GestaltSDK> {
  const candidate = await import(packageSDK).catch(() => undefined);
  if (
    candidate &&
    "defineAgentProvider" in candidate &&
    "AgentHost" in candidate &&
    "AgentRunStatus" in candidate
  ) {
    return candidate as GestaltSDK;
  }
  return (await import(localSDK)) as GestaltSDK;
}

type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
type JsonObject = { [key: string]: JsonValue };

type OpenAIConfig = {
  apiKey: string;
  baseURL: string;
  model: string;
  timeoutMs: number;
  maxTurns: number;
  store?: boolean;
};

type OpenAITool = {
  type: "function";
  name: string;
  description?: string;
  parameters: JsonObject;
  strict: boolean;
};

type ToolMapping = {
  tool: ResolvedAgentTool;
  openAIName: string;
};

type OpenAIFunctionCall = {
  type: "function_call";
  id?: string;
  call_id: string;
  name: string;
  arguments: string;
  status?: string;
};

type OpenAIResponse = {
  id?: string;
  status?: string;
  output?: unknown[];
  output_text?: string;
  error?: {
    message?: string;
    code?: string;
  } | null;
};

type RunResult = {
  responseId: string;
  outputText: string;
  structuredOutput?: JsonObject;
};

type TimestampInit = {
  seconds: bigint;
  nanos: number;
};

type StoredRun = {
  id: string;
  providerName: string;
  model: string;
  status: AgentRunStatusType;
  messages: StartAgentProviderRunRequest["messages"];
  outputText: string;
  structuredOutput?: JsonObject;
  statusMessage: string;
  sessionRef: string;
  createdBy?: NonNullable<StartAgentProviderRunRequest["createdBy"]>;
  createdAt?: TimestampInit;
  startedAt?: TimestampInit;
  completedAt?: TimestampInit;
  executionRef: string;
};

const DEFAULT_CONFIG: OpenAIConfig = {
  apiKey: "",
  baseURL: "https://api.openai.com/v1",
  model: "gpt-5.4",
  timeoutMs: 120_000,
  maxTurns: 8,
};

let config: OpenAIConfig = { ...DEFAULT_CONFIG };
let host: AgentHost | undefined;

const runs = new Map<string, StoredRun>();
const controllers = new Map<string, AbortController>();
const sessions = new Map<string, string>();

export const provider = defineAgentProvider({
  name: "openai",
  displayName: "OpenAI Agent",
  description: "OpenAI Responses API-backed agent provider",
  version: "0.0.1-alpha.0",
  configure(_name, rawConfig) {
    config = parseConfig(rawConfig);
    host = undefined;
    runs.clear();
    controllers.clear();
    sessions.clear();
  },
  async startRun(request) {
    const runID = request.runId || `run-${crypto.randomUUID()}`;
    const startedAt = timestampNow();
    const controller = new AbortController();
    controllers.set(runID, controller);

    const running = createRun(request, {
      id: runID,
      status: AgentRunStatus.RUNNING,
      startedAt,
      statusMessage: "running",
    });
    runs.set(runID, running);

    await emitEvent(runID, "agent.run.started", {
      model: modelForRequest(request),
      messageCount: request.messages.length,
      toolCount: request.tools.length,
    });

    void finishRun(request, runID, startedAt, controller);
    return running;
  },
  async getRun(request) {
    return requireRun(request.runId);
  },
  async listRuns() {
    return [...runs.values()];
  },
  async cancelRun(request) {
    const run = requireRun(request.runId);
    const controller = controllers.get(request.runId);
    if (controller) {
      controller.abort();
    }
    if (isTerminal(run.status)) {
      return run;
    }
    const canceled = cleanOptional({
      id: run.id,
      providerName: run.providerName,
      model: run.model,
      status: AgentRunStatus.CANCELED,
      messages: run.messages,
      outputText: run.outputText,
      ...(run.structuredOutput ? { structuredOutput: run.structuredOutput } : {}),
      statusMessage: request.reason || "canceled",
      sessionRef: run.sessionRef,
      ...(run.createdBy ? { createdBy: run.createdBy } : {}),
      ...(run.createdAt ? { createdAt: run.createdAt } : {}),
      ...(run.startedAt ? { startedAt: run.startedAt } : {}),
      completedAt: timestampNow(),
      executionRef: run.executionRef,
    });
    runs.set(request.runId, canceled);
    await emitEvent(request.runId, "agent.run.canceled", {
      status: "canceled",
      reason: request.reason,
    });
    return canceled;
  },
});

async function finishRun(
  request: StartAgentProviderRunRequest,
  runID: string,
  startedAt: TimestampInit,
  controller: AbortController,
): Promise<void> {
  try {
    const result = await runResponsesLoop(request, runID, controller.signal);
    const completed = createRun(request, {
      id: runID,
      status: AgentRunStatus.SUCCEEDED,
      outputText: result.outputText,
      ...(result.structuredOutput ? { structuredOutput: result.structuredOutput } : {}),
      startedAt,
      completedAt: timestampNow(),
      statusMessage: "completed",
    });
    runs.set(runID, completed);
    await emitEvent(runID, "agent.run.completed", {
      responseId: result.responseId,
      status: "succeeded",
    });
  } catch (error) {
    const aborted = isAbortError(error);
    const failed = createRun(request, {
      id: runID,
      status: aborted ? AgentRunStatus.CANCELED : AgentRunStatus.FAILED,
      startedAt,
      completedAt: timestampNow(),
      statusMessage: aborted ? "canceled" : errorMessage(error),
    });
    runs.set(runID, failed);
    await emitEvent(
      runID,
      aborted ? "agent.run.canceled" : "agent.run.failed",
      aborted
        ? { status: "canceled" }
        : {
            status: "failed",
            error: errorMessage(error),
          },
    );
  } finally {
    controllers.delete(runID);
  }
}

async function runResponsesLoop(
  request: StartAgentProviderRunRequest,
  runID: string,
  signal: AbortSignal,
): Promise<RunResult> {
  const toolMappings = createToolMappings(request.tools);
  const tools = toolMappings.map((mapping) => openAITool(mapping));
  const toolByName = new Map(toolMappings.map((mapping) => [mapping.openAIName, mapping]));
  const basePayload = createBasePayload(request, tools);
  let payload: JsonObject = {
    ...basePayload,
    ...(request.sessionRef && sessions.has(request.sessionRef)
      ? { previous_response_id: sessions.get(request.sessionRef)! }
      : {}),
    input: request.messages.map(openAIMessage),
  };
  let response: OpenAIResponse | undefined;

  for (let turn = 0; turn < config.maxTurns; turn += 1) {
    response = await createResponse(payload, signal);
    await emitEvent(runID, "agent.model.response", {
      ...(response.id ? { responseId: response.id } : {}),
      ...(response.status ? { status: response.status } : {}),
      outputCount: Array.isArray(response.output) ? response.output.length : 0,
      turn: turn + 1,
    });

    const functionCalls = response.output?.filter(isFunctionCall) ?? [];
    if (functionCalls.length === 0) {
      const outputText = extractOutputText(response);
      if (request.sessionRef && response.id) {
        sessions.set(request.sessionRef, response.id);
      }
      const structuredOutput = parseStructuredOutput(request, outputText);
      return cleanOptional({
        responseId: response.id ?? "",
        outputText,
        structuredOutput,
      }) as RunResult;
    }

    const toolOutputs = [];
    for (const call of functionCalls) {
      const mapping = toolByName.get(call.name);
      if (!mapping) {
        throw new Error(`OpenAI requested unknown tool ${call.name}`);
      }
      const args = parseToolArguments(call);
      await emitEvent(runID, "agent.tool_call.started", {
        toolCallId: call.call_id,
        toolId: mapping.tool.id,
        name: mapping.openAIName,
      });

      let toolResponse: ExecuteAgentToolResponse;
      try {
        toolResponse = await agentHost().executeTool({
          runId: runID,
          toolCallId: call.call_id,
          toolId: mapping.tool.id,
          arguments: args,
        });
      } catch (error) {
        await emitEvent(runID, "agent.tool_call.failed", {
          toolCallId: call.call_id,
          toolId: mapping.tool.id,
          error: errorMessage(error),
        });
        throw error;
      }

      await emitEvent(runID, "agent.tool_call.completed", {
        toolCallId: call.call_id,
        toolId: mapping.tool.id,
        status: toolResponse.status,
      });
      toolOutputs.push({
        type: "function_call_output",
        call_id: call.call_id,
        output: toolOutputText(toolResponse),
      });
    }

    if (!response.id) {
      throw new Error("OpenAI response omitted id before tool continuation");
    }

    payload = {
      ...basePayload,
      previous_response_id: response.id,
      input: toolOutputs,
    };
  }

  throw new Error(`OpenAI response exceeded maxTurns=${config.maxTurns}`);
}

function createBasePayload(
  request: StartAgentProviderRunRequest,
  tools: OpenAITool[],
): JsonObject {
  const payload: JsonObject = {
    model: modelForRequest(request),
  };
  if (tools.length > 0) {
    payload.tools = tools;
    payload.tool_choice = "auto";
  }
  if (request.responseSchema && Object.keys(request.responseSchema).length > 0) {
    payload.text = {
      format: {
        type: "json_schema",
        name: "gestalt_agent_response",
        schema: request.responseSchema,
        strict: false,
      },
    };
  }
  if (config.store !== undefined) {
    payload.store = config.store;
  }
  applyProviderOptions(payload, request.providerOptions ?? {});
  return payload;
}

function applyProviderOptions(payload: JsonObject, options: JsonObject): void {
  for (const key of [
    "instructions",
    "max_output_tokens",
    "parallel_tool_calls",
    "reasoning",
    "temperature",
    "tool_choice",
    "top_p",
    "truncation",
    "user",
  ]) {
    const value = options[key];
    if (value !== undefined) {
      payload[key] = value;
    }
  }
  if (isObject(options.metadata)) {
    payload.metadata = options.metadata;
  }
  if (typeof options.store === "boolean") {
    payload.store = options.store;
  }
}

async function createResponse(payload: JsonObject, signal: AbortSignal): Promise<OpenAIResponse> {
  const timeout = AbortSignal.timeout(config.timeoutMs);
  const combinedSignal = AbortSignal.any([signal, timeout]);
  const response = await fetch(`${trimTrailingSlash(config.baseURL)}/responses`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${config.apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
    signal: combinedSignal,
  });

  const text = await response.text();
  const body = text ? tryParseJSON(text) : undefined;
  if (!response.ok) {
    const message =
      isObject(body) && isObject(body.error) && typeof body.error.message === "string"
        ? body.error.message
        : text;
    throw new Error(`OpenAI responses API returned ${response.status}: ${message}`);
  }
  if (!isObject(body)) {
    throw new Error("OpenAI responses API returned a non-object response");
  }
  if (isObject(body.error) && typeof body.error.message === "string") {
    throw new Error(`OpenAI responses API error: ${body.error.message}`);
  }
  return body as OpenAIResponse;
}

function createToolMappings(tools: ResolvedAgentTool[]): ToolMapping[] {
  const seen = new Set<string>();
  return tools.map((tool, index) => {
    const base = normalizeFunctionName(tool.name || tool.id || `tool_${index + 1}`);
    let openAIName = base;
    let suffix = 2;
    while (seen.has(openAIName)) {
      const nextSuffix = `_${suffix}`;
      openAIName = `${base.slice(0, 64 - nextSuffix.length)}${nextSuffix}`;
      suffix += 1;
    }
    seen.add(openAIName);
    return { tool, openAIName };
  });
}

function openAITool(mapping: ToolMapping): OpenAITool {
  const tool: OpenAITool = {
    type: "function",
    name: mapping.openAIName,
    parameters:
      mapping.tool.parametersSchema && Object.keys(mapping.tool.parametersSchema).length > 0
        ? mapping.tool.parametersSchema
        : { type: "object", properties: {}, additionalProperties: true },
    strict: false,
  };
  if (mapping.tool.description) {
    tool.description = mapping.tool.description;
  }
  return tool;
}

function openAIMessage(message: { role: string; text: string }): JsonObject {
  return {
    role: normalizeRole(message.role),
    content: message.text,
  };
}

function normalizeRole(role: string): string {
  switch (role) {
    case "system":
    case "developer":
    case "assistant":
    case "user":
      return role;
    default:
      return "user";
  }
}

function parseToolArguments(call: OpenAIFunctionCall): JsonObject {
  const parsed = parseJSON(call.arguments || "{}");
  if (!isObject(parsed)) {
    throw new Error(`OpenAI tool call ${call.call_id} arguments must be a JSON object`);
  }
  return parsed;
}

function isFunctionCall(item: unknown): item is OpenAIFunctionCall {
  return (
    isObject(item) &&
    item.type === "function_call" &&
    typeof item.call_id === "string" &&
    typeof item.name === "string" &&
    typeof item.arguments === "string"
  );
}

function extractOutputText(response: OpenAIResponse): string {
  if (typeof response.output_text === "string") {
    return response.output_text;
  }
  const chunks: string[] = [];
  for (const item of response.output ?? []) {
    if (!isObject(item) || item.type !== "message" || !Array.isArray(item.content)) {
      continue;
    }
    for (const content of item.content) {
      if (isObject(content) && content.type === "output_text" && typeof content.text === "string") {
        chunks.push(content.text);
      }
    }
  }
  return chunks.join("");
}

function parseStructuredOutput(
  request: StartAgentProviderRunRequest,
  outputText: string,
): JsonObject | undefined {
  if (!request.responseSchema || Object.keys(request.responseSchema).length === 0) {
    return undefined;
  }
  const parsed = parseJSON(outputText);
  if (!isObject(parsed)) {
    throw new Error("OpenAI structured output was not a JSON object");
  }
  return parsed;
}

function toolOutputText(response: ExecuteAgentToolResponse): string {
  if (response.status >= 200 && response.status < 300) {
    return response.body;
  }
  return JSON.stringify({
    status: response.status,
    body: response.body,
  });
}

function createRun(
  request: StartAgentProviderRunRequest,
  fields: Partial<StoredRun> & { id: string; status: AgentRunStatusType },
): StoredRun {
  return cleanOptional({
    id: fields.id,
    providerName: request.providerName || "openai",
    model: modelForRequest(request),
    status: fields.status,
    messages: [...request.messages],
    outputText: fields.outputText ?? "",
    structuredOutput: fields.structuredOutput,
    statusMessage: fields.statusMessage ?? "",
    sessionRef: request.sessionRef,
    createdBy: request.createdBy,
    createdAt: fields.createdAt ?? timestampNow(),
    startedAt: fields.startedAt,
    completedAt: fields.completedAt,
    executionRef: request.executionRef,
  }) as StoredRun;
}

function modelForRequest(request: StartAgentProviderRunRequest): string {
  return request.model || config.model;
}

function requireRun(runID: string): StoredRun {
  const run = runs.get(runID);
  if (!run) {
    throw new Error(`unknown run ${runID}`);
  }
  return run;
}

async function emitEvent(runID: string, type: string, data: JsonObject): Promise<void> {
  if (!process.env[ENV_AGENT_HOST_SOCKET]) {
    return;
  }
  const cleanData = cleanOptional(data);
  try {
    await agentHost().emitEvent({
      runId: runID,
      type,
      visibility: "public",
      data: cleanData,
    });
  } catch {
    // Event emission should never decide run success. Providers still return the
    // current run and let the host surface callback failures through host logs.
  }
}

function agentHost(): AgentHost {
  host ??= new AgentHost();
  return host;
}

function parseConfig(raw: Record<string, unknown>): OpenAIConfig {
  const apiKey = stringConfig(raw, "apiKey", process.env.OPENAI_API_KEY ?? "");
  if (!apiKey) {
    throw new Error("OpenAI agent provider config.apiKey or OPENAI_API_KEY is required");
  }
  const parsed: OpenAIConfig = {
    apiKey,
    baseURL: stringConfig(raw, "baseURL", DEFAULT_CONFIG.baseURL),
    model: stringConfig(raw, "model", DEFAULT_CONFIG.model),
    timeoutMs: numberConfig(raw, "timeoutMs", DEFAULT_CONFIG.timeoutMs),
    maxTurns: numberConfig(raw, "maxTurns", DEFAULT_CONFIG.maxTurns),
  };
  if (typeof raw.store === "boolean") {
    parsed.store = raw.store;
  }
  return parsed;
}

function stringConfig(raw: Record<string, unknown>, key: string, fallback: string): string {
  const value = raw[key];
  return typeof value === "string" && value.trim() ? value.trim() : fallback;
}

function numberConfig(raw: Record<string, unknown>, key: string, fallback: number): number {
  const value = raw[key];
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    return fallback;
  }
  return Math.trunc(value);
}

function normalizeFunctionName(value: string): string {
  const normalized = value.replace(/[^A-Za-z0-9_-]/g, "_").replace(/^_+|_+$/g, "");
  const prefixed = /^[A-Za-z0-9]/.test(normalized) ? normalized : `tool_${normalized}`;
  return (prefixed || "tool").slice(0, 64);
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

function timestampNow() {
  return {
    seconds: BigInt(Math.trunc(Date.now() / 1000)),
    nanos: 0,
  };
}

function parseJSON(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch (error) {
    throw new Error(`parse JSON: ${errorMessage(error)}`);
  }
}

function tryParseJSON(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function cleanOptional<T extends Record<string, unknown>>(value: T): T {
  return Object.fromEntries(
    Object.entries(value).filter((entry) => entry[1] !== undefined),
  ) as T;
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

function isTerminal(status: AgentRunStatusType): boolean {
  return (
    status === AgentRunStatus.SUCCEEDED ||
    status === AgentRunStatus.FAILED ||
    status === AgentRunStatus.CANCELED
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
