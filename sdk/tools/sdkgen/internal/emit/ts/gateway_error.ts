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

interface GatewayErrorObject {
  code?: number | string;
  message?: string;
}

interface GatewayErrorBody {
  code?: number | string;
  message?: string;
  error?: string | GatewayErrorObject;
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
    extractGatewayErrorMessage(parsed) ??
    `request failed with status ${status}`;
  return new GestaltError(code, message);
}

function extractGatewayErrorMessage(
  parsed: GatewayErrorBody | undefined,
): string | undefined {
  if (!parsed) {
    return undefined;
  }
  if (typeof parsed.message === "string" && parsed.message.trim()) {
    return parsed.message;
  }
  if (typeof parsed.error === "string" && parsed.error.trim()) {
    return parsed.error;
  }
  if (
    typeof parsed.error === "object" &&
    parsed.error !== null &&
    typeof parsed.error.message === "string" &&
    parsed.error.message.trim()
  ) {
    return parsed.error.message;
  }
  return undefined;
}

function resolveGatewayErrorCode(
  status: number,
  parsed: GatewayErrorBody | undefined,
): GestaltErrorCodeType {
  if (parsed) {
    if (typeof parsed.code === "number") {
      return parsed.code as GestaltErrorCodeType;
    }
    if (typeof parsed.code === "string") {
      const fromName = grpcCodeNameToGestaltCode(parsed.code);
      if (fromName !== undefined) {
        return fromName;
      }
    }
    if (typeof parsed.error === "object" && parsed.error !== null) {
      if (typeof parsed.error.code === "number") {
        return parsed.error.code as GestaltErrorCodeType;
      }
      if (typeof parsed.error.code === "string") {
        const fromName = grpcCodeNameToGestaltCode(parsed.error.code);
        if (fromName !== undefined) {
          return fromName;
        }
      }
    }
  }
  return httpStatusToGestaltCode(status);
}

function grpcCodeNameToGestaltCode(
  name: string,
): GestaltErrorCodeType | undefined {
  const trimmed = name.trim();
  if (/^\d+$/.test(trimmed)) {
    return Number(trimmed) as GestaltErrorCodeType;
  }
  const direct = trimmed.toUpperCase().replace(/-/g, "_");
  const fromDirect = gestaltCodeFromNormalized(direct);
  if (fromDirect !== undefined) {
    return fromDirect;
  }
  const normalized = trimmed
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/-/g, "_")
    .toUpperCase();
  return gestaltCodeFromNormalized(normalized);
}

function gestaltCodeFromNormalized(
  normalized: string,
): GestaltErrorCodeType | undefined {
  switch (normalized) {
    case "CANCELED":
    case "CANCELLED":
      return GestaltErrorCode.Canceled;
    case "UNKNOWN":
      return GestaltErrorCode.Unknown;
    case "INVALID_ARGUMENT":
      return GestaltErrorCode.InvalidArgument;
    case "DEADLINE_EXCEEDED":
      return GestaltErrorCode.DeadlineExceeded;
    case "NOT_FOUND":
      return GestaltErrorCode.NotFound;
    case "ALREADY_EXISTS":
      return GestaltErrorCode.AlreadyExists;
    case "PERMISSION_DENIED":
      return GestaltErrorCode.PermissionDenied;
    case "RESOURCE_EXHAUSTED":
      return GestaltErrorCode.ResourceExhausted;
    case "FAILED_PRECONDITION":
      return GestaltErrorCode.FailedPrecondition;
    case "ABORTED":
      return GestaltErrorCode.Aborted;
    case "OUT_OF_RANGE":
      return GestaltErrorCode.OutOfRange;
    case "UNIMPLEMENTED":
      return GestaltErrorCode.Unimplemented;
    case "INTERNAL":
      return GestaltErrorCode.Internal;
    case "UNAVAILABLE":
      return GestaltErrorCode.Unavailable;
    case "DATA_LOSS":
      return GestaltErrorCode.DataLoss;
    case "UNAUTHENTICATED":
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
