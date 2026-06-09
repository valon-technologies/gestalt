import {
  Authorization,
  RuntimeEgressMode,
  RuntimeLogHost,
  WorkflowRunStatus,
  type AgentWorkspace,
  type JsonObject,
  type JsonValue,
  type RuntimeLogAppendResponse,
  type WorkflowEvent,
} from "@valon-technologies/gestalt";

// @ts-expect-error Root package must not expose protocol helper schemas.
import { StructSchema as RootStructSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protocol helper types.
import type { Struct as RootStruct } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated protocol schemas.
import { CheckAccessRequestSchema as RootCheckAccessRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose runtime-log generated request schemas.
import { AppendRuntimeLogsRequestSchema as RootAppendRuntimeLogsRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose runtime-log generated enums.
import { RuntimeLogStream as RootRuntimeLogStream } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protobuf message helper types.
import type { MessageInitShape } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider metadata wire helpers.
import { connectionModeToProtoValue } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider metadata wire helpers.
import { connectionParamToProto } from "@valon-technologies/gestalt";
// @ts-expect-error Protocol helper subpath is not public.
import type { Struct as ProtocolStruct } from "@valon-technologies/gestalt/protocol";
// @ts-expect-error Generated protocol subpath is not public.
import type { CheckAccessRequest as ProtocolRequest } from "@valon-technologies/gestalt/protocol/v1";
// @ts-expect-error Generated agent contract helpers are not public.
import type { agentContractSchemas } from "@valon-technologies/gestalt/test/agent-contract";

const appendResponse: RuntimeLogAppendResponse = { lastSeq: 1n };
const event: WorkflowEvent = { id: "event-1", type: "fixture.event" };
const workspace: AgentWorkspace = {
  cwd: "/workspace",
  checkouts: [{ url: "https://example.test/repo.git", ref: "main" }],
};
const jsonObject: JsonObject = { ok: true };
const jsonValue: JsonValue = { nested: ["value"] };
const egressMode: RuntimeEgressMode = RuntimeEgressMode.NONE;
const authorizationSubject: Authorization.SubjectInput = {
  type: "user",
  id: "user-1",
};
const authorizationOptions: Authorization.Options = {
  target: "tcp://127.0.0.1:8080",
};
const authorizationFactory: (
  options?: Authorization.Options,
) => Authorization.Client = Authorization;

void RuntimeLogHost;
void WorkflowRunStatus;
void appendResponse;
void event;
void workspace;
void jsonObject;
void jsonValue;
void egressMode;
void authorizationSubject;
void authorizationOptions;
void authorizationFactory;
void (undefined as unknown as ProtocolStruct);
void (undefined as unknown as ProtocolRequest);
void (undefined as unknown as typeof agentContractSchemas);
void connectionModeToProtoValue;
void connectionParamToProto;
