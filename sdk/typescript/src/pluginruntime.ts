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
  PreparePluginRuntimeWorkspaceResponseSchema,
  PluginRuntimeEgressMode,
  PluginRuntimeProvider as PluginRuntimeProviderService,
  PluginRuntimeSessionSchema,
  PluginRuntimeSupportSchema,
  type GetPluginRuntimeSessionRequest,
  type HostedPlugin,
  type ListPluginRuntimeSessionsRequest,
  type PreparePluginRuntimeWorkspaceRequest,
  type RemovePluginRuntimeWorkspaceRequest,
  type PluginRuntimeSession,
  type PluginRuntimeSupport,
  type StartHostedPluginRequest,
  type StartPluginRuntimeSessionRequest,
  type StopPluginRuntimeSessionRequest,
} from "./internal/gen/v1/pluginruntime_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

export type {
  GetPluginRuntimeSessionRequest,
  HostedPlugin,
  ListPluginRuntimeSessionsRequest,
  PreparePluginRuntimeWorkspaceRequest,
  RemovePluginRuntimeWorkspaceRequest,
  PluginRuntimeSession,
  PluginRuntimeSupport,
  StartHostedPluginRequest,
  StartPluginRuntimeSessionRequest,
  StopPluginRuntimeSessionRequest,
};
export { PluginRuntimeEgressMode };

export interface PluginRuntimeProviderOptions extends ProviderBaseOptions {
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
  prepareWorkspace?: (
    request: PreparePluginRuntimeWorkspaceRequest,
  ) => MaybePromise<MessageInitShape<typeof PreparePluginRuntimeWorkspaceResponseSchema>>;
  removeWorkspace?: (
    request: RemovePluginRuntimeWorkspaceRequest,
  ) => MaybePromise<void>;
  startPlugin: (
    request: StartHostedPluginRequest,
  ) => MaybePromise<MessageInitShape<typeof HostedPluginSchema>>;
}

export class PluginRuntimeProvider extends ProviderBase {
  readonly kind = "runtime" as const;

  private readonly getSupportHandler: PluginRuntimeProviderOptions["getSupport"];
  private readonly startSessionHandler: PluginRuntimeProviderOptions["startSession"];
  private readonly getSessionHandler: PluginRuntimeProviderOptions["getSession"];
  private readonly listSessionsHandler: PluginRuntimeProviderOptions["listSessions"];
  private readonly stopSessionHandler: PluginRuntimeProviderOptions["stopSession"];
  private readonly prepareWorkspaceHandler: PluginRuntimeProviderOptions["prepareWorkspace"];
  private readonly removeWorkspaceHandler: PluginRuntimeProviderOptions["removeWorkspace"];
  private readonly startPluginHandler: PluginRuntimeProviderOptions["startPlugin"];

  constructor(options: PluginRuntimeProviderOptions) {
    super(options);
    this.getSupportHandler = options.getSupport;
    this.startSessionHandler = options.startSession;
    this.getSessionHandler = options.getSession;
    this.listSessionsHandler = options.listSessions;
    this.stopSessionHandler = options.stopSession;
    this.prepareWorkspaceHandler = options.prepareWorkspace;
    this.removeWorkspaceHandler = options.removeWorkspace;
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

  async prepareWorkspace(
    request: PreparePluginRuntimeWorkspaceRequest,
  ): Promise<MessageInitShape<typeof PreparePluginRuntimeWorkspaceResponseSchema>> {
    if (!this.prepareWorkspaceHandler) {
      throw new ConnectError(
        "plugin runtime provider prepare workspace is not implemented",
        Code.Unimplemented,
      );
    }
    return await this.prepareWorkspaceHandler(request);
  }

  async removeWorkspace(
    request: RemovePluginRuntimeWorkspaceRequest,
  ): Promise<void> {
    if (!this.removeWorkspaceHandler) {
      throw new ConnectError(
        "plugin runtime provider remove workspace is not implemented",
        Code.Unimplemented,
      );
    }
    await this.removeWorkspaceHandler(request);
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
    async prepareWorkspace(request) {
      return create(
        PreparePluginRuntimeWorkspaceResponseSchema,
        await invokePluginRuntimeProvider("prepare workspace", () =>
          provider.prepareWorkspace(request),
        ),
      );
    },
    async removeWorkspace(request) {
      await invokePluginRuntimeProvider("remove workspace", () =>
        provider.removeWorkspace(request),
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
    if (error instanceof ConnectError) {
      throw error;
    }
    throw new ConnectError(
      `plugin runtime provider ${label}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
