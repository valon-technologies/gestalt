/**
 * Common request and response types shared across authored Gestalt providers.
 */
import {
  type BoundGestalt,
  type RequestGestaltOptions,
} from "./bound-gestalt.ts";
import {
  attachRequestGestaltScope,
} from "./bound-gestalt-scope.ts";
import type { App, RequestContext } from "./app.ts";
import type { AgentToolRef } from "./providers/agent.ts";

export interface Subject {
  id: string;
  email: string;
  scopes?: string[] | undefined;
  permissions?: SubjectPermission[] | undefined;
  kind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

export interface SubjectPermission {
  app: string;
  operations: string[];
}

/**
 * Subject payload authored by provider-side hooks.
 */
export interface SubjectInput {
  id: string;
  email?: string | undefined;
  displayName?: string | undefined;
  scopes?: readonly string[] | undefined;
  permissions?: readonly SubjectPermission[] | undefined;
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
  credential: Credential;
  access: Access;
  host: Host;
  idempotencyKey: string;
  // Workflow callback metadata uses a JSON-style lowerCamelCase object such as
  // runId, target.steps, trigger.activationId, and trigger.event.specVersion.
  workflow: Record<string, unknown>;
  toolRefs: AgentToolRef[];
  toolRefsSet: boolean;
  __requestContext?: RequestContext | undefined;
  /** Returns the bound Gestalt client for nested host-service calls. */
  gestalt(options?: RequestGestaltOptions): Promise<BoundGestalt>;
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
  headers: ResponseHeaders;
  body: T;
}

/**
 * HTTP response headers returned by an operation handler.
 */
export type ResponseHeaders = Readonly<Record<string, string | readonly string[]>>;

/**
 * Serialized operation result returned by the protocol runtime.
 */
export interface OperationResult {
  status: number;
  headers: Record<string, string[]>;
  body: Uint8Array;
  ok: boolean;
  bytes(): Uint8Array;
  text(): string;
  json<T = unknown>(): T;
  requireOk(): OperationResult;
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
    headers: headers ?? {},
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
export function parseSubjectId(
  subjectId: string,
): { kind: string; id: string } | undefined {
  const trimmed = subjectId.trim();
  const index = trimmed.indexOf(":");
  if (index <= 0 || index >= trimmed.length - 1) {
    return undefined;
  }
  const kind = trimmed.slice(0, index).trim();
  const id = trimmed.slice(index + 1).trim();
  if (!kind || !id) {
    return undefined;
  }
  return { kind, id };
}

export function request(
  token = "",
  connectionParams: Record<string, string> = {},
  subject: Partial<Subject> = {},
  credential: Partial<Credential> = {},
  access: Partial<Access> = {},
  workflow: Record<string, unknown> = {},
  idempotencyKey = "",
  host: Partial<Host> = {},
  agentSubject: Partial<Subject> = {},
  toolRefs: readonly AgentToolRef[] = [],
  toolRefsSet = false,
  requestContext?: RequestContext,
): Request {
  const req: Omit<Request, "gestalt"> = {
    token,
    connectionParams: {
      ...connectionParams,
    },
    subject: {
      id: subject.id ?? "",
      email: subject.email ?? "",
      scopes: [...(subject.scopes ?? [])],
      permissions: cloneSubjectPermissions(subject.permissions),
      kind: subject.kind,
      displayName: subject.displayName,
      authSource: subject.authSource,
    },
    agentSubject: {
      id: agentSubject.id ?? "",
      email: agentSubject.email ?? "",
      scopes: [...(agentSubject.scopes ?? [])],
      permissions: cloneSubjectPermissions(agentSubject.permissions),
      kind: agentSubject.kind,
      displayName: agentSubject.displayName,
      authSource: agentSubject.authSource,
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
      runAs: ref.runAs === undefined ? undefined : cloneSubjectInput(ref.runAs),
    })),
    toolRefsSet,
    host: {
      publicBaseUrl: host.publicBaseUrl ?? "",
    },
    __requestContext: requestContext,
    idempotencyKey: idempotencyKey.trim(),
  };
  return attachRequestHelpers(req, "");
}

export function attachRequestHelpers<T extends Omit<Request, "gestalt">>(
  input: T,
  capability: string,
): Request {
  const req = input as unknown as Request;
  attachRequestGestaltScope(req, capability);
  return req;
}

export function cloneSubjectInput<T extends SubjectInput | Subject>(subject: T): T {
  return {
    ...subject,
    scopes: [...(subject.scopes ?? [])],
    permissions: cloneSubjectPermissions(subject.permissions),
  };
}

export function cloneSubjectPermissions(
  permissions?: readonly SubjectPermission[] | undefined,
): SubjectPermission[] {
  return permissions?.map((permission) => ({
    app: permission.app,
    operations: [...permission.operations],
  })) ?? [];
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
