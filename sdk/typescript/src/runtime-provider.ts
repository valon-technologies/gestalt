import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  HostedAppSchema,
  ListRuntimeSessionsResponseSchema,
  PrepareRuntimeWorkspaceResponseSchema,
  RuntimeEgressMode as ProtoRuntimeEgressMode,
  Runtime as RuntimeProviderService,
  RuntimeSessionSchema,
  RuntimeSupportSchema,
  type GetRuntimeSessionRequest as ProtoGetRuntimeSessionRequest,
  type ListRuntimeSessionsRequest as ProtoListRuntimeSessionsRequest,
  type PrepareRuntimeWorkspaceRequest as ProtoPrepareRuntimeWorkspaceRequest,
  type RemoveRuntimeWorkspaceRequest as ProtoRemoveRuntimeWorkspaceRequest,
  type StartHostedAppRequest as ProtoStartHostedAppRequest,
  type StartRuntimeSessionRequest as ProtoStartRuntimeSessionRequest,
  type StopRuntimeSessionRequest as ProtoStopRuntimeSessionRequest,
} from "./internal/gen/v1/runtime_provider_pb.ts";
import {
  timestampFromDate,
} from "./protocol.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

type RuntimeProviderServiceImpl = Partial<
  ServiceImpl<typeof RuntimeProviderService>
>;

/** Native egress-mode constants for authored runtime providers. */
export const RuntimeEgressMode = {
  UNSPECIFIED: ProtoRuntimeEgressMode.UNSPECIFIED,
  NONE: ProtoRuntimeEgressMode.NONE,
  CIDR: ProtoRuntimeEgressMode.CIDR,
  HOSTNAME: ProtoRuntimeEgressMode.HOSTNAME,
} as const;
export type RuntimeEgressMode =
  (typeof RuntimeEgressMode)[keyof typeof RuntimeEgressMode];

export interface RuntimeSupport {
  canHostApps?: boolean | undefined;
  egressMode?: RuntimeEgressMode | undefined;
  supportsPrepareWorkspace?: boolean | undefined;
}

export interface RuntimeSessionLifecycle {
  startedAt?: Date | undefined;
  recommendedDrainAt?: Date | undefined;
  expiresAt?: Date | undefined;
}

export interface RuntimeSession {
  id?: string | undefined;
  state?: string | undefined;
  metadata?: Record<string, string> | undefined;
  lifecycle?: RuntimeSessionLifecycle | undefined;
  stateReason?: string | undefined;
  stateMessage?: string | undefined;
}

export interface RuntimeImagePullAuth {
  dockerConfigJson?: string | undefined;
}

export interface StartRuntimeSessionRequest {
  appName: string;
  template?: string | undefined;
  image?: string | undefined;
  metadata?: Record<string, string> | undefined;
  imagePullAuth?: RuntimeImagePullAuth | undefined;
}

export interface GetRuntimeSessionRequest {
  sessionId: string;
}

export interface ListRuntimeSessionsRequest {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface ListRuntimeSessionsResponse {
  sessions?: readonly RuntimeSession[] | undefined;
  nextPageToken?: string | undefined;
}

export interface StopRuntimeSessionRequest {
  sessionId: string;
}

export interface RuntimeAgentWorkspaceGitCheckout {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface RuntimeAgentWorkspace {
  checkouts?: readonly RuntimeAgentWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

export interface RuntimePreparedAgentWorkspace {
  root?: string | undefined;
  cwd?: string | undefined;
}

export interface PrepareRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
  workspace?: RuntimeAgentWorkspace | undefined;
}

export interface PrepareRuntimeWorkspaceResponse {
  workspace?: RuntimePreparedAgentWorkspace | undefined;
}

export interface RemoveRuntimeWorkspaceRequest {
  sessionId: string;
  agentSessionId: string;
}

export interface StartHostedAppRequest {
  sessionId: string;
  appName: string;
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
  appName?: string | undefined;
  dialTarget?: string | undefined;
}

export interface RuntimeProviderOptions extends ProviderBaseOptions {
  getSupport: () => MaybePromise<RuntimeSupport>;
  startSession: (
    request: StartRuntimeSessionRequest,
  ) => MaybePromise<RuntimeSession>;
  getSession: (
    request: GetRuntimeSessionRequest,
  ) => MaybePromise<RuntimeSession>;
  listSessions: (
    request: ListRuntimeSessionsRequest,
  ) => MaybePromise<ListRuntimeSessionsResponse>;
  stopSession: (request: StopRuntimeSessionRequest) => MaybePromise<void>;
  prepareWorkspace?: (
    request: PrepareRuntimeWorkspaceRequest,
  ) => MaybePromise<PrepareRuntimeWorkspaceResponse>;
  removeWorkspace?: (
    request: RemoveRuntimeWorkspaceRequest,
  ) => MaybePromise<void>;
  startApp: (
    request: StartHostedAppRequest,
  ) => MaybePromise<HostedApp>;
}

export class RuntimeProvider extends ProviderBase {
  readonly kind = "runtime" as const;

  private readonly getSupportHandler: RuntimeProviderOptions["getSupport"];
  private readonly startSessionHandler: RuntimeProviderOptions["startSession"];
  private readonly getSessionHandler: RuntimeProviderOptions["getSession"];
  private readonly listSessionsHandler: RuntimeProviderOptions["listSessions"];
  private readonly stopSessionHandler: RuntimeProviderOptions["stopSession"];
  private readonly prepareWorkspaceHandler: RuntimeProviderOptions["prepareWorkspace"];
  private readonly removeWorkspaceHandler: RuntimeProviderOptions["removeWorkspace"];
  private readonly startAppHandler: RuntimeProviderOptions["startApp"];

  constructor(options: RuntimeProviderOptions) {
    super(options);
    this.getSupportHandler = options.getSupport;
    this.startSessionHandler = options.startSession;
    this.getSessionHandler = options.getSession;
    this.listSessionsHandler = options.listSessions;
    this.stopSessionHandler = options.stopSession;
    this.prepareWorkspaceHandler = options.prepareWorkspace;
    this.removeWorkspaceHandler = options.removeWorkspace;
    this.startAppHandler = options.startApp;
  }

  async getSupport(): Promise<RuntimeSupport> {
    return await this.getSupportHandler();
  }

  async startSession(
    request: StartRuntimeSessionRequest,
  ): Promise<RuntimeSession> {
    return await this.startSessionHandler(request);
  }

  async getSession(
    request: GetRuntimeSessionRequest,
  ): Promise<RuntimeSession> {
    return await this.getSessionHandler(request);
  }

  async listSessions(
    request: ListRuntimeSessionsRequest,
  ): Promise<ListRuntimeSessionsResponse> {
    return await this.listSessionsHandler(request);
  }

  async stopSession(request: StopRuntimeSessionRequest): Promise<void> {
    await this.stopSessionHandler(request);
  }

  async prepareWorkspace(
    request: PrepareRuntimeWorkspaceRequest,
  ): Promise<PrepareRuntimeWorkspaceResponse> {
    if (!this.prepareWorkspaceHandler) {
      throw new ConnectError(
        "runtime provider prepare workspace is not implemented",
        Code.Unimplemented,
      );
    }
    return await this.prepareWorkspaceHandler(request);
  }

  async removeWorkspace(
    request: RemoveRuntimeWorkspaceRequest,
  ): Promise<void> {
    if (!this.removeWorkspaceHandler) {
      throw new ConnectError(
        "runtime provider remove workspace is not implemented",
        Code.Unimplemented,
      );
    }
    await this.removeWorkspaceHandler(request);
  }

  async startApp(
    request: StartHostedAppRequest,
  ): Promise<HostedApp> {
    return await this.startAppHandler(request);
  }
}

export function defineRuntimeProvider(
  options: RuntimeProviderOptions,
): RuntimeProvider {
  return new RuntimeProvider(options);
}

export function isRuntimeProvider(
  value: unknown,
): value is RuntimeProvider {
  return (
    value instanceof RuntimeProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "runtime" &&
      "getSupport" in value &&
      "startSession" in value &&
      "startApp" in value)
  );
}

export function createRuntimeProviderService(
  provider: RuntimeProvider,
): RuntimeProviderServiceImpl {
  return {
    async getSupport() {
      return create(
        RuntimeSupportSchema,
        runtimeSupportToProto(
          await invokeRuntimeProvider("get support", () =>
            provider.getSupport(),
          ),
        ),
      );
    },
    async startSession(request) {
      return create(
        RuntimeSessionSchema,
        runtimeSessionToProto(
          await invokeRuntimeProvider("start session", () =>
            provider.startSession(startSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async getSession(request) {
      return create(
        RuntimeSessionSchema,
        runtimeSessionToProto(
          await invokeRuntimeProvider("get session", () =>
            provider.getSession(getSessionRequestFromProto(request)),
          ),
        ),
      );
    },
    async listSessions(request) {
      const response = await invokeRuntimeProvider("list sessions", () =>
        provider.listSessions(listSessionsRequestFromProto(request)),
      );
      return create(ListRuntimeSessionsResponseSchema, {
        sessions: (response.sessions ?? []).map(runtimeSessionToProto),
        nextPageToken: response.nextPageToken ?? "",
      });
    },
    async stopSession(request) {
      await invokeRuntimeProvider("stop session", () =>
        provider.stopSession(stopSessionRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async prepareWorkspace(request) {
      return create(
        PrepareRuntimeWorkspaceResponseSchema,
        prepareWorkspaceResponseToProto(
          await invokeRuntimeProvider("prepare workspace", () =>
            provider.prepareWorkspace(prepareWorkspaceRequestFromProto(request)),
          ),
        ),
      );
    },
    async removeWorkspace(request) {
      await invokeRuntimeProvider("remove workspace", () =>
        provider.removeWorkspace(removeWorkspaceRequestFromProto(request)),
      );
      return create(EmptySchema);
    },
    async startApp(request) {
      return create(
        HostedAppSchema,
        hostedAppToProto(
          await invokeRuntimeProvider("start app", () =>
            provider.startApp(startHostedAppRequestFromProto(request)),
          ),
        ),
      );
    },
  };
}

function startSessionRequestFromProto(
  request: ProtoStartRuntimeSessionRequest,
): StartRuntimeSessionRequest {
  return {
    appName: request.appName,
    template: request.template,
    image: request.image,
    metadata: { ...request.metadata },
    imagePullAuth: request.imagePullAuth === undefined
      ? undefined
      : { dockerConfigJson: request.imagePullAuth.dockerConfigJson },
  };
}

function getSessionRequestFromProto(
  request: ProtoGetRuntimeSessionRequest,
): GetRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function listSessionsRequestFromProto(
  request: ProtoListRuntimeSessionsRequest,
): ListRuntimeSessionsRequest {
  const pageSize = request.pageSize;
  if (pageSize < 0) {
    throw new ConnectError(
      "page_size must be non-negative",
      Code.InvalidArgument,
    );
  }
  return {
    pageSize: pageSize === 0 ? 100 : Math.min(pageSize, 200),
    pageToken: request.pageToken,
  };
}

function stopSessionRequestFromProto(
  request: ProtoStopRuntimeSessionRequest,
): StopRuntimeSessionRequest {
  return { sessionId: request.sessionId };
}

function prepareWorkspaceRequestFromProto(
  request: ProtoPrepareRuntimeWorkspaceRequest,
): PrepareRuntimeWorkspaceRequest {
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
  request: ProtoRemoveRuntimeWorkspaceRequest,
): RemoveRuntimeWorkspaceRequest {
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
    appName: request.appName,
    command: request.command,
    args: [...request.args],
    env: { ...request.env },
    allowedHosts: [...request.allowedHosts],
    defaultAction: request.defaultAction,
    hostBinary: request.hostBinary,
    workdir: request.workdir,
  };
}

function runtimeSupportToProto(support: RuntimeSupport) {
  return {
    canHostApps: support.canHostApps ?? false,
    egressMode: support.egressMode ?? RuntimeEgressMode.UNSPECIFIED,
    supportsPrepareWorkspace: support.supportsPrepareWorkspace ?? false,
  };
}

function runtimeSessionToProto(session: RuntimeSession) {
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
  response: PrepareRuntimeWorkspaceResponse,
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

function hostedAppToProto(app: HostedApp) {
  return {
    id: app.id ?? "",
    sessionId: app.sessionId ?? "",
    appName: app.appName ?? "",
    dialTarget: app.dialTarget ?? "",
  };
}

async function invokeRuntimeProvider<T>(
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
      `runtime provider ${label}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
