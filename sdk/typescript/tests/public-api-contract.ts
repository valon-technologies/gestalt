import {
  Authorization,
  PluginRuntimeEgressMode,
  RuntimeLogHost,
  WorkflowRunStatus,
  type AuthorizationEvaluateInput,
  type AgentManagerWorkspace,
  type RuntimeLogAppendResponse,
  type WorkflowEvent,
} from "@valon-technologies/gestalt";

// @ts-expect-error Root package must not expose protocol helper schemas.
import { StructSchema as RootStructSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protocol helper types.
import type { Struct as RootStruct } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated protocol schemas.
import { AccessEvaluationRequestSchema as RootAccessEvaluationRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated protocol message types.
import type { AccessEvaluationRequest as RootAccessEvaluationRequest } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose runtime-log generated request schemas.
import { AppendPluginRuntimeLogsRequestSchema as RootAppendPluginRuntimeLogsRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose runtime-log generated enums.
import { PluginRuntimeLogStream as RootPluginRuntimeLogStream } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protobuf message helper types.
import type { MessageInitShape } from "@valon-technologies/gestalt";
// @ts-expect-error Protocol helper subpath is not public.
import type { Struct as ProtocolStruct } from "@valon-technologies/gestalt/protocol";
// @ts-expect-error Generated protocol subpath is not public.
import type { AccessEvaluationRequest as ProtocolRequest } from "@valon-technologies/gestalt/protocol/v1";
// @ts-expect-error Generated agent contract helpers are not public.
import type { agentContractSchemas } from "@valon-technologies/gestalt/test/agent-contract";

const evaluateInput: AuthorizationEvaluateInput = {
  subject: { type: "user", id: "user-1" },
  action: { name: "view" },
  resource: { type: "document", id: "doc-1" },
};
const appendResponse: RuntimeLogAppendResponse = { lastSeq: 1n };
const event: WorkflowEvent = { id: "event-1", type: "fixture.event" };
const workspace: AgentManagerWorkspace = {
  cwd: "/workspace",
  checkouts: [{ url: "https://example.test/repo.git", ref: "main" }],
};
const egressMode: PluginRuntimeEgressMode = PluginRuntimeEgressMode.NONE;

void Authorization;
void RuntimeLogHost;
void WorkflowRunStatus;
void evaluateInput;
void appendResponse;
void event;
void workspace;
void egressMode;
void (undefined as unknown as ProtocolStruct);
void (undefined as unknown as ProtocolRequest);
void (undefined as unknown as typeof agentContractSchemas);
