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
import {
  StructSchema,
  structFromObject,
  timestampFromDate,
  valueFromJson,
  type Struct,
} from "@valon-technologies/gestalt/protocol";
import {
  AccessEvaluationRequestSchema,
  type AccessEvaluationRequest,
} from "@valon-technologies/gestalt/protocol/v1";

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
const struct = structFromObject({ ok: true });
type StructMessage = Struct;
type ProtocolRequest = AccessEvaluationRequest;
const egressMode: PluginRuntimeEgressMode = PluginRuntimeEgressMode.NONE;

void Authorization;
void RuntimeLogHost;
void WorkflowRunStatus;
void evaluateInput;
void appendResponse;
void event;
void workspace;
void egressMode;
void StructSchema;
void struct;
void (undefined as unknown as StructMessage);
void timestampFromDate(new Date(0));
void valueFromJson("ok");
void AccessEvaluationRequestSchema;
void (undefined as unknown as ProtocolRequest);
