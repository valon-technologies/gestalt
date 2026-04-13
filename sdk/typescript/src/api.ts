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
  subject: Subject;
  credential: Credential;
}

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
  };
}

export function connectionParam(input: Request | undefined, name: string): string | undefined {
  return input?.connectionParams[name];
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error);
}
