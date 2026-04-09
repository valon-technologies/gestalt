export interface Request {
  token: string;
  connectionParams: Record<string, string>;
}

export interface Response<T> {
  status?: number;
  body: T;
}

export interface OperationResult {
  status: number;
  body: string;
}

export type MaybePromise<T> = T | Promise<T>;

export function ok<T>(body: T): Response<T> {
  return {
    status: 200,
    body,
  };
}

export function request(token = "", connectionParams: Record<string, string> = {}): Request {
  return {
    token,
    connectionParams: {
      ...connectionParams,
    },
  };
}

export function connectionParam(input: Request | undefined, name: string): string {
  return input?.connectionParams[name] ?? "";
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error);
}
