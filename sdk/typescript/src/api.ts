/**
 * Common request and response types shared across authored Gestalt providers.
 */
export interface Subject {
  id: string;
  kind: string;
  displayName: string;
  authSource: string;
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
 * Request metadata forwarded to provider handlers by the Gestalt runtime.
 */
export interface Request {
  token: string;
  connectionParams: Record<string, string>;
  subject: Subject;
  credential: Credential;
  access: Access;
  // Workflow callback metadata uses a JSON-style lowerCamelCase object such as
  // runId, target.pluginName, trigger.scheduleId, and trigger.event.specVersion.
  workflow: Record<string, unknown>;
  requestHandle: string;
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
  body: T;
}

/**
 * Serialized operation result returned by the protocol runtime.
 */
export interface OperationResult {
  status: number;
  body: string;
}

/**
 * Value or promise-like return accepted by provider handlers.
 */
export type MaybePromise<T> = T | Promise<T>;

/**
 * Wraps a handler result with an explicit status code.
 */
export function response<T>(status: number, body: T): Response<T> {
  return {
    [responseBrand]: true,
    status,
    body,
  };
}

/**
 * Wraps a handler result with the default `200` status code.
 */
export function ok<T>(body: T): Response<T> {
  return response(200, body);
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
  requestHandle = "",
  workflow: Record<string, unknown> = {},
): Request {
  return {
    token,
    connectionParams: {
      ...connectionParams,
    },
    subject: {
      id: subject.id ?? "",
      kind: subject.kind ?? "",
      displayName: subject.displayName ?? "",
      authSource: subject.authSource ?? "",
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
    requestHandle,
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
