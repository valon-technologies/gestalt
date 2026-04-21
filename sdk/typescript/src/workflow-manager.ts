import { connect } from "node:net";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  WorkflowManagerCreateScheduleRequestSchema,
  WorkflowManagerDeleteScheduleRequestSchema,
  WorkflowManagerGetScheduleRequestSchema,
  WorkflowManagerHost as WorkflowManagerHostService,
  WorkflowManagerPauseScheduleRequestSchema,
  WorkflowManagerResumeScheduleRequestSchema,
  WorkflowManagerUpdateScheduleRequestSchema,
  type ManagedWorkflowSchedule,
} from "../gen/v1/workflow_pb.ts";
import type { Request } from "./api.ts";

export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";

export type ManagedWorkflowScheduleMessage = ManagedWorkflowSchedule;
export type WorkflowManagerCreateScheduleInput = MessageInitShape<
  typeof WorkflowManagerCreateScheduleRequestSchema
>;
export type WorkflowManagerGetScheduleInput = MessageInitShape<
  typeof WorkflowManagerGetScheduleRequestSchema
>;
export type WorkflowManagerUpdateScheduleInput = MessageInitShape<
  typeof WorkflowManagerUpdateScheduleRequestSchema
>;
export type WorkflowManagerDeleteScheduleInput = MessageInitShape<
  typeof WorkflowManagerDeleteScheduleRequestSchema
>;
export type WorkflowManagerPauseScheduleInput = MessageInitShape<
  typeof WorkflowManagerPauseScheduleRequestSchema
>;
export type WorkflowManagerResumeScheduleInput = MessageInitShape<
  typeof WorkflowManagerResumeScheduleRequestSchema
>;

export class WorkflowManager {
  private readonly client: Client<typeof WorkflowManagerHostService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const socketPath = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    if (!socketPath) {
      throw new Error(
        `workflow manager: ${ENV_WORKFLOW_MANAGER_SOCKET} is not set`,
      );
    }

    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
    });
    this.client = createClient(WorkflowManagerHostService, transport);
  }

  async createSchedule(
    request: WorkflowManagerCreateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.createSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getSchedule(
    request: WorkflowManagerGetScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.getSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async updateSchedule(
    request: WorkflowManagerUpdateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.updateSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async deleteSchedule(
    request: WorkflowManagerDeleteScheduleInput,
  ): Promise<void> {
    await this.client.deleteSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async pauseSchedule(
    request: WorkflowManagerPauseScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.pauseSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async resumeSchedule(
    request: WorkflowManagerResumeScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.resumeSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("workflow manager: invocation token is not available");
  }
  return trimmed;
}
