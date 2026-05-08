"""Public low-level access to Gestalt v1 protobuf modules.

These names are aliases for the SDK's generated transport modules. They are
provided for advanced integration tests and protocol fixtures that need service
stubs or servicer base classes.
"""

from __future__ import annotations

from ..._gen.v1 import (
    agent_pb2,
    agent_pb2_grpc,
    authentication_pb2,
    authentication_pb2_grpc,
    authorization_pb2,
    authorization_pb2_grpc,
    cache_pb2,
    cache_pb2_grpc,
    datastore_pb2,
    datastore_pb2_grpc,
    plugin_pb2,
    plugin_pb2_grpc,
    pluginruntime_pb2,
    pluginruntime_pb2_grpc,
    runtime_pb2,
    runtime_pb2_grpc,
    s3_pb2,
    s3_pb2_grpc,
    secrets_pb2,
    secrets_pb2_grpc,
    workflow_pb2,
    workflow_pb2_grpc,
)

__all__ = [
    "agent_pb2",
    "agent_pb2_grpc",
    "authentication_pb2",
    "authentication_pb2_grpc",
    "authorization_pb2",
    "authorization_pb2_grpc",
    "cache_pb2",
    "cache_pb2_grpc",
    "datastore_pb2",
    "datastore_pb2_grpc",
    "plugin_pb2",
    "plugin_pb2_grpc",
    "pluginruntime_pb2",
    "pluginruntime_pb2_grpc",
    "runtime_pb2",
    "runtime_pb2_grpc",
    "s3_pb2",
    "s3_pb2_grpc",
    "secrets_pb2",
    "secrets_pb2_grpc",
    "workflow_pb2",
    "workflow_pb2_grpc",
]
