/**
 * Gestalt public transport errors.
 */

import { ConnectError } from "@connectrpc/connect";

import {
  GestaltError,
  GestaltErrorCode,
  type GestaltErrorCode as GestaltErrorCodeType,
} from "../rpc_support.ts";

export { GestaltError, GestaltErrorCode };
export type { GestaltErrorCodeType };

interface GatewayErrorBody {
  code?: number;
  message?: string;
  details?: unknown[];
}

/**
 * Parses a grpc-gateway JSON error body into GestaltError.
 */
export function parseGatewayError(
  status: number,
  bodyText: string,
): GestaltError {
  let parsed: GatewayErrorBody | undefined;
  if (bodyText.trim() !== "") {
    try {
      parsed = JSON.parse(bodyText) as GatewayErrorBody;
    } catch {
      parsed = undefined;
    }
  }
  const code =
    typeof parsed?.code === "number"
      ? (parsed.code as GestaltErrorCodeType)
      : httpStatusToGestaltCode(status);
  const message =
    (typeof parsed?.message === "string" && parsed.message.trim()) ||
    `request failed with status ${status}`;
  return new GestaltError(code, message);
}

export function toGestaltError(error: unknown): GestaltError {
  if (error instanceof GestaltError) {
    return error;
  }
  if (error instanceof ConnectError) {
    return new GestaltError(error.code as GestaltErrorCodeType, error.rawMessage, {
      cause: error,
    });
  }
  return new GestaltError(GestaltErrorCode.Unknown, String(error), {
    cause: error,
  });
}

function httpStatusToGestaltCode(status: number): GestaltErrorCodeType {
  switch (status) {
    case 400:
      return GestaltErrorCode.InvalidArgument;
    case 401:
      return GestaltErrorCode.Unauthenticated;
    case 403:
      return GestaltErrorCode.PermissionDenied;
    case 404:
      return GestaltErrorCode.NotFound;
    case 409:
      return GestaltErrorCode.AlreadyExists;
    case 412:
      return GestaltErrorCode.FailedPrecondition;
    case 429:
      return GestaltErrorCode.ResourceExhausted;
    case 499:
      return GestaltErrorCode.Canceled;
    case 500:
      return GestaltErrorCode.Internal;
    case 501:
      return GestaltErrorCode.Unimplemented;
    case 503:
      return GestaltErrorCode.Unavailable;
    case 504:
      return GestaltErrorCode.DeadlineExceeded;
    default:
      return GestaltErrorCode.Unknown;
  }
}
