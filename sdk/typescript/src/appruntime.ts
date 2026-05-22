import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  HostedAppSchema,
  ListAppRuntimeSessionsResponseSchema,
  PrepareAppRuntimeWorkspaceResponseSchema,
  AppRuntimeEgressMode as ProtoAppRuntimeEgressMode,
  AppRuntimeProvider as AppRuntimeProviderService,
  AppRuntimeSessionSchema,
  AppRuntimeSupportSchema,
  type GetAppRuntimeSessionRequest as ProtoGetAppRuntimeSessionRequest,
  type ListAppRuntimeSessionsRequest as ProtoListAppRuntimeSessionsRequest,
  type PrepareAppRuntimeWorkspaceRequest as ProtoPrepareAppRuntimeWorkspaceRequest,
  type RemoveAppRuntimeWorkspaceRequest as ProtoRemoveAppRuntimeWorkspaceRequest,
  type StartHostedAppRequest as ProtoStartHostedAppRequest,
  type StartAppRuntimeSessionRequest as ProtoStartAppRuntimeSessionRequest,
  type StopAppRuntimeSessionRequest as ProtoStopAppRuntimeSessionRequest,
} from "./internal/gen/v1/appruntime_pb.ts";
import {
  timestampFromDate,
} from "./protocol.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

type AppRuntimeProviderServiceImpl = Partial<
  ServiceImpl<typeof AppRuntimeProviderService>
>;

/** Native egress-mode constants for authored app runtime providers. */
export const AppRuntimeEgressMode = {
  UNSPECIFIED: ProtoAppRuntimeEgressMode.UNSPECIFIED,
  NONE: ProtoAppRuntimeEgressMode.NONE,
  CIDR: ProtoAppRuntimeEgressMode.CIDR,
  HOSTNAME: ProtoAppRuntimeEgressMode.HOSTNAME,
} as const;
export type AppRuntimeEgressMode =
  (typeof AppRuntimeEgressMode)[keyof typeof AppRuntimeEgressMode];

export interface AppRuntimeSupport {
  canHostPlugins?: boolean | undefined;
  egressMode?: AppRuntimeEgressMode | undefined;
  supportsPrepareWorkspace?: boolean | undefined;
}

export interface AppRuntimeSessionLifecycle {
  startedAt?: Date | undefined;
  recommendedDrainAt?: Date | undefined;
  expiresAt?: Date | undefined;
}

export interface AppRuntimeSession {
  id?: string | undefined;
  state?: string | undefined;
  metadata?: Record<string, string> | undefined;
  lifecycle?: AppRuntimeSessionLifecycle | undefined;
  stateReason?: string | undefined;
  stateMessage?: string | undefined;
}

export interface AppRuntimeImagePullAuth {
  dockerConfigJson?: string | undefined;
}

export interface StartAppRuntimeSessionRequest {
  pluginName: string;
  template?: string | undefined;
  image?: string | undefined;
  metadata?: Record<string, string> | undefined;
  imagePullAuth?: AppRuntimeImagePullAuth | undefined;
}

export interface GetAppRuntimeSessionRequest {
  sessionId: string;
}

export interface ListAppRuntimeSessionsRequest {}

export interface StopAppRuntimeSessionRequest {
  sessionId: string;
}

export interface AppRuntimeAgentWorkspaceGitCheckout {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface AppRuntimeAgentWorkspace {
  checkouts?: readonly AppRuntimeAgentWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

export interface AppRuntimePreparedAgentWorkspace {
  root?: string | undefined;
  cwd?: string | undefined;
}

export interface PrepareAppRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
  workspace?: AppRuntimeAgentWorkspace | undefined;
}

export interface PrepareAppRuntimeWorkspaceResponse {
  workspace?: AppRuntimePreparedAgentWorkspace | undefined;
}

export interface RemoveAppRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
}

export interface StartHostedAppRequest {
  sessionId: string;
  pluginName: string;
  command?: string | undefined;
  args?: readonly string[] | undefined;
  env?: Record<string, string> | undefined;
  allowedHosts?: readonly string[] | undefined;
  defaultAction?: string | undefined;
  hostBinary?: string | undefined;
  workdir?: string | undefined;
}

export interface HostedApp {
  id?: string | undefined;
  sessionId?: string | undefined;
  pluginName?: string | undefined;
  dialTarget?: string | undefined;
}

export interface AppRuntimeProviderOptions extends ProviderBaseOptions {
  getSupport: () => MaybePromise<AppRuntimeSupport>;
  startSession: (
    request: StartAppRuntimeSessionRequest,
  ) => MaybePromise<AppRuntimeSession>;
  getSession: (
    request: GetAppRuntimeSessionRequest,
  ) => MaybePromise<AppRuntimeSession>;
  listSessions: (
    request: ListAppRuntimeSessionsRequest,
  ) => MaybePromise<readonly AppRuntimeSession[]>;
  stopSession: (request: StopAppRuntimeSessionRequest) => MaybePromise<void>;
  prepareWorkspace?: (
    request: PrepareAppRuntimeWorkspaceRequest,
  ) => MaybePromise<PrepareAppRuntimeWorkspaceResponse>;
  removeWorkspace?: (
    request: RemoveAppRuntimeWorkspaceRequest,
  ) => MaybePromise<void>;
  startPlugin: (
    request: StartHostedAppRequest,
  ) => MaybePromise<HostedApp>;
}

export class AppRuntimeProvider extends ProviderBase {
  readonly kind = "runtime" as const;

  private readonly getSupportHandler: AppRuntimeProviderOptions["getSupport"];
  private readonly startSessionHandler: AppRuntimeProviderOptions["startSession"];
  private readonly getSessionHandler: AppRuntimeProviderOptions["getSession"];
  private readonly listSessionsHandler: AppRuntimeProviderOptions["listSessions"];
  private readonly stopSessionHandler: AppRuntimeProviderOptions["stopSession"];
  private readonly prepareWorkspaceHandler: AppRuntimeProviderOptions["prepareWorkspace"];
  private readonly removeWorkspaceHandler: AppRuntimeProviderOptions["removeWorkspace"];
  private readonly startPluginHandler: AppRuntimeProviderOptions["startPlugin"];

  constructor(options: AppRuntimeProviderOptions) {
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

  async getSupport(): Promise<AppRuntimeSupport> {
    return await this.getSupportHandler();
  }

  async startSession(
    request: StartAppRuntimeSessionRequest,
  ): Promise<AppRuntimeSession> {
    return await this.startSessionHandler(request);
  }

  async getSession(
    request: GetAppRuntimeSessionRequest,
  ): Promise<AppRuntimeSession> {
    return await this.getSessionHandler(request);
  }

  async listSessions(
    request: ListAppRuntimeSessionsRequest,
  ): Promise<readonly AppRuntimeSession[]> {
    return await this.listSessionsHandler(request);
  }

  async stopSession(request: StopAppRuntimeSessionRequest): Promise<void> {
    await this.stopSessionHandler(request);
  }

  async prepareWorkspace(
    request: PrepareAppRuntimeWorkspaceRequest,
  ): Promise<PrepareAppRuntimeWorkspaceResponse> {
    if (!this.prepareWorkspaceHandler) {
      throw new ConnectError(
        "plugin runtime provider prepare workspace is not implemented",
        Code.Unimplemented,
      );
    }
    return await this.prepareWorkspaceHandler(request);
  }

  async removeWorkspace(
    request: RemoveAppRuntimeWorkspaceRequest,
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
    request: StartHostedAppRequest,
  ): Promise<HostedApp> {
    return await this.startPluginHandler(request);
  }
}

export function defineAppRuntimeProvider(
  options: AppRuntimeProviderOptions,
): AppRuntimeProvider {
  return new AppRuntimeProvider(options);
}

export function isAppRuntimeProvider(
  value: unknown,
): value is AppRuntimeProvider {
  return (
    value instanceof AppRuntimeProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "runtime" &&
      "getSupport" in value &&
      "startSession" in value &&
      "startPlugin" in value)
  );
}

export function createAppRuntimeProviderService(
  provider: AppRuntimeProvider,
): AppRuntimeProviderServiceImpl {
  return {
    async getSupport() {
      return create(
        AppRuntimeSupportSchema,
        pluginRuntimeSupportToProto(
          await invokeAppRuntimeProvider("get support", () =>
            provider.getSupport(),
          ),
        ),
      );
    },
    async startSession(request) {
      return create(
        AppRuntimeSessionSchema,
        pluginRuntimeSessionToProto(
          await invokeAppRuntimeProvider("start session", () =>
            provider.startSession(startSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async getSession(request) {
      return create(
        AppRuntimeSessionSchema,
        pluginRuntimeSessionToProto(
          await invokeAppRuntimeProvider("get session", () =>
            provider.getSession(getSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async listSessions(request) {
      return create(ListAppRuntimeSessionsResponseSchema, {
        sessions: (
          await invokeAppRuntimeProvider("list sessions", () =>
            provider.listSessions(listSessionsRequestFromProto(request)),
          )
        ).map(pluginRuntimeSessionToProto),
      });
    },
    async stopSession(request) {
      await invokeAppRuntimeProvider("stop session", () =>
        provider.stopSession(stopSessionRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async prepareWorkspace(request) {
      return create(
        PrepareAppRuntimeWorkspaceResponseSchema,
        prepareWorkspaceResponseToProto(
          await invokeAppRuntimeProvider("prepare workspace", () =>
            provider.prepareWorkspace(prepareWorkspaceRequestFromProto(request)),
          ),
        ),
      );
    },
    async removeWorkspace(request) {
      await invokeAppRuntimeProvider("remove workspace", () =>
        provider.removeWorkspace(removeWorkspaceRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async startPlugin(request) {
      return create(
        HostedAppSchema,
        hostedPluginToProto(
          await invokeAppRuntimeProvider("start plugin", () =>
            provider.startPlugin(startHostedAppRequestFromProto(request)),
          ),
        ),
      );
    },
  };
}

function startSessionRequestFromProto(
  request: ProtoStartAppRuntimeSessionRequest,
): StartAppRuntimeSessionRequest {
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
  request: ProtoGetAppRuntimeSessionRequest,
): GetAppRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function listSessionsRequestFromProto(
  _request: ProtoListAppRuntimeSessionsRequest,
): ListAppRuntimeSessionsRequest {
  return {};
}

function stopSessionRequestFromProto(
  request: ProtoStopAppRuntimeSessionRequest,
): StopAppRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function prepareWorkspaceRequestFromProto(
  request: ProtoPrepareAppRuntimeWorkspaceRequest,
): PrepareAppRuntimeWorkspaceRequest {
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
  request: ProtoRemoveAppRuntimeWorkspaceRequest,
): RemoveAppRuntimeWorkspaceRequest {
  return {
    sessionId: request.sessionId,
    agentSessionId: request.agentSessionId,
  };
}

function startHostedAppRequestFromProto(
  request: ProtoStartHostedAppRequest,
): StartHostedAppRequest {
  return {
    sessionId: request.sessionId,
    pluginName: request.pluginName,
    command: request.command,
    args: [...request.args],
    env: { ...request.env },
    allowedHosts: [...request.allowedHosts],
    defaultAction: request.defaultAction,
    hostBinary: request.hostBinary,
    workdir: request.workdir,
  };
}

function pluginRuntimeSupportToProto(support: AppRuntimeSupport) {
  return {
    canHostApps: support.canHostApps ?? false,
    egressMode: support.egressMode ?? AppRuntimeEgressMode.UNSPECIFIED,
    supportsPrepareWorkspace: support.supportsPrepareWorkspace ?? false,
  };
}

function pluginRuntimeSessionToProto(session: AppRuntimeSession) {
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
  response: PrepareAppRuntimeWorkspaceResponse,
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

function hostedPluginToProto(plugin: HostedApp) {
  return {
    id: app.id ?? "",
    sessionId: app.sessionId ?? "",
    pluginName: app.pluginName ?? "",
    dialTarget: app.dialTarget ?? "",
  };
}

async function invokeAppRuntimeProvider<T>(
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
