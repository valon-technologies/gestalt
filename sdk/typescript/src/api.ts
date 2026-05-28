/**
 * Common request and response types shared across authored Gestalt providers.
 */
import type { AgentToolRef } from "./agent.ts";

export interface Subject {
  id: string;
  kind: string;
  credentialSubjectId: string;
  displayName: string;
  authSource: string;
  email: string;
}

/**
 * Subject payload authored by provider-side hooks.
 */
export interface SubjectInput {
  id: string;
  kind: string;
  credentialSubjectId?: string | undefined;
  displayName: string;
  authSource: string;
  email?: string | undefined;
}

/**
 * Provider-owned external identity attached to an incoming provider request.
 */
export interface ExternalIdentity {
  type: string;
  id: string;
}

/**
 * Describes the credential Gestalt used to authorize the current request.
 */
export interface Credential {
  mode: string;
  subjectId: string;
  connection: string;
  instance: string;
}

/**
 * Describes the access policy and effective role for the current request.
 */
export interface Access {
  policy: string;
  role: string;
}

/**
 * Describes public host metadata available to provider code.
 */
export interface Host {
  publicBaseUrl: string;
}

/**
 * Request metadata forwarded to provider handlers by the Gestalt runtime.
 */
export interface Request {
  token: string;
  connectionParams: Record<string, string>;
  subject: Subject;
  agentSubject: Subject;
  externalIdentity: ExternalIdentity;
  agentExternalIdentity: ExternalIdentity;
  credential: Credential;
  access: Access;
  host: Host;
  idempotencyKey: string;
  // Workflow callback metadata uses a JSON-style lowerCamelCase object such as
  // runId, target.steps, trigger.scheduleId, and trigger.event.specVersion.
  workflow: Record<string, unknown>;
  toolRefs: AgentToolRef[];
  toolRefsSet: boolean;
  invocationToken: string;
}

/**
 * Internal discriminator used by {@link response} and {@link ok}.
 */
export const responseBrand: unique symbol = Symbol("gestalt.response");

/**
 * Explicit handler response with an optional HTTP status override.
 */
export interface Response<T> {
  readonly [responseBrand]: true;
  status?: number;
  headers?: ResponseHeaders;
  body: T;
}

/**
 * HTTP response headers returned by an operation handler.
 */
export type ResponseHeaders = Readonly<Record<string, string | readonly string[]>>;

/**
 * Serialized HTTP response headers returned by the protocol runtime.
 */
export type OperationResultHeaders = Record<string, string[]>;

/**
 * Serialized operation result returned by the protocol runtime.
 */
export interface OperationResult {
  status: number;
  headers?: OperationResultHeaders;
  body: string;
}

/**
 * Value or promise-like return accepted by provider handlers.
 */
export type MaybePromise<T> = T | Promise<T>;

/**
 * Wraps a handler result with an explicit status code.
 */
export function response<T>(
  status: number,
  body: T,
  headers?: ResponseHeaders,
): Response<T> {
  return {
    [responseBrand]: true,
    status,
    ...(headers === undefined ? {} : { headers }),
    body,
  };
}

/**
 * Wraps a handler result with the default `200` status code.
 */
export function ok<T>(body: T, headers?: ResponseHeaders): Response<T> {
  return response(200, body, headers);
}

/**
 * Creates a request object for local testing or direct provider invocation.
 *
 * @example
 * ```ts
 * import { request } from "@valon-technologies/gestalt";
 *
 * const input = request("token", { region: "us-east-1" }, { id: "usr_123" });
 * ```
 */
export function request(
  token = "",
  connectionParams: Record<string, string> = {},
  subject: Partial<Subject> = {},
  credential: Partial<Credential> = {},
  access: Partial<Access> = {},
  workflow: Record<string, unknown> = {},
  invocationToken = "",
  idempotencyKey = "",
  host: Partial<Host> = {},
  agentSubject: Partial<Subject> = {},
  externalIdentity: Partial<ExternalIdentity> = {},
  agentExternalIdentity: Partial<ExternalIdentity> = {},
  toolRefs: readonly AgentToolRef[] = [],
  toolRefsSet = false,
): Request {
  return {
    token,
    connectionParams: {
      ...connectionParams,
    },
    subject: {
      id: subject.id ?? "",
      kind: subject.kind ?? "",
      credentialSubjectId: subject.credentialSubjectId ?? "",
      displayName: subject.displayName ?? "",
      authSource: subject.authSource ?? "",
      email: subject.email ?? "",
    },
    agentSubject: {
      id: agentSubject.id ?? "",
      kind: agentSubject.kind ?? "",
      credentialSubjectId: agentSubject.credentialSubjectId ?? "",
      displayName: agentSubject.displayName ?? "",
      authSource: agentSubject.authSource ?? "",
      email: agentSubject.email ?? "",
    },
    externalIdentity: {
      type: externalIdentity.type ?? "",
      id: externalIdentity.id ?? "",
    },
    agentExternalIdentity: {
      type: agentExternalIdentity.type ?? "",
      id: agentExternalIdentity.id ?? "",
    },
    credential: {
      mode: credential.mode ?? "",
      subjectId: credential.subjectId ?? "",
      connection: credential.connection ?? "",
      instance: credential.instance ?? "",
    },
    access: {
      policy: access.policy ?? "",
      role: access.role ?? "",
    },
    workflow: {
      ...workflow,
    },
    toolRefs: toolRefs.map((ref) => ({
      ...ref,
      runAs: ref.runAs === undefined ? undefined : { ...ref.runAs },
      runAsExternalIdentity:
        ref.runAsExternalIdentity === undefined
          ? undefined
          : { ...ref.runAsExternalIdentity },
    })),
    toolRefsSet,
    host: {
      publicBaseUrl: host.publicBaseUrl ?? "",
    },
    invocationToken,
    idempotencyKey: idempotencyKey.trim(),
  };
}

/**
 * Looks up a single connection parameter from a request.
 */
export function connectionParam(
  input: Request | undefined,
  name: string,
): string | undefined {
  return input?.connectionParams[name];
}

/**
 * Normalizes unknown thrown values into a readable error message.
 */
export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error);
}
