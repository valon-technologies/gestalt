from __future__ import annotations

import builtins as _builtins
import datetime as _dt
import importlib
import os
from collections.abc import Iterator, Sequence
from typing import Any, overload

import grpc
from google.protobuf.descriptor import FieldDescriptor as _FieldDescriptor

from ._indexeddb import AlreadyExistsError, NotFoundError

ENV_FILEAPI_SOCKET = "GESTALT_FILEAPI_SOCKET"


def fileapi_socket_env(name: str | None = None) -> str:
    trimmed = (name or "").strip()
    if not trimmed:
        return ENV_FILEAPI_SOCKET
    normalized = "".join(ch.upper() if ch.isalnum() else "_" for ch in trimmed)
    return f"{ENV_FILEAPI_SOCKET}_{normalized}"


def _require_generated_fileapi_modules() -> tuple[Any, Any]:
    try:
        pb = importlib.import_module(".gen.v1.fileapi_pb2", __package__)
        pb_grpc = importlib.import_module(".gen.v1.fileapi_pb2_grpc", __package__)
    except ImportError as error:
        raise RuntimeError("generated FileAPI protobuf stubs are unavailable") from error
    return pb, pb_grpc


def _proto_type(module: Any, *names: str) -> Any:
    for name in names:
        value = getattr(module, name, None)
        if value is not None:
            return value
    return None


def _require_proto_type(module: Any, *names: str) -> Any:
    value = _proto_type(module, *names)
    if value is None:
        joined = ", ".join(names)
        raise RuntimeError(f"generated FileAPI protobuf stubs do not define any of: {joined}")
    return value


def _message_fields(message: Any) -> dict[str, _FieldDescriptor]:
    descriptor = getattr(message, "DESCRIPTOR", None)
    if descriptor is None:
        return {}
    return dict(descriptor.fields_by_name)


def _has_field(message: Any, name: str) -> bool:
    return name in _message_fields(message)


def _copy_message_field(message: Any, proto: Any, *field_names: str) -> bool:
    for field_name in field_names:
        field = _message_fields(message).get(field_name)
        if field is None or field.type != _FieldDescriptor.TYPE_MESSAGE:
            continue
        getattr(message, field_name).CopyFrom(proto)
        return True
    return False


def _append_message_field(message: Any, proto: Any, *field_names: str) -> bool:
    for field_name in field_names:
        field = _message_fields(message).get(field_name)
        if field is None:
            continue
        if not getattr(field, "is_repeated", False):
            continue
        if field.type == _FieldDescriptor.TYPE_MESSAGE:
            getattr(message, field_name).add().CopyFrom(proto)
            return True
    return False


def _set_scalar_field(message: Any, value: Any, *field_names: str) -> bool:
    for field_name in field_names:
        field = _message_fields(message).get(field_name)
        if field is None or field.type == _FieldDescriptor.TYPE_MESSAGE:
            continue
        setattr(message, field_name, value)
        return True
    return False


def _resolve_stub_method(stub: Any, *names: str) -> Any:
    for name in names:
        value = getattr(stub, name, None)
        if value is not None:
            return value
    joined = ", ".join(names)
    raise RuntimeError(f"generated FileAPI gRPC stubs do not define any of: {joined}")


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError as error:
        code = error.code()  # ty: ignore[unresolved-attribute]
        details = error.details()  # ty: ignore[unresolved-attribute]
        if code == grpc.StatusCode.NOT_FOUND:
            raise NotFoundError(details) from error
        if code == grpc.StatusCode.ALREADY_EXISTS:
            raise AlreadyExistsError(details) from error
        raise


def _extract_scalar(response: Any, *field_names: str) -> Any:
    for field_name in field_names:
        if _has_field(response, field_name):
            return getattr(response, field_name)
    return response


def _looks_like_blob_message(message: Any) -> bool:
    fields = _message_fields(message)
    return bool(fields) and "size" in fields and "type" in fields


def _looks_like_file_message(message: Any) -> bool:
    fields = _message_fields(message)
    return _looks_like_blob_message(message) and "name" in fields and "last_modified" in fields


def _extract_blob_proto(response: Any) -> Any:
    if _looks_like_blob_message(response):
        return response
    for field_name in ("object", "blob", "value", "result"):
        if _has_field(response, field_name):
            return getattr(response, field_name)
    raise RuntimeError("FileAPI response did not include a blob payload")


def _extract_file_proto(response: Any) -> Any:
    if _looks_like_file_message(response):
        return response
    for field_name in ("file", "value", "result"):
        if _has_field(response, field_name):
            return getattr(response, field_name)
    blob_proto = _extract_blob_proto(response)
    if _looks_like_file_message(blob_proto):
        return blob_proto
    raise RuntimeError("FileAPI response did not include a file payload")


def _extract_files_proto(response: Any) -> list[Any]:
    if _has_field(response, "files"):
        return list(response.files)
    if _has_field(response, "items"):
        return list(response.items)
    raise RuntimeError("FileAPI response did not include a file list payload")


def _line_endings_to_proto(pb: Any, endings: str | None) -> int | None:
    normalized = (endings or "").strip().lower()
    if not normalized:
        return None
    if normalized == "native":
        return getattr(pb, "LINE_ENDINGS_NATIVE", None)
    if normalized == "transparent":
        return getattr(pb, "LINE_ENDINGS_TRANSPARENT", None)
    raise ValueError(f"unsupported line endings mode: {endings!r}")


def _blob_options(pb: Any, *, mime_type: str, endings: str | None) -> Any | None:
    options_type = _proto_type(pb, "BlobOptions", "BlobPropertyBag")
    if options_type is None:
        return None
    options = options_type()
    if mime_type:
        _set_scalar_field(options, mime_type, "type", "mime_type", "content_type")
    endings_value = _line_endings_to_proto(pb, endings)
    if endings_value is not None:
        _set_scalar_field(options, endings_value, "endings")
    return options


def _file_options(
    pb: Any,
    *,
    mime_type: str,
    endings: str | None,
    last_modified: int | _dt.datetime | None,
) -> Any | None:
    options_type = _proto_type(pb, "FileOptions", "FilePropertyBag")
    if options_type is None:
        return None
    options = options_type()
    if mime_type:
        _set_scalar_field(options, mime_type, "type", "mime_type", "content_type")
    endings_value = _line_endings_to_proto(pb, endings)
    if endings_value is not None:
        _set_scalar_field(options, endings_value, "endings")
    millis = _last_modified_millis(last_modified)
    if millis is not None:
        _set_scalar_field(options, millis, "last_modified")
    return options


def _last_modified_millis(last_modified: int | _dt.datetime | None) -> int | None:
    if last_modified is None:
        return None
    if isinstance(last_modified, int):
        return last_modified
    aware = (
        last_modified
        if last_modified.tzinfo is not None
        else last_modified.replace(tzinfo=_dt.timezone.utc)
    )
    return int(aware.timestamp() * 1000)


def _blob_part_to_proto(pb: Any, part: Any) -> Any:
    part_type = _require_proto_type(pb, "BlobPart")
    message = part_type()
    if isinstance(part, Blob):
        if _set_scalar_field(message, getattr(part._proto, "id", ""), "blob_id", "blobId"):
            return message
        raise RuntimeError("generated FileAPI BlobPart does not accept blob references")
    if isinstance(part, str):
        if _set_scalar_field(message, part, "string_data", "text", "string_value", "string", "value"):
            return message
        raise RuntimeError("generated FileAPI BlobPart does not accept string values")
    if isinstance(part, (bytes, bytearray, memoryview)):
        if _set_scalar_field(message, bytes(part), "bytes_data", "bytes", "bytes_value", "data", "value"):
            return message
        raise RuntimeError("generated FileAPI BlobPart does not accept bytes values")
    raise TypeError(f"unsupported BlobPart value: {type(part)!r}")


def _apply_blob_reference(request: Any, proto: Any) -> None:
    if _copy_message_field(request, proto, "blob", "file", "value", "source"):
        return
    for request_field, proto_field in (
        ("blob_id", "id"),
        ("id", "id"),
        ("handle", "handle"),
        ("blob_handle", "handle"),
        ("token", "token"),
    ):
        value = getattr(proto, proto_field, None)
        if value and _set_scalar_field(request, value, request_field):
            return
    raise RuntimeError("generated FileAPI request shape cannot reference blob values")


class Blob:
    def __init__(self, stub: Any, proto: Any) -> None:
        self._stub = stub
        self._proto = proto

    @property
    def id(self) -> str:
        return str(getattr(self._proto, "id", ""))

    @property
    def size(self) -> int:
        return int(getattr(self._proto, "size", 0))

    @property
    def type(self) -> str:
        return str(getattr(self._proto, "type", ""))

    def stat(self) -> Blob | File:
        pb, _pb_grpc = _require_generated_fileapi_modules()
        request_type = _require_proto_type(pb, "FileObjectRequest")
        request = request_type()
        _apply_blob_reference(request, self._proto)
        method = _resolve_stub_method(self._stub, "Stat")
        return _file_object(self._stub, _extract_blob_proto(_grpc_call(method, request)))

    def slice(
        self,
        start: int | None = None,
        end: int | None = None,
        content_type: str = "",
    ) -> Blob:
        pb, _pb_grpc = _require_generated_fileapi_modules()
        request_type = _require_proto_type(pb, "SliceBlobRequest", "BlobSliceRequest", "SliceRequest")
        request = request_type()
        _apply_blob_reference(request, self._proto)
        if start is not None:
            _set_scalar_field(request, start, "start", "offset")
        if end is not None:
            _set_scalar_field(request, end, "end")
        if content_type:
            _set_scalar_field(request, content_type, "content_type", "type", "mime_type")
        method = _resolve_stub_method(self._stub, "Slice", "SliceBlob")
        return Blob(self._stub, _extract_blob_proto(_grpc_call(method, request)))

    def text(self) -> str:
        return self.bytes().decode("utf-8")

    def array_buffer(self) -> _builtins.bytes:
        return self.bytes()

    def bytes(self) -> _builtins.bytes:
        pb, _pb_grpc = _require_generated_fileapi_modules()
        request_type = _require_proto_type(
            pb,
            "FileObjectRequest",
            "ReadBytesRequest",
            "BlobReadRequest",
            "ReadRequest",
        )
        request = request_type()
        _apply_blob_reference(request, self._proto)
        method = _resolve_stub_method(self._stub, "ReadBytes")
        return bytes(_extract_scalar(_grpc_call(method, request), "data", "bytes", "value"))

    def stream(self) -> Iterator[_builtins.bytes]:
        pb, _pb_grpc = _require_generated_fileapi_modules()
        request_type = _require_proto_type(pb, "ReadStreamRequest", "BlobReadRequest", "ReadRequest")
        request = request_type()
        _apply_blob_reference(request, self._proto)
        method = _resolve_stub_method(self._stub, "OpenReadStream", "ReadStream", "Stream", "OpenStream")
        try:
            responses = method(request)
        except grpc.RpcError as error:
            code = error.code()  # ty: ignore[unresolved-attribute]
            details = error.details()  # ty: ignore[unresolved-attribute]
            if code == grpc.StatusCode.NOT_FOUND:
                raise NotFoundError(details) from error
            if code == grpc.StatusCode.ALREADY_EXISTS:
                raise AlreadyExistsError(details) from error
            raise
        for response in responses:
            yield bytes(_extract_scalar(response, "chunk", "data", "bytes", "value"))

    def create_object_url(self) -> str:
        pb, _pb_grpc = _require_generated_fileapi_modules()
        request_type = _require_proto_type(pb, "CreateObjectURLRequest")
        request = request_type()
        _apply_blob_reference(request, self._proto)
        method = _resolve_stub_method(self._stub, "CreateObjectURL")
        response = _grpc_call(method, request)
        return str(_extract_scalar(response, "url", "value"))


class File(Blob):
    @property
    def name(self) -> str:
        return str(getattr(self._proto, "name", ""))

    @property
    def last_modified(self) -> int:
        return int(getattr(self._proto, "last_modified", 0))


class FileList(Sequence[File]):
    def __init__(self, files: Sequence[File]) -> None:
        self._files = list(files)

    @overload
    def __getitem__(self, index: int) -> File: ...

    @overload
    def __getitem__(self, index: slice) -> Sequence[File]: ...

    def __getitem__(self, index: int | slice) -> File | Sequence[File]:
        return self._files[index]

    def __len__(self) -> int:
        return len(self._files)

    def item(self, index: int) -> File | None:
        if index < 0 or index >= len(self._files):
            return None
        return self._files[index]


class FileAPI:
    def __init__(self, name: str | None = None) -> None:
        env_name = fileapi_socket_env(name)
        socket_path = os.environ.get(env_name, "")
        if not socket_path:
            raise RuntimeError(f"{env_name} is not set")
        pb, pb_grpc = _require_generated_fileapi_modules()
        stub_type = _require_proto_type(pb_grpc, "FileAPIStub")
        self._pb = pb
        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = stub_type(self._channel)

    def close(self) -> None:
        self._channel.close()

    def create_blob(
        self,
        parts: Sequence[Any] = (),
        *,
        type: str = "",
        endings: str | None = None,
    ) -> Blob:
        request_type = _require_proto_type(
            self._pb,
            "CreateBlobRequest",
            "BlobCreateRequest",
        )
        request = request_type()
        for part in parts:
            if not _append_message_field(
                request,
                _blob_part_to_proto(self._pb, part),
                "parts",
                "blob_parts",
                "bits",
            ):
                raise RuntimeError("generated FileAPI create-blob request does not expose blob parts")
        options = _blob_options(self._pb, mime_type=type, endings=endings)
        if options is not None:
            if not _copy_message_field(request, options, "options", "property_bag"):
                raise RuntimeError("generated FileAPI create-blob request does not expose options")
        elif type:
            _set_scalar_field(request, type, "type", "mime_type", "content_type")
        method = _resolve_stub_method(self._stub, "CreateBlob", "Blob")
        return Blob(self._stub, _extract_blob_proto(_grpc_call(method, request)))

    def blob(
        self,
        parts: Sequence[Any] = (),
        *,
        type: str = "",
        endings: str | None = None,
    ) -> Blob:
        return self.create_blob(parts=parts, type=type, endings=endings)

    def create_file(
        self,
        file_bits: Sequence[Any],
        file_name: str,
        *,
        type: str = "",
        endings: str | None = None,
        last_modified: int | _dt.datetime | None = None,
    ) -> File:
        request_type = _require_proto_type(
            self._pb,
            "CreateFileRequest",
            "FileCreateRequest",
        )
        request = request_type()
        for part in file_bits:
            if not _append_message_field(
                request,
                _blob_part_to_proto(self._pb, part),
                "file_bits",
                "bits",
                "parts",
            ):
                raise RuntimeError("generated FileAPI create-file request does not expose file bits")
        if not _set_scalar_field(request, file_name, "file_name", "name"):
            raise RuntimeError("generated FileAPI create-file request does not expose file name")
        options = _file_options(
            self._pb,
            mime_type=type,
            endings=endings,
            last_modified=last_modified,
        )
        if options is not None:
            if not _copy_message_field(request, options, "options", "property_bag"):
                raise RuntimeError("generated FileAPI create-file request does not expose options")
        else:
            if type:
                _set_scalar_field(request, type, "type", "mime_type", "content_type")
            millis = _last_modified_millis(last_modified)
            if millis is not None:
                _set_scalar_field(request, millis, "last_modified")
        method = _resolve_stub_method(self._stub, "CreateFile", "File")
        return File(self._stub, _extract_file_proto(_grpc_call(method, request)))

    def file(
        self,
        file_bits: Sequence[Any],
        file_name: str,
        *,
        type: str = "",
        endings: str | None = None,
        last_modified: int | _dt.datetime | None = None,
    ) -> File:
        return self.create_file(
            file_bits=file_bits,
            file_name=file_name,
            type=type,
            endings=endings,
            last_modified=last_modified,
        )

    def file_list(self, files: Sequence[File]) -> FileList:
        return FileList(files)

    def stat(self, object_id: str) -> Blob | File:
        request_type = _require_proto_type(self._pb, "FileObjectRequest")
        request = request_type()
        if not _set_scalar_field(request, object_id, "id", "blob_id", "blobId"):
            raise RuntimeError("generated FileAPI stat request does not expose object id")
        method = _resolve_stub_method(self._stub, "Stat")
        return _file_object(self._stub, _extract_blob_proto(_grpc_call(method, request)))

    def resolve_object_url(self, url: str) -> Blob | File:
        request_type = _require_proto_type(self._pb, "ObjectURLRequest")
        request = request_type()
        if not _set_scalar_field(request, url, "url", "value"):
            raise RuntimeError("generated FileAPI object-url request does not expose a url field")
        method = _resolve_stub_method(self._stub, "ResolveObjectURL")
        return _file_object(self._stub, _extract_blob_proto(_grpc_call(method, request)))

    def revoke_object_url(self, url: str) -> None:
        request_type = _require_proto_type(self._pb, "ObjectURLRequest")
        request = request_type()
        if not _set_scalar_field(request, url, "url", "value"):
            raise RuntimeError("generated FileAPI object-url request does not expose a url field")
        method = _resolve_stub_method(self._stub, "RevokeObjectURL")
        _grpc_call(method, request)

    def list_files(self) -> FileList:
        request_type = _proto_type(self._pb, "ListFilesRequest")
        if request_type is None:
            raise RuntimeError("generated FileAPI protobuf stubs do not define ListFilesRequest")
        method = _resolve_stub_method(self._stub, "ListFiles")
        response = _grpc_call(method, request_type())
        return FileList([File(self._stub, proto) for proto in _extract_files_proto(response)])

    def __enter__(self) -> FileAPI:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _file_object(stub: Any, proto: Any) -> Blob | File:
    if int(getattr(proto, "kind", 0)) == 2 or _looks_like_file_message(proto):
        return File(stub, proto)
    return Blob(stub, proto)
