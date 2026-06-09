import {
  Authorization,
  RuntimeEgressMode,
  RuntimeLogHost,
  WorkflowRunStatus,
  type AgentWorkspace,
  type JsonObject,
  type JsonValue,
  type RuntimeLogAppendResponse,
  type Subject,
  type WorkflowEvent,
} from "@valon-technologies/gestalt";

// @ts-expect-error Root package must not expose protocol helper schemas.
import { StructSchema as RootStructSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protocol helper types.
import type { Struct as RootStruct } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated protocol schemas.
import { CheckAccessRequestSchema as RootCheckAccessRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose authorization request aliases; use Authorization.CheckAccessRequest.
import type { CheckAccessRequest as RootAuthorizationCheckAccessRequest } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose authorization subject aliases; use Authorization.Subject.
import type { AuthorizationSubject as RootAuthorizationSubject } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose authorization enum aliases; use Authorization.SourceLayer.
import { SourceLayer as RootAuthorizationSourceLayer } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose the old Authorization provider SDK.
import { defineAuthorizationProvider as RootDefineAuthorizationProvider } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose the old Authorization provider runtime bridge.
import { createAuthorizationProviderService as RootCreateAuthorizationProviderService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose the old Authorization provider class.
import { AuthorizationProvider as RootAuthorizationProvider } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated grpc-js authorization clients.
import { AuthorizationProviderClient as RootAuthorizationProviderClient } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated grpc-js authorization service descriptors.
import { AuthorizationProviderService as RootAuthorizationProviderService } from "@valon-technologies/gestalt";
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
const authorizationOptions: Authorization.Options = {
  transport: "grpc",
  target: "tcp://127.0.0.1:1",
  relayToken: "relay-token",
};
const authorizationSubject: Authorization.Subject = {
  type: "user",
  id: "user-123",
  properties: { tenant: "acme" },
};
const subject: Subject = {
  id: "subject-1",
  credentialSubjectId: "credential-subject-1",
  email: "subject@example.test",
};
const jsonObject: JsonObject = { ok: true };
const jsonValue: JsonValue = { nested: ["value"] };
const egressMode: RuntimeEgressMode = RuntimeEgressMode.NONE;

void Authorization;
void RuntimeLogHost;
void WorkflowRunStatus;
void appendResponse;
void event;
void workspace;
void authorizationOptions;
void authorizationSubject;
void subject;
void jsonObject;
void jsonValue;
void egressMode;
void RootDefineAuthorizationProvider;
void RootCreateAuthorizationProviderService;
void RootAuthorizationProvider;
void RootAuthorizationProviderClient;
void RootAuthorizationProviderService;
void (undefined as unknown as RootAuthorizationCheckAccessRequest);
void (undefined as unknown as RootAuthorizationSubject);
void RootAuthorizationSourceLayer;
void (undefined as unknown as ProtocolStruct);
void (undefined as unknown as ProtocolRequest);
void (undefined as unknown as typeof agentContractSchemas);
void connectionModeToProtoValue;
void connectionParamToProto;
