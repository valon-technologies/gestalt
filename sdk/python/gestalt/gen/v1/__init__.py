# Generated Python protobuf modules for gestalt.provider.v1.

from importlib import import_module

__all__ = [
    "agent_pb2",
    "agent_pb2_grpc",
    "authentication_pb2",
    "authentication_pb2_grpc",
    "cache_pb2",
    "cache_pb2_grpc",
    "datastore_pb2",
    "datastore_pb2_grpc",
    "plugin_pb2",
    "plugin_pb2_grpc",
    "runtime_pb2",
    "runtime_pb2_grpc",
    "s3_pb2",
    "s3_pb2_grpc",
    "secrets_pb2",
    "secrets_pb2_grpc",
    "workflow_pb2",
    "workflow_pb2_grpc",
]


def __getattr__(name: str):
    if name not in __all__:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    module = import_module(f".{name}", __name__)
    globals()[name] = module
    return module
