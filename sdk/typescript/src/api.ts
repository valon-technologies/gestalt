export interface Request {
  token: string;
  connectionParams: Record<string, string>;
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

export function request(token = "", connectionParams: Record<string, string> = {}): Request {
  return {
    token,
    connectionParams: {
      ...connectionParams,
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
