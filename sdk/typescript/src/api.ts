export interface Subject {
  id: string;
  kind: string;
  displayName: string;
  authSource: string;
}

export interface Credential {
  mode: string;
  subjectId: string;
  connection: string;
  instance: string;
}

export interface Request {
  token: string;
  connectionParams: Record<string, string>;
}

type RequestContext = {
  subject: Subject;
  credential: Credential;
};

const requestContextBrand: unique symbol = Symbol("gestalt.requestContext");

type InternalRequest = Request & {
  [requestContextBrand]?: RequestContext;
};

export const responseBrand: unique symbol = Symbol("gestalt.response");

export interface Response<T> {
  readonly [responseBrand]: true;
  status?: number;
  body: T;
}

export interface OperationResult {
  status: number;
  body: string;
}

export type MaybePromise<T> = T | Promise<T>;

function normalizeSubject(subject: Partial<Subject> = {}): Subject {
  return {
    id: subject.id ?? "",
    kind: subject.kind ?? "",
    displayName: subject.displayName ?? "",
    authSource: subject.authSource ?? "",
  };
}

function normalizeCredential(credential: Partial<Credential> = {}): Credential {
  return {
    mode: credential.mode ?? "",
    subjectId: credential.subjectId ?? "",
    connection: credential.connection ?? "",
    instance: credential.instance ?? "",
  };
}

function withRequestContext(
  request: Request,
  subject: Partial<Subject> = {},
  credential: Partial<Credential> = {},
): Request {
  Object.defineProperty(request as InternalRequest, requestContextBrand, {
    value: {
      subject: normalizeSubject(subject),
      credential: normalizeCredential(credential),
    },
    enumerable: false,
  });
  return request;
}

export function response<T>(status: number, body: T): Response<T> {
  return {
    [responseBrand]: true,
    status,
    body,
  };
}

export function ok<T>(body: T): Response<T> {
  return response(200, body);
}

export function request(
  token = "",
  connectionParams: Record<string, string> = {},
  subject: Partial<Subject> = {},
  credential: Partial<Credential> = {},
): Request {
  return withRequestContext(
    {
      token,
      connectionParams: {
        ...connectionParams,
      },
    },
    subject,
    credential,
  );
}

export function requestSubject(input: Request | undefined): Subject {
  return (
    (input as InternalRequest | undefined)?.[requestContextBrand]?.subject ??
    normalizeSubject()
  );
}

export function requestCredential(input: Request | undefined): Credential {
  return (
    (input as InternalRequest | undefined)?.[requestContextBrand]?.credential ??
    normalizeCredential()
  );
}

export function connectionParam(
  input: Request | undefined,
  name: string,
): string | undefined {
  return input?.connectionParams[name];
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error);
}
