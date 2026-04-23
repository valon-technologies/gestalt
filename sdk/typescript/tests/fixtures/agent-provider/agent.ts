import { create } from "@bufbuild/protobuf";

import {
  AgentRunStatus,
  BoundAgentRunSchema,
  type GetAgentProviderRunRequest,
  type StartAgentProviderRunRequest,
} from "../../../gen/v1/agent_pb.ts";
import { defineAgentProvider } from "../../../src/index.ts";

const runs = new Map<string, ReturnType<typeof createRun>>();
let canceledRuns = 0;

export const provider = defineAgentProvider({
  displayName: "Fixture Agent",
  description: "Agent provider fixture used by SDK tests",
  configure() {
    runs.clear();
    canceledRuns = 0;
  },
  async startRun(request) {
    const run = createRun(
      request.runId || `run-${runs.size + 1}`,
      request,
      AgentRunStatus.PENDING,
      request.idempotencyKey ? `idempotency:${request.idempotencyKey}` : "queued",
    );
    runs.set(run.id, run);
    return run;
  },
  async getRun(request) {
    return requireRun(request);
  },
  async listRuns() {
    return [...runs.values()];
  },
  async cancelRun(request) {
    const run = requireRunByID(request.runId);
    const updated = create(BoundAgentRunSchema, {
      id: run.id,
      providerName: run.providerName,
      model: run.model,
      status: AgentRunStatus.CANCELED,
      messages: run.messages,
      outputText: run.outputText,
      ...(run.structuredOutput ? { structuredOutput: run.structuredOutput } : {}),
      statusMessage: request.reason,
      sessionRef: run.sessionRef,
      ...(run.createdBy ? { createdBy: run.createdBy } : {}),
      ...(run.createdAt ? { createdAt: run.createdAt } : {}),
      startedAt: timestampNow(),
      completedAt: timestampNow(),
      executionRef: run.executionRef,
    });
    runs.set(updated.id, updated);
    canceledRuns += 1;
    return updated;
  },
  warnings() {
    return canceledRuns > 0 ? [`canceled-runs:${canceledRuns}`] : [];
  },
});

function requireRun(request: GetAgentProviderRunRequest) {
  return requireRunByID(request.runId);
}

function requireRunByID(runId: string) {
  const run = runs.get(runId);
  if (!run) {
    throw new Error(`unknown run ${runId}`);
  }
  return run;
}

function createRun(
  id: string,
  request: StartAgentProviderRunRequest,
  status: AgentRunStatus,
  statusMessage: string,
) {
  return create(BoundAgentRunSchema, {
    id,
    providerName: request.providerName || "fixture-agent",
    model: request.model,
    status,
    messages: request.messages,
    outputText: request.messages.at(-1)?.text
      ? `echo:${request.messages.at(-1)!.text}`
      : "",
    statusMessage,
    sessionRef: request.sessionRef,
    ...(request.createdBy ? { createdBy: request.createdBy } : {}),
    createdAt: timestampNow(),
    executionRef: request.executionRef,
  });
}

function timestampNow() {
  return {
    seconds: BigInt(Math.trunc(Date.now() / 1000)),
    nanos: 0,
  };
}
