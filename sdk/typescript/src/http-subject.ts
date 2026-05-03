import type { Access, Credential, Host, MaybePromise, Subject } from "./api.ts";

/**
 * Verified hosted HTTP request metadata passed into optional plugin-local
 * subject resolution hooks before normal operation dispatch.
 */
export interface HTTPSubjectRequest {
  binding: string;
  method: string;
  path: string;
  contentType: string;
  headers: Record<string, string[]>;
  query: Record<string, string[]>;
  params: Record<string, unknown>;
  rawBody: Uint8Array;
  securityScheme: string;
  verifiedSubject: string;
  verifiedClaims: Record<string, string>;
}

/**
 * Request-scoped caller context available while resolving the concrete subject
 * for a hosted HTTP request.
 */
export interface HTTPSubjectResolutionContext {
  subject: Subject;
  credential: Credential;
  access: Access;
  host: Host;
  workflow: Record<string, unknown>;
}

/**
 * Explicit HTTP rejection surfaced from a hosted HTTP subject resolver.
 */
export class HTTPSubjectResolutionError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "HTTPSubjectResolutionError";
    this.status = status;
  }
}

/**
 * Creates an explicit hosted HTTP subject-resolution rejection.
 */
export function httpSubjectError(
  status: number,
  message: string,
): HTTPSubjectResolutionError {
  return new HTTPSubjectResolutionError(status, message);
}

/**
 * Optional hook that maps a verified hosted HTTP request to a concrete Gestalt
 * subject before the target operation is authorized and executed.
 */
export type HTTPSubjectResolver = (
  request: HTTPSubjectRequest,
  context: HTTPSubjectResolutionContext,
) => MaybePromise<Subject | null | undefined>;

export function cloneHTTPSubjectRequest(
  input: HTTPSubjectRequest,
): HTTPSubjectRequest {
  return {
    binding: input.binding,
    method: input.method,
    path: input.path,
    contentType: input.contentType,
    headers: cloneStringLists(input.headers),
    query: cloneStringLists(input.query),
    params: {
      ...input.params,
    },
    rawBody: new Uint8Array(input.rawBody),
    securityScheme: input.securityScheme,
    verifiedSubject: input.verifiedSubject,
    verifiedClaims: {
      ...input.verifiedClaims,
    },
  };
}

export function cloneHTTPSubjectResolutionContext(
  input: HTTPSubjectResolutionContext,
): HTTPSubjectResolutionContext {
  return {
    subject: {
      ...input.subject,
    },
    credential: {
      ...input.credential,
    },
    access: {
      ...input.access,
    },
    host: {
      ...input.host,
    },
    workflow: {
      ...input.workflow,
    },
  };
}

function cloneStringLists(
  input: Record<string, string[]>,
): Record<string, string[]> {
  const output: Record<string, string[]> = {};
  for (const [key, value] of Object.entries(input)) {
    output[key] = [...value];
  }
  return output;
}
