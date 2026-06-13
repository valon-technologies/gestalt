"""Provider-side error mapping for generated dispatch servicers.

This module is emitted once as _serve/support.py when any service carries the
provider annotation. Generated dispatch adapters call status_error to convert
handler errors to the gRPC status the host receives."""

from __future__ import annotations

from typing import Any

import grpc

from ..rpc_support import GestaltError, GestaltErrorCode


def status_error(context: grpc.ServicerContext, operation: str, err: BaseException) -> Any:
    """Convert one handler error to the gRPC status returned to the host.

    GestaltError carries its code through as the gRPC status code.  An error
    already carrying a gRPC status (grpc.Call) passes through unchanged.  Any
    other error is mapped to UNKNOWN tagged with the operation string.
    """
    if isinstance(err, GestaltError):
        code = grpc.StatusCode.UNKNOWN
        for sc in grpc.StatusCode:
            if sc.value[0] == err.code:
                code = sc
                break
        context.abort(code, err.message)
        return None
    if isinstance(err, grpc.Call):
        context.abort(err.code(), str(err.details() or ""))
        return None
    context.abort(grpc.StatusCode.UNKNOWN, f"{operation}: {err}")
    return None
