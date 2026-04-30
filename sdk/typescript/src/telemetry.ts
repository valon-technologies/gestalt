import {
  SpanKind,
  SpanStatusCode,
  ValueType,
  context,
  metrics,
  trace,
  type AttributeValue,
  type Attributes,
  type Exception,
  type Histogram,
  type Span,
} from "@opentelemetry/api";

/** OpenTelemetry instrumentation scope used by the Gestalt SDK. */
export const TELEMETRY_INSTRUMENTATION_NAME = "gestalt.provider";
/** Default GenAI provider name used for Gestalt-owned agent and tool work. */
export const GENAI_PROVIDER_NAME = "gestalt";

export const GENAI_OPERATION_CHAT = "chat";
export const GENAI_OPERATION_EXECUTE_TOOL = "execute_tool";
export const GENAI_OPERATION_INVOKE_AGENT = "invoke_agent";

export const GENAI_TOOL_TYPE_DATASTORE = "datastore";
export const GENAI_TOOL_TYPE_EXTENSION = "extension";

const OPERATION_DURATION_METRIC = "gen_ai.client.operation.duration";
const TOKEN_USAGE_METRIC = "gen_ai.client.token.usage";
const OPERATION_DURATION_BUCKETS = [
  0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24,
  20.48, 40.96, 81.92,
];
const TOKEN_USAGE_BUCKETS = [
  1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304,
  16777216, 67108864,
];

let telemetryInstruments:
  | { operationDuration: Histogram; tokenUsage: Histogram }
  | undefined;

function getTelemetryInstruments(): {
  operationDuration: Histogram;
  tokenUsage: Histogram;
} {
  if (telemetryInstruments === undefined) {
    const meter = metrics.getMeter(TELEMETRY_INSTRUMENTATION_NAME);
    telemetryInstruments = {
      operationDuration: meter.createHistogram(OPERATION_DURATION_METRIC, {
        description: "GenAI operation duration.",
        unit: "s",
        advice: { explicitBucketBoundaries: OPERATION_DURATION_BUCKETS },
      }),
      tokenUsage: meter.createHistogram(TOKEN_USAGE_METRIC, {
        description: "Number of input and output tokens used.",
        unit: "{token}",
        valueType: ValueType.INT,
        advice: { explicitBucketBoundaries: TOKEN_USAGE_BUCKETS },
      }),
    };
  }
  return telemetryInstruments;
}

/** Options for recording an upstream model SDK call. */
export interface ModelOperationOptions {
  providerName: string;
  requestModel: string;
  requestOptions?: Record<string, unknown>;
  requestAttributes?: Attributes;
}

/** Options for recording provider-owned agent turn execution. */
export interface AgentInvocationOptions {
  agentName: string;
  sessionId: string;
  turnId: string;
  model: string;
}

/** Options for recording provider-owned tool execution. */
export interface ToolExecutionOptions {
  toolName: string;
  toolCallId?: string;
  toolType?: string;
}

/** GenAI token usage recorded on spans and token usage metrics. */
export interface TokenUsage {
  inputTokens?: number;
  outputTokens?: number;
  cacheCreationInputTokens?: number;
  cacheReadInputTokens?: number;
  reasoningOutputTokens?: number;
}

/** Records a GenAI span plus operation duration and token usage metrics. */
export class GenAIOperation {
  readonly #span: Span;
  readonly #startedAt = performance.now();
  #metricAttributes: Attributes;
  #errorType = "";
  #ended = false;

  constructor(span: Span, metricAttributes: Attributes) {
    this.#span = span;
    this.#metricAttributes = cleanAttributes(metricAttributes);
  }

  /** Ends the span and records operation duration. */
  end(error?: unknown): void {
    if (this.#ended) {
      return;
    }
    if (error !== undefined && error !== null) {
      this.markError(errorType(error), errorMessage(error), error);
    }
    this.#ended = true;

    const metricAttributes = { ...this.#metricAttributes };
    if (this.#errorType !== "") {
      metricAttributes["error.type"] = this.#errorType;
    }
    getTelemetryInstruments().operationDuration.record(
      Math.max(0, (performance.now() - this.#startedAt) / 1000),
      metricAttributes,
    );
    this.#span.end();
  }

  /** Marks the operation span and duration metric as failed. */
  markError(
    errorTypeValue: string,
    description = "",
    exception?: unknown,
  ): void {
    this.#errorType = cleanString(errorTypeValue) || "_OTHER";
    this.setAttribute("error.type", this.#errorType);
    this.#metricAttributes["error.type"] = this.#errorType;
    if (exception !== undefined && exception !== null) {
      this.#span.recordException(exceptionForSpan(exception));
    }
    this.#span.setStatus({
      code: SpanStatusCode.ERROR,
      message: description || this.#errorType,
    });
  }

  /** Sets a span attribute when the value is valid for OpenTelemetry. */
  setAttribute(key: string, value: unknown): void {
    const attrValue = toAttributeValue(value);
    if (attrValue === undefined) {
      return;
    }
    this.#span.setAttribute(key, attrValue);
    if (key === "gen_ai.response.model") {
      this.#metricAttributes[key] = attrValue;
    }
  }

  /** Attaches common GenAI response metadata to the span. */
  setResponseMetadata(input: {
    id?: string;
    model?: string;
    finishReasons?: string[];
  }): void {
    this.setAttribute("gen_ai.response.id", input.id);
    this.setAttribute("gen_ai.response.model", input.model);
    if (input.finishReasons !== undefined) {
      this.setAttribute("gen_ai.response.finish_reasons", input.finishReasons);
    }
  }

  /** Records GenAI token usage on the span and token usage metric. */
  recordUsage(usage: TokenUsage): void {
    const inputTokens = tokenCount(usage.inputTokens);
    const outputTokens = tokenCount(usage.outputTokens);
    const cacheCreationInputTokens = tokenCount(usage.cacheCreationInputTokens);
    const cacheReadInputTokens = tokenCount(usage.cacheReadInputTokens);
    const reasoningOutputTokens = tokenCount(usage.reasoningOutputTokens);

    this.setAttribute("gen_ai.usage.input_tokens", inputTokens);
    this.setAttribute("gen_ai.usage.output_tokens", outputTokens);
    this.setAttribute(
      "gen_ai.usage.cache_creation.input_tokens",
      cacheCreationInputTokens,
    );
    this.setAttribute(
      "gen_ai.usage.cache_read.input_tokens",
      cacheReadInputTokens,
    );
    this.setAttribute(
      "gen_ai.usage.reasoning.output_tokens",
      reasoningOutputTokens,
    );

    this.#recordTokenUsage(inputTokens, "input");
    this.#recordTokenUsage(outputTokens, "output");
  }

  #recordTokenUsage(tokens: number | undefined, tokenType: string): void {
    if (tokens === undefined) {
      return;
    }
    getTelemetryInstruments().tokenUsage.record(tokens, {
      ...this.#metricAttributes,
      "gen_ai.token.type": tokenType,
    });
  }
}

/** Runs a callback inside a GenAI model operation span. */
export async function withModelOperation<T>(
  options: ModelOperationOptions,
  callback: (operation: GenAIOperation) => T | Promise<T>,
): Promise<T> {
  const providerName = cleanString(options.providerName) || "_OTHER";
  const requestModel = cleanString(options.requestModel);
  const metricAttributes = cleanAttributes({
    "gen_ai.operation.name": GENAI_OPERATION_CHAT,
    "gen_ai.provider.name": providerName,
    "gen_ai.request.model": requestModel,
  });
  const spanAttributes = cleanAttributes({
    ...metricAttributes,
    ...requestOptionAttributes(options.requestOptions),
    ...options.requestAttributes,
  });
  return withOperation(
    spanName(GENAI_OPERATION_CHAT, requestModel),
    SpanKind.CLIENT,
    spanAttributes,
    metricAttributes,
    callback,
  );
}

/** Runs a callback inside a GenAI agent invocation span. */
export async function withAgentInvocation<T>(
  options: AgentInvocationOptions,
  callback: (operation: GenAIOperation) => T | Promise<T>,
): Promise<T> {
  const agentName = cleanString(options.agentName) || "provider";
  const model = cleanString(options.model);
  return withOperation(
    spanName(GENAI_OPERATION_INVOKE_AGENT, agentName),
    SpanKind.INTERNAL,
    cleanAttributes({
      "gen_ai.operation.name": GENAI_OPERATION_INVOKE_AGENT,
      "gen_ai.provider.name": GENAI_PROVIDER_NAME,
      "gen_ai.agent.name": agentName,
      "gen_ai.conversation.id": cleanString(options.sessionId),
      "gen_ai.request.model": model,
      "gestalt.agent.turn_id": cleanString(options.turnId),
    }),
    cleanAttributes({
      "gen_ai.operation.name": GENAI_OPERATION_INVOKE_AGENT,
      "gen_ai.provider.name": GENAI_PROVIDER_NAME,
      "gen_ai.agent.name": agentName,
      "gen_ai.request.model": model,
    }),
    callback,
  );
}

/** Runs a callback inside a GenAI tool execution span. */
export async function withToolExecution<T>(
  options: ToolExecutionOptions,
  callback: (operation: GenAIOperation) => T | Promise<T>,
): Promise<T> {
  const toolName = cleanString(options.toolName) || "_OTHER";
  const toolType = cleanString(options.toolType) || GENAI_TOOL_TYPE_EXTENSION;
  return withOperation(
    spanName(GENAI_OPERATION_EXECUTE_TOOL, toolName),
    SpanKind.INTERNAL,
    cleanAttributes({
      "gen_ai.operation.name": GENAI_OPERATION_EXECUTE_TOOL,
      "gen_ai.provider.name": GENAI_PROVIDER_NAME,
      "gen_ai.tool.name": toolName,
      "gen_ai.tool.call.id": cleanString(options.toolCallId),
      "gen_ai.tool.type": toolType,
    }),
    cleanAttributes({
      "gen_ai.operation.name": GENAI_OPERATION_EXECUTE_TOOL,
      "gen_ai.provider.name": GENAI_PROVIDER_NAME,
      "gen_ai.tool.name": toolName,
      "gen_ai.tool.type": toolType,
    }),
    callback,
  );
}

async function withOperation<T>(
  name: string,
  kind: SpanKind,
  spanAttributes: Attributes,
  metricAttributes: Attributes,
  callback: (operation: GenAIOperation) => T | Promise<T>,
): Promise<T> {
  const span = trace
    .getTracer(TELEMETRY_INSTRUMENTATION_NAME)
    .startSpan(name, { attributes: spanAttributes, kind });
  const operation = new GenAIOperation(span, metricAttributes);

  try {
    return await context.with(
      trace.setSpan(context.active(), span),
      async () => await callback(operation),
    );
  } catch (error) {
    operation.markError(errorType(error), errorMessage(error), error);
    throw error;
  } finally {
    operation.end();
  }
}

function requestOptionAttributes(
  options: Record<string, unknown> | undefined,
): Attributes {
  if (options === undefined) {
    return {};
  }
  const mapping: Record<string, string> = {
    choice_count: "gen_ai.request.choice.count",
    frequency_penalty: "gen_ai.request.frequency_penalty",
    max_completion_tokens: "gen_ai.request.max_tokens",
    max_output_tokens: "gen_ai.request.max_tokens",
    max_tokens: "gen_ai.request.max_tokens",
    n: "gen_ai.request.choice.count",
    presence_penalty: "gen_ai.request.presence_penalty",
    seed: "gen_ai.request.seed",
    temperature: "gen_ai.request.temperature",
    top_k: "gen_ai.request.top_k",
    top_p: "gen_ai.request.top_p",
  };
  const attributes: Attributes = {};
  for (const [optionName, attributeName] of Object.entries(mapping)) {
    const value = toAttributeValue(options[optionName]);
    if (value !== undefined) {
      attributes[attributeName] = value;
    }
  }
  return attributes;
}

function cleanAttributes(attributes: Attributes): Attributes {
  const cleaned: Attributes = {};
  for (const [key, value] of Object.entries(attributes)) {
    const attrValue = toAttributeValue(value);
    if (key.trim() !== "" && attrValue !== undefined) {
      cleaned[key] = attrValue;
    }
  }
  return cleaned;
}

function toAttributeValue(value: unknown): AttributeValue | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === "string") {
    const cleaned = cleanString(value);
    return cleaned === "" ? undefined : cleaned;
  }
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : undefined;
  }
  if (typeof value === "boolean") {
    return value;
  }
  if (Array.isArray(value)) {
    const strings = value.filter(
      (item): item is string => typeof item === "string",
    );
    if (strings.length === value.length && strings.length > 0) {
      return strings;
    }
    const numbers = value.filter(
      (item): item is number =>
        typeof item === "number" && Number.isFinite(item),
    );
    if (numbers.length === value.length && numbers.length > 0) {
      return numbers;
    }
    const booleans = value.filter(
      (item): item is boolean => typeof item === "boolean",
    );
    if (booleans.length === value.length && booleans.length > 0) {
      return booleans;
    }
    return undefined;
  }
  return String(value);
}

function tokenCount(value: number | undefined): number | undefined {
  if (value === undefined || !Number.isFinite(value) || value < 0) {
    return undefined;
  }
  return value;
}

function exceptionForSpan(error: unknown): Exception {
  return error instanceof Error ? error : String(error);
}

function errorType(error: unknown): string {
  if (error instanceof Error && cleanString(error.name) !== "") {
    return error.name;
  }
  return "_OTHER";
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function spanName(operation: string, subject: string): string {
  const cleaned = cleanString(subject);
  return cleaned === "" ? operation : `${operation} ${cleaned}`;
}

function cleanString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}
