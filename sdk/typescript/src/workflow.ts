import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type Interceptor,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  ListWorkflowProviderEventTriggersResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  ListWorkflowProviderSchedulesResponseSchema,
  WorkflowHost as WorkflowHostService,
  WorkflowProvider as WorkflowProviderService,
  type BoundWorkflowEventTrigger,
  type BoundWorkflowRun,
  type BoundWorkflowSchedule,
  type CancelWorkflowProviderRunRequest,
  type DeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest,
  type GetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest,
  type InvokeWorkflowOperationRequest,
  type InvokeWorkflowOperationResponse,
  type ListWorkflowProviderEventTriggersRequest,
  type ListWorkflowProviderRunsRequest,
  type ListWorkflowProviderSchedulesRequest,
  type PauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest,
  type ResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest,
  type StartWorkflowProviderRunRequest,
  type UpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest,
  type WorkflowEvent,
  WorkflowRunStatus,
} from "./internal/gen/v1/workflow_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";

/** Environment variable containing the workflow-host service target. */
export const ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET";
/** Environment variable containing the optional workflow-host relay token. */
export const ENV_WORKFLOW_HOST_SOCKET_TOKEN = `${ENV_WORKFLOW_HOST_SOCKET}_TOKEN`;
const WORKFLOW_HOST_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

/**
 * Generated workflow protocol message types commonly used by providers.
 *
 * These are re-exported so workflow provider code can type runs, schedules,
 * triggers, and operation-invocation requests without importing from `gen`.
 */
export type {
  BoundWorkflowEventTrigger,
  BoundWorkflowRun,
  BoundWorkflowSchedule,
  CancelWorkflowProviderRunRequest,
  DeleteWorkflowProviderEventTriggerRequest,
  DeleteWorkflowProviderScheduleRequest,
  GetWorkflowProviderEventTriggerRequest,
  GetWorkflowProviderRunRequest,
  GetWorkflowProviderScheduleRequest,
  InvokeWorkflowOperationRequest,
  InvokeWorkflowOperationResponse,
  ListWorkflowProviderEventTriggersRequest,
  ListWorkflowProviderRunsRequest,
  ListWorkflowProviderSchedulesRequest,
  PauseWorkflowProviderEventTriggerRequest,
  PauseWorkflowProviderScheduleRequest,
  PublishWorkflowProviderEventRequest,
  ResumeWorkflowProviderEventTriggerRequest,
  ResumeWorkflowProviderScheduleRequest,
  StartWorkflowProviderRunRequest,
  UpsertWorkflowProviderEventTriggerRequest,
  UpsertWorkflowProviderScheduleRequest,
  WorkflowEvent,
};
export { WorkflowRunStatus };

/** Handlers and runtime metadata for a workflow provider. */
export interface WorkflowProviderOptions extends RuntimeProviderOptions {
  startRun: (
    request: StartWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  getRun: (
    request: GetWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  listRuns: (
    request: ListWorkflowProviderRunsRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>[]>;
  cancelRun: (
    request: CancelWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  upsertSchedule: (
    request: UpsertWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  getSchedule: (
    request: GetWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  listSchedules: (
    request: ListWorkflowProviderSchedulesRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>[]>;
  deleteSchedule: (
    request: DeleteWorkflowProviderScheduleRequest,
  ) => MaybePromise<void>;
  pauseSchedule: (
    request: PauseWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  resumeSchedule: (
    request: ResumeWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  upsertEventTrigger: (
    request: UpsertWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  getEventTrigger: (
    request: GetWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  listEventTriggers: (
    request: ListWorkflowProviderEventTriggersRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>[]>;
  deleteEventTrigger: (
    request: DeleteWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<void>;
  pauseEventTrigger: (
    request: PauseWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  resumeEventTrigger: (
    request: ResumeWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  publishEvent: (
    request: PublishWorkflowProviderEventRequest,
  ) => MaybePromise<void>;
}

/** Runtime provider implementation for the Gestalt workflow host contract. */
export class WorkflowProvider extends RuntimeProvider {
  readonly kind = "workflow" as const;

  private readonly startRunHandler: WorkflowProviderOptions["startRun"];
  private readonly getRunHandler: WorkflowProviderOptions["getRun"];
  private readonly listRunsHandler: WorkflowProviderOptions["listRuns"];
  private readonly cancelRunHandler: WorkflowProviderOptions["cancelRun"];
  private readonly upsertScheduleHandler: WorkflowProviderOptions["upsertSchedule"];
  private readonly getScheduleHandler: WorkflowProviderOptions["getSchedule"];
  private readonly listSchedulesHandler: WorkflowProviderOptions["listSchedules"];
  private readonly deleteScheduleHandler: WorkflowProviderOptions["deleteSchedule"];
  private readonly pauseScheduleHandler: WorkflowProviderOptions["pauseSchedule"];
  private readonly resumeScheduleHandler: WorkflowProviderOptions["resumeSchedule"];
  private readonly upsertEventTriggerHandler: WorkflowProviderOptions["upsertEventTrigger"];
  private readonly getEventTriggerHandler: WorkflowProviderOptions["getEventTrigger"];
  private readonly listEventTriggersHandler: WorkflowProviderOptions["listEventTriggers"];
  private readonly deleteEventTriggerHandler: WorkflowProviderOptions["deleteEventTrigger"];
  private readonly pauseEventTriggerHandler: WorkflowProviderOptions["pauseEventTrigger"];
  private readonly resumeEventTriggerHandler: WorkflowProviderOptions["resumeEventTrigger"];
  private readonly publishEventHandler: WorkflowProviderOptions["publishEvent"];

  constructor(options: WorkflowProviderOptions) {
    super(options);
    this.startRunHandler = options.startRun;
    this.getRunHandler = options.getRun;
    this.listRunsHandler = options.listRuns;
    this.cancelRunHandler = options.cancelRun;
    this.upsertScheduleHandler = options.upsertSchedule;
    this.getScheduleHandler = options.getSchedule;
    this.listSchedulesHandler = options.listSchedules;
    this.deleteScheduleHandler = options.deleteSchedule;
    this.pauseScheduleHandler = options.pauseSchedule;
    this.resumeScheduleHandler = options.resumeSchedule;
    this.upsertEventTriggerHandler = options.upsertEventTrigger;
    this.getEventTriggerHandler = options.getEventTrigger;
    this.listEventTriggersHandler = options.listEventTriggers;
    this.deleteEventTriggerHandler = options.deleteEventTrigger;
    this.pauseEventTriggerHandler = options.pauseEventTrigger;
    this.resumeEventTriggerHandler = options.resumeEventTrigger;
    this.publishEventHandler = options.publishEvent;
  }

  async startRun(
    request: StartWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.startRunHandler(request);
  }

  async getRun(
    request: GetWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.getRunHandler(request);
  }

  async listRuns(
    request: ListWorkflowProviderRunsRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>[]> {
    return await this.listRunsHandler(request);
  }

  async cancelRun(
    request: CancelWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.cancelRunHandler(request);
  }

  async upsertSchedule(
    request: UpsertWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.upsertScheduleHandler(request);
  }

  async getSchedule(
    request: GetWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.getScheduleHandler(request);
  }

  async listSchedules(
    request: ListWorkflowProviderSchedulesRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>[]> {
    return await this.listSchedulesHandler(request);
  }

  async deleteSchedule(
    request: DeleteWorkflowProviderScheduleRequest,
  ): Promise<void> {
    await this.deleteScheduleHandler(request);
  }

  async pauseSchedule(
    request: PauseWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.pauseScheduleHandler(request);
  }

  async resumeSchedule(
    request: ResumeWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.resumeScheduleHandler(request);
  }

  async upsertEventTrigger(
    request: UpsertWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.upsertEventTriggerHandler(request);
  }

  async getEventTrigger(
    request: GetWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.getEventTriggerHandler(request);
  }

  async listEventTriggers(
    request: ListWorkflowProviderEventTriggersRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>[]> {
    return await this.listEventTriggersHandler(request);
  }

  async deleteEventTrigger(
    request: DeleteWorkflowProviderEventTriggerRequest,
  ): Promise<void> {
    await this.deleteEventTriggerHandler(request);
  }

  async pauseEventTrigger(
    request: PauseWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.pauseEventTriggerHandler(request);
  }

  async resumeEventTrigger(
    request: ResumeWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.resumeEventTriggerHandler(request);
  }

  async publishEvent(
    request: PublishWorkflowProviderEventRequest,
  ): Promise<void> {
    await this.publishEventHandler(request);
  }
}

/** Creates a workflow provider for export from a provider module. */
export function defineWorkflowProvider(
  options: WorkflowProviderOptions,
): WorkflowProvider {
  return new WorkflowProvider(options);
}

/** Runtime type guard for workflow providers loaded from user modules. */
export function isWorkflowProvider(value: unknown): value is WorkflowProvider {
  return (
    value instanceof WorkflowProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "workflow" &&
      "startRun" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "cancelRun" in value &&
      "upsertSchedule" in value &&
      "getSchedule" in value &&
      "listSchedules" in value &&
      "deleteSchedule" in value &&
      "pauseSchedule" in value &&
      "resumeSchedule" in value &&
      "upsertEventTrigger" in value &&
      "getEventTrigger" in value &&
      "listEventTriggers" in value &&
      "deleteEventTrigger" in value &&
      "pauseEventTrigger" in value &&
      "resumeEventTrigger" in value &&
      "publishEvent" in value)
  );
}

/** Client for invoking operations from workflow provider code. */
export class WorkflowHost {
  private readonly client: Client<typeof WorkflowHostService>;

  constructor() {
    const target = process.env[ENV_WORKFLOW_HOST_SOCKET];
    if (!target) {
      throw new Error(`workflow host: ${ENV_WORKFLOW_HOST_SOCKET} is not set`);
    }
    const relayToken = process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN]?.trim() ?? "";
    const transport = createGrpcTransport({
      ...workflowHostTransportOptions(target),
      interceptors: relayToken
        ? [workflowHostRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(WorkflowHostService, transport);
  }

  /** Invokes an operation through the workflow host service. */
  async invokeOperation(
    request: InvokeWorkflowOperationRequest,
  ): Promise<InvokeWorkflowOperationResponse> {
    return await this.client.invokeOperation(request);
  }
}

function workflowHostTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("workflow host: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `workflow host: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `workflow host: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `workflow host: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `workflow host: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function workflowHostRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(WORKFLOW_HOST_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}

/** Builds the Connect service implementation used by the TypeScript runtime. */
export function createWorkflowProviderService(
  provider: WorkflowProvider,
): Partial<ServiceImpl<typeof WorkflowProviderService>> {
  return {
    async startRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("start run", () => provider.startRun(request)),
      );
    },
    async getRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("get run", () => provider.getRun(request)),
      );
    },
    async listRuns(request) {
      return create(ListWorkflowProviderRunsResponseSchema, {
        runs: await invokeWorkflowProvider("list runs", () =>
          provider.listRuns(request),
        ),
      });
    },
    async cancelRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("cancel run", () => provider.cancelRun(request)),
      );
    },
    async upsertSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("upsert schedule", () =>
          provider.upsertSchedule(request),
        ),
      );
    },
    async getSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("get schedule", () =>
          provider.getSchedule(request),
        ),
      );
    },
    async listSchedules(request) {
      return create(ListWorkflowProviderSchedulesResponseSchema, {
        schedules: await invokeWorkflowProvider("list schedules", () =>
          provider.listSchedules(request),
        ),
      });
    },
    async deleteSchedule(request) {
      await invokeWorkflowProvider("delete schedule", () =>
        provider.deleteSchedule(request),
      );
      return create(EmptySchema, {});
    },
    async pauseSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("pause schedule", () =>
          provider.pauseSchedule(request),
        ),
      );
    },
    async resumeSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("resume schedule", () =>
          provider.resumeSchedule(request),
        ),
      );
    },
    async upsertEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("upsert event trigger", () =>
          provider.upsertEventTrigger(request),
        ),
      );
    },
    async getEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("get event trigger", () =>
          provider.getEventTrigger(request),
        ),
      );
    },
    async listEventTriggers(request) {
      return create(ListWorkflowProviderEventTriggersResponseSchema, {
        triggers: await invokeWorkflowProvider("list event triggers", () =>
          provider.listEventTriggers(request),
        ),
      });
    },
    async deleteEventTrigger(request) {
      await invokeWorkflowProvider("delete event trigger", () =>
        provider.deleteEventTrigger(request),
      );
      return create(EmptySchema, {});
    },
    async pauseEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("pause event trigger", () =>
          provider.pauseEventTrigger(request),
        ),
      );
    },
    async resumeEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("resume event trigger", () =>
          provider.resumeEventTrigger(request),
        ),
      );
    },
    async publishEvent(request) {
      await invokeWorkflowProvider("publish event", () =>
        provider.publishEvent(request),
      );
      return create(EmptySchema, {});
    },
  };
}

async function invokeWorkflowProvider<T>(
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
      `workflow provider ${action}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}
