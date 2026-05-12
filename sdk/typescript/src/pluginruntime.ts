import { create } from "@bufbuild/protobuf";
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
  PluginRuntimeEgressMode as ProtoPluginRuntimeEgressMode,
  PluginRuntimeProvider as PluginRuntimeProviderService,
  PluginRuntimeSessionSchema,
  PluginRuntimeSupportSchema,
  type GetPluginRuntimeSessionRequest as ProtoGetPluginRuntimeSessionRequest,
  type ListPluginRuntimeSessionsRequest as ProtoListPluginRuntimeSessionsRequest,
  type PreparePluginRuntimeWorkspaceRequest as ProtoPreparePluginRuntimeWorkspaceRequest,
  type RemovePluginRuntimeWorkspaceRequest as ProtoRemovePluginRuntimeWorkspaceRequest,
  type StartHostedPluginRequest as ProtoStartHostedPluginRequest,
  type StartPluginRuntimeSessionRequest as ProtoStartPluginRuntimeSessionRequest,
  type StopPluginRuntimeSessionRequest as ProtoStopPluginRuntimeSessionRequest,
} from "./internal/gen/v1/pluginruntime_pb.ts";
import {
  timestampFromDate,
} from "./protocol.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

type PluginRuntimeProviderServiceImpl = Partial<
  ServiceImpl<typeof PluginRuntimeProviderService>
>;

/** Native egress-mode constants for authored plugin runtime providers. */
export const PluginRuntimeEgressMode = {
  UNSPECIFIED: ProtoPluginRuntimeEgressMode.UNSPECIFIED,
  NONE: ProtoPluginRuntimeEgressMode.NONE,
  CIDR: ProtoPluginRuntimeEgressMode.CIDR,
  HOSTNAME: ProtoPluginRuntimeEgressMode.HOSTNAME,
} as const;
export type PluginRuntimeEgressMode =
  (typeof PluginRuntimeEgressMode)[keyof typeof PluginRuntimeEgressMode];

export interface PluginRuntimeSupport {
  canHostPlugins?: boolean | undefined;
  egressMode?: PluginRuntimeEgressMode | undefined;
  supportsPrepareWorkspace?: boolean | undefined;
}

export interface PluginRuntimeSessionLifecycle {
  startedAt?: Date | undefined;
  recommendedDrainAt?: Date | undefined;
  expiresAt?: Date | undefined;
}

export interface PluginRuntimeSession {
  id?: string | undefined;
  state?: string | undefined;
  metadata?: Record<string, string> | undefined;
  lifecycle?: PluginRuntimeSessionLifecycle | undefined;
  stateReason?: string | undefined;
  stateMessage?: string | undefined;
}

export interface PluginRuntimeImagePullAuth {
  dockerConfigJson?: string | undefined;
}

export interface StartPluginRuntimeSessionRequest {
  pluginName: string;
  template?: string | undefined;
  image?: string | undefined;
  metadata?: Record<string, string> | undefined;
  imagePullAuth?: PluginRuntimeImagePullAuth | undefined;
}

export interface GetPluginRuntimeSessionRequest {
  sessionId: string;
}

export interface ListPluginRuntimeSessionsRequest {}

export interface StopPluginRuntimeSessionRequest {
  sessionId: string;
}

export interface PluginRuntimeAgentWorkspaceGitCheckout {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface PluginRuntimeAgentWorkspace {
  checkouts?: readonly PluginRuntimeAgentWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

export interface PluginRuntimePreparedAgentWorkspace {
  root?: string | undefined;
  cwd?: string | undefined;
}

export interface PreparePluginRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
  workspace?: PluginRuntimeAgentWorkspace | undefined;
}

export interface PreparePluginRuntimeWorkspaceResponse {
  workspace?: PluginRuntimePreparedAgentWorkspace | undefined;
}

export interface RemovePluginRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
}

export interface StartHostedPluginRequest {
  sessionId: string;
  pluginName: string;
  command?: string | undefined;
  args?: readonly string[] | undefined;
  env?: Record<string, string> | undefined;
  allowedHosts?: readonly string[] | undefined;
  defaultAction?: string | undefined;
  hostBinary?: string | undefined;
}

export interface HostedPlugin {
  id?: string | undefined;
  sessionId?: string | undefined;
  pluginName?: string | undefined;
  dialTarget?: string | undefined;
}

export interface PluginRuntimeProviderOptions extends ProviderBaseOptions {
  getSupport: () => MaybePromise<PluginRuntimeSupport>;
  startSession: (
    request: StartPluginRuntimeSessionRequest,
  ) => MaybePromise<PluginRuntimeSession>;
  getSession: (
    request: GetPluginRuntimeSessionRequest,
  ) => MaybePromise<PluginRuntimeSession>;
  listSessions: (
    request: ListPluginRuntimeSessionsRequest,
  ) => MaybePromise<readonly PluginRuntimeSession[]>;
  stopSession: (request: StopPluginRuntimeSessionRequest) => MaybePromise<void>;
  prepareWorkspace?: (
    request: PreparePluginRuntimeWorkspaceRequest,
  ) => MaybePromise<PreparePluginRuntimeWorkspaceResponse>;
  removeWorkspace?: (
    request: RemovePluginRuntimeWorkspaceRequest,
  ) => MaybePromise<void>;
  startPlugin: (
    request: StartHostedPluginRequest,
  ) => MaybePromise<HostedPlugin>;
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

  async getSupport(): Promise<PluginRuntimeSupport> {
    return await this.getSupportHandler();
  }

  async startSession(
    request: StartPluginRuntimeSessionRequest,
  ): Promise<PluginRuntimeSession> {
    return await this.startSessionHandler(request);
  }

  async getSession(
    request: GetPluginRuntimeSessionRequest,
  ): Promise<PluginRuntimeSession> {
    return await this.getSessionHandler(request);
  }

  async listSessions(
    request: ListPluginRuntimeSessionsRequest,
  ): Promise<readonly PluginRuntimeSession[]> {
    return await this.listSessionsHandler(request);
  }

  async stopSession(request: StopPluginRuntimeSessionRequest): Promise<void> {
    await this.stopSessionHandler(request);
  }

  async prepareWorkspace(
    request: PreparePluginRuntimeWorkspaceRequest,
  ): Promise<PreparePluginRuntimeWorkspaceResponse> {
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
  ): Promise<HostedPlugin> {
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
): PluginRuntimeProviderServiceImpl {
  return {
    async getSupport() {
      return create(
        PluginRuntimeSupportSchema,
        pluginRuntimeSupportToProto(
          await invokePluginRuntimeProvider("get support", () =>
            provider.getSupport(),
          ),
        ),
      );
    },
    async startSession(request) {
      return create(
        PluginRuntimeSessionSchema,
        pluginRuntimeSessionToProto(
          await invokePluginRuntimeProvider("start session", () =>
            provider.startSession(startSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async getSession(request) {
      return create(
        PluginRuntimeSessionSchema,
        pluginRuntimeSessionToProto(
          await invokePluginRuntimeProvider("get session", () =>
            provider.getSession(getSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async listSessions(request) {
      return create(ListPluginRuntimeSessionsResponseSchema, {
        sessions: (
          await invokePluginRuntimeProvider("list sessions", () =>
            provider.listSessions(listSessionsRequestFromProto(request)),
          )
        ).map(pluginRuntimeSessionToProto),
      });
    },
    async stopSession(request) {
      await invokePluginRuntimeProvider("stop session", () =>
        provider.stopSession(stopSessionRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async prepareWorkspace(request) {
      return create(
        PreparePluginRuntimeWorkspaceResponseSchema,
        prepareWorkspaceResponseToProto(
          await invokePluginRuntimeProvider("prepare workspace", () =>
            provider.prepareWorkspace(prepareWorkspaceRequestFromProto(request)),
          ),
        ),
      );
    },
    async removeWorkspace(request) {
      await invokePluginRuntimeProvider("remove workspace", () =>
        provider.removeWorkspace(removeWorkspaceRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async startPlugin(request) {
      return create(
        HostedPluginSchema,
        hostedPluginToProto(
          await invokePluginRuntimeProvider("start plugin", () =>
            provider.startPlugin(startHostedPluginRequestFromProto(request)),
          ),
        ),
      );
    },
  };
}

function startSessionRequestFromProto(
  request: ProtoStartPluginRuntimeSessionRequest,
): StartPluginRuntimeSessionRequest {
  return {
    pluginName: request.pluginName,
    template: request.template,
    image: request.image,
    metadata: { ...request.metadata },
    imagePullAuth: request.imagePullAuth === undefined
      ? undefined
      : { dockerConfigJson: request.imagePullAuth.dockerConfigJson },
  };
}

function getSessionRequestFromProto(
  request: ProtoGetPluginRuntimeSessionRequest,
): GetPluginRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function listSessionsRequestFromProto(
  _request: ProtoListPluginRuntimeSessionsRequest,
): ListPluginRuntimeSessionsRequest {
  return {};
}

function stopSessionRequestFromProto(
  request: ProtoStopPluginRuntimeSessionRequest,
): StopPluginRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function prepareWorkspaceRequestFromProto(
  request: ProtoPreparePluginRuntimeWorkspaceRequest,
): PreparePluginRuntimeWorkspaceRequest {
  return {
    sessionId: request.sessionId,
    agentSessionId: request.agentSessionId,
    workspace: request.workspace === undefined
      ? undefined
      : {
          checkouts: request.workspace.checkouts.map((checkout) => ({
            url: checkout.url,
            ref: checkout.ref,
            path: checkout.path,
          })),
          cwd: request.workspace.cwd,
        },
  };
}

function removeWorkspaceRequestFromProto(
  request: ProtoRemovePluginRuntimeWorkspaceRequest,
): RemovePluginRuntimeWorkspaceRequest {
  return {
    sessionId: request.sessionId,
    agentSessionId: request.agentSessionId,
  };
}

function startHostedPluginRequestFromProto(
  request: ProtoStartHostedPluginRequest,
): StartHostedPluginRequest {
  return {
    sessionId: request.sessionId,
    pluginName: request.pluginName,
    command: request.command,
    args: [...request.args],
    env: { ...request.env },
    allowedHosts: [...request.allowedHosts],
    defaultAction: request.defaultAction,
    hostBinary: request.hostBinary,
  };
}

function pluginRuntimeSupportToProto(support: PluginRuntimeSupport) {
  return {
    canHostPlugins: support.canHostPlugins ?? false,
    egressMode: support.egressMode ?? PluginRuntimeEgressMode.UNSPECIFIED,
    supportsPrepareWorkspace: support.supportsPrepareWorkspace ?? false,
  };
}

function pluginRuntimeSessionToProto(session: PluginRuntimeSession) {
  return {
    id: session.id ?? "",
    state: session.state ?? "",
    metadata: { ...(session.metadata ?? {}) },
    lifecycle: session.lifecycle === undefined
      ? undefined
      : {
          startedAt: session.lifecycle.startedAt === undefined
            ? undefined
            : timestampFromDate(session.lifecycle.startedAt),
          recommendedDrainAt: session.lifecycle.recommendedDrainAt === undefined
            ? undefined
            : timestampFromDate(session.lifecycle.recommendedDrainAt),
          expiresAt: session.lifecycle.expiresAt === undefined
            ? undefined
            : timestampFromDate(session.lifecycle.expiresAt),
        },
    stateReason: session.stateReason ?? "",
    stateMessage: session.stateMessage ?? "",
  };
}

function prepareWorkspaceResponseToProto(
  response: PreparePluginRuntimeWorkspaceResponse,
) {
  return {
    workspace: response.workspace === undefined
      ? undefined
      : {
          root: response.workspace.root ?? "",
          cwd: response.workspace.cwd ?? "",
        },
  };
}

function hostedPluginToProto(plugin: HostedPlugin) {
  return {
    id: plugin.id ?? "",
    sessionId: plugin.sessionId ?? "",
    pluginName: plugin.pluginName ?? "",
    dialTarget: plugin.dialTarget ?? "",
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
