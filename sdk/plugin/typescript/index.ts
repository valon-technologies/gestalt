declare const process: any;
declare const Buffer: any;

export const PROTOCOL_VERSION = "gestalt-plugin/1";

export type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue };

export type JsonObject = Record<string, unknown>;

export interface HostInfo {
  name: string;
  version: string;
}

export interface IntegrationInfo {
  name: string;
  config: JsonObject;
}

export interface ParameterDef {
  name: string;
  type: string;
  description?: string;
  required?: boolean;
  default?: unknown;
}

export interface OperationDef {
  name: string;
  description?: string;
  method?: string;
  parameters?: ParameterDef[];
}

export interface CatalogOperationDef {
  id: string;
  title?: string;
  description?: string;
  inputSchema?: JsonValue;
  parameters?: ParameterDef[];
  requiredScopes?: string[];
  tags?: string[];
  readOnly?: boolean;
  visible?: boolean;
  transport?: string;
  query?: string;
}

export interface CatalogDef {
  name: string;
  displayName?: string;
  description?: string;
  iconSvg?: string;
  baseUrl?: string;
  authStyle?: string;
  headers?: Record<string, string>;
  operations: CatalogOperationDef[];
}

export type ConnectionMode = "none" | "user" | "identity" | "either";

export interface ProviderManifest {
  displayName: string;
  description: string;
  connectionMode: ConnectionMode;
  operations: OperationDef[];
  catalog?: CatalogDef | JsonObject;
  auth?: JsonObject;
}

export interface PluginInfo {
  name: string;
  version: string;
}

export interface Capabilities {
  catalog?: boolean;
  oauth?: boolean;
  manualAuth?: boolean;
  cancellation?: boolean;
}

export interface InitializeRequest {
  protocolVersion: string;
  hostInfo: HostInfo;
  integration: IntegrationInfo;
}

export interface InitializeResult {
  protocolVersion: string;
  pluginInfo: PluginInfo;
  provider: ProviderManifest;
  capabilities?: Capabilities;
}

export interface ExecuteRequest {
  operation: string;
  params: JsonObject;
  token: string;
  meta?: JsonObject;
}

export interface ExecuteResult {
  status: number;
  body: string;
}

export interface AuthStartRequest {
  state: string;
  scopes: string[];
}

export interface AuthStartResult {
  authUrl: string;
  verifier?: string;
}

export interface AuthExchangeRequest {
  code: string;
  verifier?: string;
}

export interface TokenResult {
  accessToken: string;
  refreshToken?: string;
  expiresIn?: number;
  tokenType?: string;
}

export interface AuthRefreshRequest {
  refreshToken: string;
}

export interface PluginDefinition {
  pluginInfo: PluginInfo;
  provider: ProviderManifest;
  capabilities?: Capabilities;
  initialize?(request: InitializeRequest): Promise<InitializeResult> | InitializeResult;
  execute(request: ExecuteRequest): Promise<ExecuteResult> | ExecuteResult;
  auth?: {
    start?(request: AuthStartRequest): Promise<AuthStartResult> | AuthStartResult;
    exchangeCode?(request: AuthExchangeRequest): Promise<TokenResult> | TokenResult;
    refreshToken?(request: AuthRefreshRequest): Promise<TokenResult> | TokenResult;
  };
  onShutdown?(): Promise<void> | void;
}

export interface JsonRpcRequest {
  jsonrpc?: string;
  method: string;
  params?: unknown;
  id?: string | number | null;
}

export interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

export interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: string | number | null;
  result?: unknown;
  error?: JsonRpcError;
}

const JSONRPC_VERSION = "2.0";

const ERR_PARSE = -32700;
const ERR_INVALID_REQUEST = -32600;
const ERR_METHOD_NOT_FOUND = -32601;
const ERR_INVALID_PARAMS = -32602;
const ERR_INTERNAL = -32603;

class StdioJsonRpcReader {
  private buffer = Buffer.alloc(0);
  private queue: any[] = [];
  private waiters: Array<{
    resolve: (value: any | null) => void;
    reject: (reason: Error) => void;
  }> = [];
  private closed = false;
  private failure: Error | null = null;

  constructor(stream: any) {
    stream.on("data", (chunk: any) => {
      this.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
    });
    stream.on("end", () => this.finish());
    stream.on("close", () => this.finish());
    stream.on("error", (err: Error) => this.fail(err));
  }

  read(): Promise<any | null> {
    if (this.queue.length > 0) {
      return Promise.resolve(this.queue.shift() ?? null);
    }
    if (this.failure) {
      return Promise.reject(this.failure);
    }
    if (this.closed) {
      return Promise.resolve(null);
    }
    return new Promise((resolve, reject) => {
      this.waiters.push({ resolve, reject });
    });
  }

  private push(chunk: any): void {
    if (this.closed || this.failure) {
      return;
    }
    this.buffer = Buffer.concat([this.buffer, chunk]);
    this.pump();
  }

  private finish(): void {
    this.closed = true;
    this.drainWaiters(null);
  }

  private fail(err: Error): void {
    if (this.failure) {
      return;
    }
    this.failure = err;
    while (this.waiters.length > 0) {
      const waiter = this.waiters.shift();
      if (waiter) {
        waiter.reject(err);
      }
    }
  }

  private drainWaiters(value: any | null): void {
    while (this.waiters.length > 0 && this.queue.length > 0) {
      const waiter = this.waiters.shift();
      const item = this.queue.shift();
      if (waiter) {
        waiter.resolve(item ?? value);
      }
    }
    if (value === null && this.waiters.length > 0) {
      while (this.waiters.length > 0) {
        const waiter = this.waiters.shift();
        if (waiter) {
          waiter.resolve(null);
        }
      }
    }
  }

  private pump(): void {
    while (true) {
      const frame = this.nextFrame();
      if (frame === null) {
        return;
      }
      if (this.waiters.length > 0) {
        const waiter = this.waiters.shift();
        if (waiter) {
          waiter.resolve(frame);
        }
      } else {
        this.queue.push(frame);
      }
    }
  }

  private nextFrame(): any | null {
    const crlf = this.buffer.indexOf("\r\n\r\n");
    const lf = this.buffer.indexOf("\n\n");
    let boundary = -1;
    let separatorLength = 0;

    if (crlf >= 0 && (lf < 0 || crlf < lf)) {
      boundary = crlf;
      separatorLength = 4;
    } else if (lf >= 0) {
      boundary = lf;
      separatorLength = 2;
    }

    if (boundary < 0) {
      return null;
    }

    const headerText = this.buffer.slice(0, boundary).toString("utf8");
    const contentLength = parseContentLength(headerText);
    if (contentLength === null) {
      this.fail(new Error("missing Content-Length header"));
      return null;
    }

    const bodyStart = boundary + separatorLength;
    const bodyEnd = bodyStart + contentLength;
    if (this.buffer.length < bodyEnd) {
      return null;
    }

    const frame = this.buffer.slice(bodyStart, bodyEnd);
    this.buffer = this.buffer.slice(bodyEnd);
    return frame;
  }
}

function parseContentLength(headers: string): number | null {
  for (const line of headers.split(/\r?\n/)) {
    const match = /^Content-Length:\s*(\d+)$/i.exec(line.trim());
    if (match) {
      return Number.parseInt(match[1] ?? "", 10);
    }
  }
  return null;
}

async function writeFrame(stream: any, value: unknown): Promise<void> {
  const body = Buffer.from(JSON.stringify(value), "utf8");
  const header = Buffer.from(`Content-Length: ${body.length}\r\n\r\n`, "utf8");
  if (!stream.write(header)) {
    await onceDrain(stream);
  }
  if (!stream.write(body)) {
    await onceDrain(stream);
  }
}

function onceDrain(stream: any): Promise<void> {
  return new Promise((resolve, reject) => {
    const onDrain = () => {
      cleanup();
      resolve();
    };
    const onError = (err: Error) => {
      cleanup();
      reject(err);
    };
    const cleanup = () => {
      stream.off("drain", onDrain);
      stream.off("error", onError);
    };
    stream.once("drain", onDrain);
    stream.once("error", onError);
  });
}

function errorResponse(id: string | number | null, code: number, message: string, data?: unknown): JsonRpcResponse {
  return { jsonrpc: JSONRPC_VERSION, id, error: { code, message, data } };
}

function successResponse(id: string | number | null, result: unknown): JsonRpcResponse {
  return { jsonrpc: JSONRPC_VERSION, id, result };
}

async function maybeAsync<T>(value: T | Promise<T>): Promise<T> {
  return await value;
}

export class PluginRuntime {
  private readonly definition: PluginDefinition;
  private readonly input: any;
  private readonly output: any;
  private readonly stderr: any;

  constructor(
    definition: PluginDefinition,
    input: any = process.stdin,
    output: any = process.stdout,
    stderr: any = process.stderr,
  ) {
    this.definition = definition;
    this.input = input;
    this.output = output;
    this.stderr = stderr;
  }

  async serve(): Promise<void> {
    const reader = new StdioJsonRpcReader(this.input);
    while (true) {
      const raw = await reader.read();
      if (raw === null) {
        break;
      }

      let message: JsonRpcRequest;
      try {
        message = JSON.parse(raw.toString("utf8")) as JsonRpcRequest;
      } catch (error) {
        await this.reply(null, errorResponse(null, ERR_PARSE, "invalid JSON payload", String(error)));
        continue;
      }

      if (!message || typeof message.method !== "string") {
        await this.reply(message?.id ?? null, errorResponse(message?.id ?? null, ERR_INVALID_REQUEST, "invalid JSON-RPC request"));
        continue;
      }

      if (message.id === undefined) {
        const shouldExit = await this.handleNotification(message);
        if (shouldExit) {
          break;
        }
        continue;
      }

      const shouldExit = await this.handleRequest(message);
      if (shouldExit) {
        break;
      }
    }
  }

  private async handleNotification(message: JsonRpcRequest): Promise<boolean> {
    if (message.method === "exit") {
      await this.shutdown();
      return true;
    }
    return false;
  }

  private async handleRequest(message: JsonRpcRequest): Promise<boolean> {
    const id = message.id ?? null;
    try {
      switch (message.method) {
        case "initialize":
          await this.reply(id, successResponse(id, await this.handleInitialize(message.params)));
          return false;
        case "provider.execute":
          await this.reply(id, successResponse(id, await this.handleExecute(message.params)));
          return false;
        case "auth.start":
          await this.reply(id, successResponse(id, await this.handleAuthStart(message.params)));
          return false;
        case "auth.exchange_code":
          await this.reply(id, successResponse(id, await this.handleAuthExchangeCode(message.params)));
          return false;
        case "auth.refresh_token":
          await this.reply(id, successResponse(id, await this.handleAuthRefreshToken(message.params)));
          return false;
        case "shutdown":
          await this.reply(id, successResponse(id, null));
          await this.shutdown();
          return true;
        default:
          await this.reply(id, errorResponse(id, ERR_METHOD_NOT_FOUND, `method not found: ${message.method}`));
          return false;
      }
    } catch (error) {
      await this.reply(id, errorResponse(id, ERR_INTERNAL, error instanceof Error ? error.message : String(error)));
    }
    return false;
  }

  private async handleInitialize(params: unknown): Promise<InitializeResult> {
    const request = this.requireParams<InitializeRequest>(params, "initialize");
    if (this.definition.initialize) {
      return await maybeAsync(this.definition.initialize(request));
    }
    return {
      protocolVersion: PROTOCOL_VERSION,
      pluginInfo: this.definition.pluginInfo,
      provider: this.definition.provider,
      capabilities: this.definition.capabilities,
    };
  }

  private async handleExecute(params: unknown): Promise<ExecuteResult> {
    const request = this.requireParams<ExecuteRequest>(params, "provider.execute");
    return await maybeAsync(this.definition.execute(request));
  }

  private async handleAuthStart(params: unknown): Promise<AuthStartResult> {
    const request = this.requireParams<AuthStartRequest>(params, "auth.start");
    const handler = this.definition.auth?.start;
    if (!handler) {
      throw new Error("auth.start is not implemented");
    }
    return await maybeAsync(handler(request));
  }

  private async handleAuthExchangeCode(params: unknown): Promise<TokenResult> {
    const request = this.requireParams<AuthExchangeRequest>(params, "auth.exchange_code");
    const handler = this.definition.auth?.exchangeCode;
    if (!handler) {
      throw new Error("auth.exchange_code is not implemented");
    }
    return await maybeAsync(handler(request));
  }

  private async handleAuthRefreshToken(params: unknown): Promise<TokenResult> {
    const request = this.requireParams<AuthRefreshRequest>(params, "auth.refresh_token");
    const handler = this.definition.auth?.refreshToken;
    if (!handler) {
      throw new Error("auth.refresh_token is not implemented");
    }
    return await maybeAsync(handler(request));
  }

  private requireParams<T>(params: unknown, method: string): T {
    if (!params || typeof params !== "object") {
      throw new Error(`${method} requires a JSON object`);
    }
    return params as T;
  }

  private async reply(id: string | number | null, response: JsonRpcResponse): Promise<void> {
    if (id === null && response.error) {
      await writeFrame(this.output, response);
      return;
    }
    await writeFrame(this.output, response);
  }

  private async shutdown(): Promise<void> {
    if (this.definition.onShutdown) {
      await maybeAsync(this.definition.onShutdown());
    }
  }
}

export function createPlugin(definition: PluginDefinition): PluginRuntime {
  return new PluginRuntime(definition);
}

export async function servePlugin(definition: PluginDefinition): Promise<void> {
  await createPlugin(definition).serve();
}
