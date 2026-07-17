/**
 * Canonical gateway error parsing for the public REST transport.
 *
 * @module client/generated/gateway_error
 */

import {
  GestaltError,
  GestaltErrorCode,
  type GestaltErrorCode as GestaltErrorCodeType,
} from __RPC_SUPPORT_IMPORT__;

interface GatewayErrorBody {
  code?: number | string;
  message?: string;
  error?: string;
}

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
  const code = resolveGatewayErrorCode(status, parsed);
  const message =
    (typeof parsed?.message === "string" && parsed.message.trim()) ||
    (typeof parsed?.error === "string" && parsed.error.trim()) ||
    `request failed with status ${status}`;
  return new GestaltError(code, message);
}

function resolveGatewayErrorCode(
  status: number,
  parsed: GatewayErrorBody | undefined,
): GestaltErrorCodeType {
  if (typeof parsed?.code === "number") {
    return parsed.code as GestaltErrorCodeType;
  }
  if (typeof parsed?.code === "string") {
    const fromName = grpcCodeNameToGestaltCode(parsed.code);
    if (fromName !== undefined) {
      return fromName;
    }
  }
  return httpStatusToGestaltCode(status);
}

function grpcCodeNameToGestaltCode(
  name: string,
): GestaltErrorCodeType | undefined {
  switch (name) {
    case "Canceled":
      return GestaltErrorCode.Canceled;
    case "Unknown":
      return GestaltErrorCode.Unknown;
    case "InvalidArgument":
      return GestaltErrorCode.InvalidArgument;
    case "DeadlineExceeded":
      return GestaltErrorCode.DeadlineExceeded;
    case "NotFound":
      return GestaltErrorCode.NotFound;
    case "AlreadyExists":
      return GestaltErrorCode.AlreadyExists;
    case "PermissionDenied":
      return GestaltErrorCode.PermissionDenied;
    case "ResourceExhausted":
      return GestaltErrorCode.ResourceExhausted;
    case "FailedPrecondition":
      return GestaltErrorCode.FailedPrecondition;
    case "Aborted":
      return GestaltErrorCode.Aborted;
    case "OutOfRange":
      return GestaltErrorCode.OutOfRange;
    case "Unimplemented":
      return GestaltErrorCode.Unimplemented;
    case "Internal":
      return GestaltErrorCode.Internal;
    case "Unavailable":
      return GestaltErrorCode.Unavailable;
    case "DataLoss":
      return GestaltErrorCode.DataLoss;
    case "Unauthenticated":
      return GestaltErrorCode.Unauthenticated;
    default:
      return undefined;
  }
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
