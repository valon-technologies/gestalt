import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  HostedPluginSchema,
  ListPluginRuntimeSessionsResponseSchema,
  PluginRuntimeEgressMode,
  PluginRuntimeProvider as PluginRuntimeProviderService,
  PluginRuntimeSessionSchema,
  PluginRuntimeSupportSchema,
  type GetPluginRuntimeSessionRequest,
  type HostedPlugin,
  type ListPluginRuntimeSessionsRequest,
  type PluginRuntimeSession,
  type PluginRuntimeSupport,
  type StartHostedPluginRequest,
  type StartPluginRuntimeSessionRequest,
  type StopPluginRuntimeSessionRequest,
} from "../gen/v1/pluginruntime_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

export type {
  GetPluginRuntimeSessionRequest,
  HostedPlugin,
  ListPluginRuntimeSessionsRequest,
  PluginRuntimeSession,
  PluginRuntimeSupport,
  StartHostedPluginRequest,
  StartPluginRuntimeSessionRequest,
  StopPluginRuntimeSessionRequest,
};
export { PluginRuntimeEgressMode };

export interface PluginRuntimeProviderOptions extends RuntimeProviderOptions {
  getSupport: () => MaybePromise<MessageInitShape<typeof PluginRuntimeSupportSchema>>;
  startSession: (
    request: StartPluginRuntimeSessionRequest,
  ) => MaybePromise<MessageInitShape<typeof PluginRuntimeSessionSchema>>;
  getSession: (
    request: GetPluginRuntimeSessionRequest,
  ) => MaybePromise<MessageInitShape<typeof PluginRuntimeSessionSchema>>;
  listSessions: (
    request: ListPluginRuntimeSessionsRequest,
  ) => MaybePromise<MessageInitShape<typeof PluginRuntimeSessionSchema>[]>;
  stopSession: (request: StopPluginRuntimeSessionRequest) => MaybePromise<void>;
  startPlugin: (
    request: StartHostedPluginRequest,
  ) => MaybePromise<MessageInitShape<typeof HostedPluginSchema>>;
}

export class PluginRuntimeProvider extends RuntimeProvider {
  readonly kind = "runtime" as const;

  private readonly getSupportHandler: PluginRuntimeProviderOptions["getSupport"];
  private readonly startSessionHandler: PluginRuntimeProviderOptions["startSession"];
  private readonly getSessionHandler: PluginRuntimeProviderOptions["getSession"];
  private readonly listSessionsHandler: PluginRuntimeProviderOptions["listSessions"];
  private readonly stopSessionHandler: PluginRuntimeProviderOptions["stopSession"];
  private readonly startPluginHandler: PluginRuntimeProviderOptions["startPlugin"];

  constructor(options: PluginRuntimeProviderOptions) {
    super(options);
    this.getSupportHandler = options.getSupport;
    this.startSessionHandler = options.startSession;
    this.getSessionHandler = options.getSession;
    this.listSessionsHandler = options.listSessions;
    this.stopSessionHandler = options.stopSession;
    this.startPluginHandler = options.startPlugin;
  }

  async getSupport(): Promise<MessageInitShape<typeof PluginRuntimeSupportSchema>> {
    return await this.getSupportHandler();
  }

  async startSession(
    request: StartPluginRuntimeSessionRequest,
  ): Promise<MessageInitShape<typeof PluginRuntimeSessionSchema>> {
    return await this.startSessionHandler(request);
  }

  async getSession(
    request: GetPluginRuntimeSessionRequest,
  ): Promise<MessageInitShape<typeof PluginRuntimeSessionSchema>> {
    return await this.getSessionHandler(request);
  }

  async listSessions(
    request: ListPluginRuntimeSessionsRequest,
  ): Promise<MessageInitShape<typeof PluginRuntimeSessionSchema>[]> {
    return await this.listSessionsHandler(request);
  }

  async stopSession(request: StopPluginRuntimeSessionRequest): Promise<void> {
    await this.stopSessionHandler(request);
  }

  async startPlugin(
    request: StartHostedPluginRequest,
  ): Promise<MessageInitShape<typeof HostedPluginSchema>> {
    return await this.startPluginHandler(request);
  }
}

export function definePluginRuntimeProvider(
  options: PluginRuntimeProviderOptions,
): PluginRuntimeProvider {
  return new PluginRuntimeProvider(options);
}

export function isPluginRuntimeProvider(
  value: unknown,
): value is PluginRuntimeProvider {
  return (
    value instanceof PluginRuntimeProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "runtime" &&
      "getSupport" in value &&
      "startSession" in value &&
      "startPlugin" in value)
  );
}

export function createPluginRuntimeProviderService(
  provider: PluginRuntimeProvider,
): Partial<ServiceImpl<typeof PluginRuntimeProviderService>> {
  return {
    async getSupport() {
      return create(
        PluginRuntimeSupportSchema,
        await invokePluginRuntimeProvider("get support", () =>
          provider.getSupport(),
        ),
      );
    },
    async startSession(request) {
      return create(
        PluginRuntimeSessionSchema,
        await invokePluginRuntimeProvider("start session", () =>
          provider.startSession(request),
        ),
      );
    },
    async getSession(request) {
      return create(
        PluginRuntimeSessionSchema,
        await invokePluginRuntimeProvider("get session", () =>
          provider.getSession(request),
        ),
      );
    },
    async listSessions(request) {
      return create(ListPluginRuntimeSessionsResponseSchema, {
        sessions: await invokePluginRuntimeProvider("list sessions", () =>
          provider.listSessions(request),
        ),
      });
    },
    async stopSession(request) {
      await invokePluginRuntimeProvider("stop session", () =>
        provider.stopSession(request),
      );
      return create(EmptySchema);
    },
    async startPlugin(request) {
      return create(
        HostedPluginSchema,
        await invokePluginRuntimeProvider("start plugin", () =>
          provider.startPlugin(request),
        ),
      );
    },
  };
}

async function invokePluginRuntimeProvider<T>(
  label: string,
  fn: () => MaybePromise<T>,
): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    throw new ConnectError(
      `plugin runtime provider ${label}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
