import {
  RuntimeEgressMode,
  WorkflowRunStatus,
  type AgentWorkspace,
  type JsonObject,
  type JsonValue,
  type WorkflowEvent,
} from "@valon-technologies/gestalt";

// @ts-expect-error Root package must not expose protocol helper schemas.
import { StructSchema as RootStructSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protocol helper types.
import type { Struct as RootStruct } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose generated protocol schemas.
import { CheckAccessRequestSchema as RootCheckAccessRequestSchema } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose protobuf message helper types.
import type { MessageInitShape } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider metadata wire helpers.
import { connectionModeToProtoValue } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider metadata wire helpers.
import { connectionParamToProto } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose workflow proto conversion helpers.
import { workflowEventTriggerInvocationToProto } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose workflow proto conversion helpers.
import { workflowRunFromProto } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { responseBrand as removed_responseBrand } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { buildProviderBinary as removed_buildProviderBinary } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { bunBuildCommand as removed_bunBuildCommand } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { bunTarget as removed_bunTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { parseBuildArgs as removed_parseBuildArgs } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { CURRENT_PROTOCOL_VERSION as removed_CURRENT_PROTOCOL_VERSION } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { ENV_PROVIDER_PARENT_PID as removed_ENV_PROVIDER_PARENT_PID } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { ENV_PROVIDER_SOCKET as removed_ENV_PROVIDER_SOCKET } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { ENV_WRITE_CATALOG as removed_ENV_WRITE_CATALOG } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { loadProviderFromTarget as removed_loadProviderFromTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { runtimeMain as removed_runtimeMain } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { parseRuntimeArgs as removed_parseRuntimeArgs } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { runBundledProvider as removed_runBundledProvider } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { runLoadedProvider as removed_runLoadedProvider } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { defaultProviderName as removed_defaultProviderName } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { formatModuleTarget as removed_formatModuleTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { formatProviderTarget as removed_formatProviderTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { parseModuleTarget as removed_parseModuleTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { parseProviderTarget as removed_parseProviderTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { readPackageConfig as removed_readPackageConfig } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { readPackageProviderTarget as removed_readPackageProviderTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { resolveProviderImportUrl as removed_resolveProviderImportUrl } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import { resolveProviderModulePath as removed_resolveProviderModulePath } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import type { ModuleTarget as RemovedModuleTarget } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import type { PackageConfig as RemovedPackageConfig } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose provider loader, build, or target plumbing.
import type { ProviderTarget as RemovedProviderTarget } from "@valon-technologies/gestalt";
// @ts-expect-error The runtime subpath exposes only the provider-protocol bridges.
import { parseRuntimeArgs as runtimeSub_parseRuntimeArgs } from "@valon-technologies/gestalt/runtime";
// @ts-expect-error The runtime subpath exposes only the provider-protocol bridges.
import { loadProviderFromTarget as runtimeSub_loadProviderFromTarget } from "@valon-technologies/gestalt/runtime";
// @ts-expect-error The runtime subpath exposes only the provider-protocol bridges.
import { runBundledProvider as runtimeSub_runBundledProvider } from "@valon-technologies/gestalt/runtime";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createIdentityService as removedInternal_createIdentityService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createCacheService as removedInternal_createCacheService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createSecretsService as removedInternal_createSecretsService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createProviderService as removedInternal_createProviderService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createRuntimeService as removedInternal_createRuntimeService } from "@valon-technologies/gestalt";
// @ts-expect-error Root package must not expose internal protocol-service adapters.
import { createS3Service as removedInternal_createS3Service } from "@valon-technologies/gestalt";
// @ts-expect-error Protocol helper subpath is not public.
import type { Struct as ProtocolStruct } from "@valon-technologies/gestalt/protocol";
// @ts-expect-error Generated protocol subpath is not public.
import type { CheckAccessRequest as ProtocolRequest } from "@valon-technologies/gestalt/protocol/v1";
// @ts-expect-error Generated agent contract helpers are not public.
import type { agentContractSchemas } from "@valon-technologies/gestalt/test/agent-contract";

const event: WorkflowEvent = { id: "event-1", type: "fixture.event" };
const workspace: AgentWorkspace = {
  cwd: "/workspace",
  checkouts: [{ url: "https://example.test/repo.git", ref: "main", path: "" }],
};
const jsonObject: JsonObject = { ok: true };
const jsonValue: JsonValue = { nested: ["value"] };
const egressMode: RuntimeEgressMode = RuntimeEgressMode.NONE;

void WorkflowRunStatus;
void event;
void workspace;
void jsonObject;
void jsonValue;
void egressMode;
void (undefined as unknown as ProtocolStruct);
void (undefined as unknown as ProtocolRequest);
void (undefined as unknown as typeof agentContractSchemas);
void connectionModeToProtoValue;
void connectionParamToProto;
void workflowEventTriggerInvocationToProto;
void workflowRunFromProto;
void removed_responseBrand;
void removed_buildProviderBinary;
void removed_bunBuildCommand;
void removed_bunTarget;
void removed_parseBuildArgs;
void removed_CURRENT_PROTOCOL_VERSION;
void removed_ENV_PROVIDER_PARENT_PID;
void removed_ENV_PROVIDER_SOCKET;
void removed_ENV_WRITE_CATALOG;
void removed_loadProviderFromTarget;
void removed_runtimeMain;
void removed_parseRuntimeArgs;
void removed_runBundledProvider;
void removed_runLoadedProvider;
void removed_defaultProviderName;
void removed_formatModuleTarget;
void removed_formatProviderTarget;
void removed_parseModuleTarget;
void removed_parseProviderTarget;
void removed_readPackageConfig;
void removed_readPackageProviderTarget;
void removed_resolveProviderImportUrl;
void removed_resolveProviderModulePath;
declare const useModuleTarget: RemovedModuleTarget; void useModuleTarget;
declare const usePackageConfig: RemovedPackageConfig; void usePackageConfig;
declare const useProviderTarget: RemovedProviderTarget; void useProviderTarget;
void runtimeSub_parseRuntimeArgs;
void runtimeSub_loadProviderFromTarget;
void runtimeSub_runBundledProvider;
void removedInternal_createIdentityService;
void removedInternal_createCacheService;
void removedInternal_createSecretsService;
void removedInternal_createProviderService;
void removedInternal_createRuntimeService;
void removedInternal_createS3Service;
