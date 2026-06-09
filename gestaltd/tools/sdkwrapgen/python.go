package main

import (
	"fmt"
	"strings"
)

func renderPythonAuthorization(ir authorizationIR, outputs []outputConfig) ([]generatedFile, error) {
	if len(outputs) != 1 {
		return nil, fmt.Errorf("%s: python expects exactly one output", ir.Config.Proto)
	}
	path := outputs[0].Path
	var b strings.Builder
	b.Write(generatedHeader(path))
	b.WriteString(`from __future__ import annotations

import datetime as dt
import os
import threading
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Any, Protocol

from google.protobuf import empty_pb2 as _empty_pb2

from ._gen.v1 import authorization_pb2 as _pb
from ._gen.v1 import authorization_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    host_service_channel,
)
from ._protocol import coerce_model as _coerce
from ._protocol import copy_message as _copy
from ._protocol import datetime_from_timestamp as _datetime_from_timestamp
from ._protocol import has_field as _has_field
from ._protocol import struct_from_dict as _struct_from_dict
from ._protocol import struct_to_dict as _struct_to_dict
from ._protocol import timestamp_from_datetime as _timestamp_from_datetime
from ._protocol import which_oneof as _which_oneof

pb: Any = _pb
pb_grpc: Any = _pb_grpc

_shared_authorization_transport: dict[str, Any] = {}
_shared_authorization_lock = threading.Lock()

`)
	for _, enum := range ir.Enums {
		for _, value := range enum.Values {
			b.WriteString(fmt.Sprintf("%s = _pb.%s\n", value.ProtoName, value.ProtoName))
		}
		b.WriteString("\n")
	}
	for _, message := range ir.Messages {
		renderPythonDataclass(&b, message)
	}
	renderPythonClient(&b, ir)
	renderPythonConversions(&b, ir)
	return []generatedFile{{Path: path, Data: []byte(b.String())}}, nil
}

func renderPythonDataclass(b *strings.Builder, message irMessage) {
	if message.Empty {
		return
	}
	b.WriteString("@dataclass(slots=True)\n")
	b.WriteString(fmt.Sprintf("class %s:\n", message.PublicName))
	if message.Oneof != nil {
		for _, field := range message.Oneof.Variants {
			b.WriteString(fmt.Sprintf("    %s: %s | None = None\n", field.PyName, pyType(field)))
		}
		b.WriteString("\n")
		b.WriteString("    def __post_init__(self) -> None:\n")
		b.WriteString("        selected = sum(value is not None for value in (\n")
		for _, field := range message.Oneof.Variants {
			b.WriteString(fmt.Sprintf("            self.%s,\n", field.PyName))
		}
		b.WriteString("        ))\n")
		b.WriteString("        if selected > 1:\n")
		b.WriteString(fmt.Sprintf("            raise ValueError(%q)\n\n", message.PublicName+" accepts exactly one variant"))
		for _, field := range message.Oneof.Variants {
			b.WriteString("    @classmethod\n")
			b.WriteString(fmt.Sprintf("    def from_%s(cls, %s: %s) -> %q:\n", field.PyName, field.PyName, pyType(field), message.PublicName))
			b.WriteString(fmt.Sprintf("        return cls(%s=%s)\n\n", field.PyName, field.PyName))
		}
		return
	}
	if len(message.Fields) == 0 {
		b.WriteString("    pass\n\n\n")
		return
	}
	for _, field := range message.Fields {
		b.WriteString(fmt.Sprintf("    %s: %s = %s\n", field.PyName, pyFieldType(field), pyDefaultValue(field)))
	}
	b.WriteString("\n\n")
}

func renderPythonClient(b *strings.Builder, ir authorizationIR) {
	b.WriteString("class ClientProtocol(Protocol):\n")
	b.WriteString("    def __enter__(self) -> \"ClientProtocol\": ...\n")
	b.WriteString("    def __exit__(self, *args: Any) -> None: ...\n")
	b.WriteString("    def close(self) -> None: ...\n")
	for _, method := range ir.Methods {
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    def %s(self) -> %s: ...\n", method.SnakeName, ir.MessagesByName[method.OutputName].PublicName))
		} else {
			b.WriteString(fmt.Sprintf("    def %s(self, request: %s) -> %s: ...\n", method.SnakeName, ir.MessagesByName[method.InputName].PublicName, ir.MessagesByName[method.OutputName].PublicName))
		}
	}
	b.WriteString("\n\n")
	b.WriteString("class Client:\n")
	b.WriteString(`    def __new__(
        cls,
        socket_target: str | None = None,
        *,
        _token: str | None = None,
        _shared: bool = False,
    ) -> "Client":
        if not _shared and socket_target is None and _token is None:
            return _shared_authorization_client()
        return super().__new__(cls)

    def __init__(
        self,
        socket_target: str | None = None,
        *,
        _token: str | None = None,
        _shared: bool = False,
    ) -> None:
        if getattr(self, "_authorization_initialized", False):
            return
        target = _resolve_authorization_socket_target(socket_target)
        token = (_token if _token is not None else os.environ.get(ENV_HOST_SERVICE_TOKEN, "")).strip()
        self._channel = host_service_channel("authorization", target, token=token)
        self._stub = pb_grpc.AuthorizationProviderStub(self._channel)
        self._closed = False
        self._shared = _shared
        self._authorization_initialized = True

    def close(self) -> None:
        if self._shared:
            return
        self._close_channel()

    def _close_channel(self) -> None:
        if self._closed:
            return
        self._closed = True
        self._channel.close()

`)
	for _, method := range ir.Methods {
		output := ir.MessagesByName[method.OutputName]
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("    def %s(self) -> %s:\n", method.SnakeName, output.PublicName))
			b.WriteString(fmt.Sprintf("        return %s_from_proto(self._stub.%s(getattr(_empty_pb2, \"Empty\")()))\n\n", pyFunctionPrefix(output), method.ProtoName))
			continue
		}
		input := ir.MessagesByName[method.InputName]
		b.WriteString(fmt.Sprintf("    def %s(self, request: %s) -> %s:\n", method.SnakeName, input.PublicName, output.PublicName))
		b.WriteString(fmt.Sprintf("        return %s_from_proto(self._stub.%s(%s_to_proto(request)))\n\n", pyFunctionPrefix(output), method.ProtoName, pyFunctionPrefix(input)))
	}
	b.WriteString(`    def __enter__(self) -> "Client":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _shared_authorization_client() -> Client:
    target = _resolve_authorization_socket_target()
    token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "").strip()
    with _shared_authorization_lock:
        client = _shared_authorization_transport.get("client")
        if client is not None and _shared_authorization_transport.get("target") == target and _shared_authorization_transport.get("token") == token:
            return client

        client = Client(target, _token=token, _shared=True)
        stale = _shared_authorization_transport.get("client")
        _shared_authorization_transport["target"] = target
        _shared_authorization_transport["token"] = token
        _shared_authorization_transport["client"] = client
        if stale is not None:
            stale._close_channel()
        return client


def _resolve_authorization_socket_target(socket_target: str | None = None) -> str:
    target = (socket_target if socket_target is not None else os.environ.get(ENV_HOST_SERVICE_SOCKET, "")).strip()
    if not target:
        raise RuntimeError(f"authorization: {ENV_HOST_SERVICE_SOCKET} is not set")
    return target


`)
}

func renderPythonConversions(b *strings.Builder, ir authorizationIR) {
	for _, message := range ir.Messages {
		if message.Empty {
			continue
		}
		prefix := pyFunctionPrefix(message)
		b.WriteString(fmt.Sprintf("def %s_to_proto(value: Any) -> Any:\n", prefix))
		b.WriteString(fmt.Sprintf("    if isinstance(value, pb.%s):\n        return _copy(value)\n", message.ProtoName))
		if message.Oneof == nil && len(message.Fields) == 0 {
			b.WriteString(fmt.Sprintf("    _coerce(value, %s, %q)\n", message.PublicName, message.PublicName))
			b.WriteString(fmt.Sprintf("    return pb.%s()\n\n", message.ProtoName))
			b.WriteString(fmt.Sprintf("def %s_from_proto(value: Any) -> %s:\n", prefix, message.PublicName))
			b.WriteString(fmt.Sprintf("    return %s()\n\n", message.PublicName))
			continue
		}
		b.WriteString(fmt.Sprintf("    model = _coerce(value, %s, %q)\n", message.PublicName, message.PublicName))
		if message.Oneof != nil {
			b.WriteString(fmt.Sprintf("    selected = sum(value is not None for value in (%s))\n", strings.Join(pySelfFields("model", message.Oneof.Variants), ", ")))
			b.WriteString("    if selected > 1:\n")
			b.WriteString(fmt.Sprintf("        raise ValueError(%q)\n", message.PublicName+" accepts exactly one variant"))
			b.WriteString(fmt.Sprintf("    out = pb.%s()\n", message.ProtoName))
			for _, field := range message.Oneof.Variants {
				b.WriteString(fmt.Sprintf("    if model.%s is not None:\n", field.PyName))
				if field.Kind == irKindMessage {
					b.WriteString(fmt.Sprintf("        out.%s.CopyFrom(%s)\n", field.PyName, pyToProtoExpr(field, "model."+field.PyName)))
				} else {
					b.WriteString(fmt.Sprintf("        out.%s = %s\n", field.PyName, pyToProtoExpr(field, "model."+field.PyName)))
				}
				b.WriteString("        return out\n")
			}
			b.WriteString("    return out\n\n")
			b.WriteString(fmt.Sprintf("def %s_from_proto(value: Any) -> %s:\n", prefix, message.PublicName))
			b.WriteString("    selected = _which_oneof(value, \"kind\")\n")
			for i, field := range message.Oneof.Variants {
				keyword := "if"
				if i > 0 {
					keyword = "elif"
				}
				b.WriteString(fmt.Sprintf("    %s selected == %q:\n", keyword, field.PyName))
				b.WriteString(fmt.Sprintf("        return %s(%s=%s)\n", message.PublicName, field.PyName, pyFromProtoExpr(field, "getattr(value, "+fmt.Sprintf("%q", field.PyName)+")")))
			}
			b.WriteString(fmt.Sprintf("    return %s()\n\n", message.PublicName))
			continue
		}
		b.WriteString(fmt.Sprintf("    return pb.%s(\n", message.ProtoName))
		for _, field := range message.Fields {
			b.WriteString(fmt.Sprintf("        %s=%s,\n", field.PyName, pyToProtoExpr(field, "model."+field.PyName)))
		}
		b.WriteString("    )\n\n")
		b.WriteString(fmt.Sprintf("def %s_from_proto(value: Any) -> %s:\n", prefix, message.PublicName))
		b.WriteString(fmt.Sprintf("    return %s(\n", message.PublicName))
		for _, field := range message.Fields {
			b.WriteString(fmt.Sprintf("        %s=%s,\n", field.PyName, pyFromProtoExpr(field, "value."+field.PyName)))
		}
		b.WriteString("    )\n\n")
	}
}

func pySelfFields(receiver string, fields []irField) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, receiver+"."+field.PyName)
	}
	return out
}

func pyType(field irField) string {
	switch field.Kind {
	case irKindString:
		return "str"
	case irKindBool:
		return "bool"
	case irKindInt32, irKindEnum:
		return "int"
	case irKindJSON:
		return "Mapping[str, Any]"
	case irKindTimestamp:
		return "dt.datetime"
	case irKindMessage:
		return publicMessageName(field.MessageName)
	default:
		return "Any"
	}
}

func pyFieldType(field irField) string {
	base := pyType(field)
	if field.Repeated {
		return "list[" + base + "]"
	}
	if field.Kind == irKindMessage || field.Kind == irKindJSON || field.Kind == irKindTimestamp {
		return base + " | None"
	}
	return base
}

func pyDefaultValue(field irField) string {
	if field.Repeated {
		return "field(default_factory=list)"
	}
	switch field.Kind {
	case irKindString:
		return `""`
	case irKindBool:
		return "False"
	case irKindInt32:
		return "0"
	case irKindEnum:
		if field.DefaultEnumName != "" {
			return enumValuePrefix(field.EnumName) + field.DefaultEnumName
		}
		return "0"
	default:
		return "None"
	}
}

func pyToProtoExpr(field irField, expr string) string {
	if field.Repeated {
		item := "item"
		return fmt.Sprintf("[%s for %s in %s]", pyToProtoExpr(irField{Kind: field.Kind, MessageName: field.MessageName, EnumName: field.EnumName}, item), item, expr)
	}
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s_to_proto(%s) if %s is not None else None", snakeName(field.MessageName), expr, expr)
	case irKindJSON:
		return fmt.Sprintf("_struct_from_dict(%s) if %s is not None else None", expr, expr)
	case irKindTimestamp:
		return fmt.Sprintf("_timestamp_from_datetime(%s) if %s is not None else None", expr, expr)
	default:
		return expr
	}
}

func pyFromProtoExpr(field irField, expr string) string {
	if field.Repeated {
		item := "item"
		itemField := irField{Kind: field.Kind, MessageName: field.MessageName, EnumName: field.EnumName}
		switch field.Kind {
		case irKindMessage:
			return fmt.Sprintf("[%s_from_proto(%s) for %s in %s]", snakeName(field.MessageName), item, item, expr)
		default:
			return fmt.Sprintf("[%s for %s in %s]", pyFromProtoExpr(itemField, item), item, expr)
		}
	}
	switch field.Kind {
	case irKindMessage:
		return fmt.Sprintf("%s_from_proto(%s) if _has_field(value, %q) else None", snakeName(field.MessageName), expr, field.PyName)
	case irKindJSON:
		return fmt.Sprintf("_struct_to_dict(%s) if _has_field(value, %q) else None", expr, field.PyName)
	case irKindTimestamp:
		return fmt.Sprintf("_datetime_from_timestamp(%s) if _has_field(value, %q) else None", expr, field.PyName)
	default:
		return expr
	}
}

func pyFunctionPrefix(message irMessage) string {
	return snakeName(message.ProtoName)
}
