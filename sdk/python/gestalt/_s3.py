"""S3-compatible client helpers for provider processes."""

from __future__ import annotations

import datetime as _dt
import io
import json
import os
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, BinaryIO, Iterable, Iterator, Protocol, cast
from urllib import parse as _urlparse

import grpc
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from ._grpc_transport import insecure_internal_channel, secure_internal_channel
from .gen.v1 import s3_pb2 as _pb
from .gen.v1 import s3_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
timestamp_pb2: Any = _timestamp_pb2

#: Base environment variable for discovering S3 runtime sockets.
ENV_S3_SOCKET = "GESTALT_S3_SOCKET"
_S3_SOCKET_TOKEN_SUFFIX = "_TOKEN"
_S3_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"
ENV_S3_SOCKET_TOKEN = f"{ENV_S3_SOCKET}{_S3_SOCKET_TOKEN_SUFFIX}"
_WRITE_CHUNK_SIZE = 64 * 1024
_UTC = _dt.timezone.utc
BytesData = bytes
BytesLike = bytes | bytearray | memoryview
ObjectBody = BytesLike | BinaryIO | Iterable[bytes] | None


def s3_socket_env(name: str | None = None) -> str:
    """Return the environment variable name for an S3 socket binding."""

    trimmed = (name or "").strip()
    if not trimmed:
        return ENV_S3_SOCKET
    normalized = "".join(
        ch.upper() if ("a" <= ch <= "z" or "A" <= ch <= "Z" or "0" <= ch <= "9") else "_"
        for ch in trimmed
    )
    return f"{ENV_S3_SOCKET}_{normalized}"


def s3_socket_token_env(name: str | None = None) -> str:
    """Return the environment variable name for an S3 relay token."""

    return f"{s3_socket_env(name)}{_S3_SOCKET_TOKEN_SUFFIX}"


class S3NotFoundError(Exception):
    """Raised when the requested object does not exist."""

    pass


class S3PreconditionFailedError(Exception):
    """Raised when conditional request headers fail."""

    pass


class S3InvalidRangeError(Exception):
    """Raised when a requested byte range is invalid."""

    pass


@dataclass
class ObjectRef:
    """Reference to an S3 object, optionally pinned to a version."""

    bucket: str
    key: str
    version_id: str = ""


@dataclass
class ObjectMeta:
    """Object metadata returned by S3 operations."""

    ref: ObjectRef
    etag: str = ""
    size: int = 0
    content_type: str = ""
    last_modified: _dt.datetime | None = None
    metadata: dict[str, str] = field(default_factory=dict)
    storage_class: str = ""


@dataclass
class ByteRange:
    """Inclusive byte range used for object reads."""

    start: int | None = None
    end: int | None = None


@dataclass
class ReadOptions:
    """Conditional and ranged read options for an object request."""

    range: ByteRange | None = None
    if_match: str = ""
    if_none_match: str = ""
    if_modified_since: _dt.datetime | None = None
    if_unmodified_since: _dt.datetime | None = None


@dataclass
class WriteOptions:
    """Metadata and precondition options for object writes."""

    content_type: str = ""
    cache_control: str = ""
    content_disposition: str = ""
    content_encoding: str = ""
    content_language: str = ""
    metadata: dict[str, str] = field(default_factory=dict)
    if_match: str = ""
    if_none_match: str = ""


@dataclass
class ListOptions:
    """Query options for listing objects within a bucket."""

    bucket: str
    prefix: str = ""
    delimiter: str = ""
    continuation_token: str = ""
    start_after: str = ""
    max_keys: int = 0


@dataclass
class ListPage:
    """One page of object-listing results."""

    objects: list[ObjectMeta] = field(default_factory=list)
    common_prefixes: list[str] = field(default_factory=list)
    next_continuation_token: str = ""
    has_more: bool = False


@dataclass
class CopyOptions:
    """Conditional headers for copy operations."""

    if_match: str = ""
    if_none_match: str = ""


class PresignMethod(str, Enum):
    """HTTP methods supported by presigned object URLs."""

    GET = "GET"
    PUT = "PUT"
    DELETE = "DELETE"
    HEAD = "HEAD"


@dataclass
class PresignOptions:
    """Options for generating a presigned object URL."""

    method: PresignMethod | str | None = None
    expires: _dt.timedelta | None = None
    content_type: str = ""
    content_disposition: str = ""
    headers: dict[str, str] = field(default_factory=dict)


@dataclass
class PresignResult:
    """Presigned URL response returned by the provider."""

    url: str
    method: PresignMethod | str | None = None
    expires_at: _dt.datetime | None = None
    headers: dict[str, str] = field(default_factory=dict)


class S3ReadStream:
    """Streaming object body reader returned by :meth:`S3.read_object`."""

    def __init__(self, stream: Any) -> None:
        self._stream = stream
        self._buffer = bytearray()
        self._closed = False

    def __iter__(self) -> Iterator[bytes]:
        """Iterate over the remaining data chunks."""

        return self.iter_chunks()

    def __enter__(self) -> S3ReadStream:
        """Return the stream for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the stream at the end of a context manager block."""

        self.close()

    def iter_chunks(self) -> Iterator[bytes]:
        """Yield the remaining object body chunks."""

        if self._buffer:
            chunk = bytes(self._buffer)
            self._buffer.clear()
            if chunk:
                yield chunk
        while True:
            chunk = self._recv_chunk()
            if chunk is None:
                return
            if chunk:
                yield chunk

    def read(self, size: int = -1) -> bytes:
        """Read up to ``size`` bytes from the stream."""

        if size == 0:
            return b""
        if size < 0:
            parts: list[bytes] = []
            if self._buffer:
                parts.append(bytes(self._buffer))
                self._buffer.clear()
            while True:
                chunk = self._recv_chunk()
                if chunk is None:
                    break
                if chunk:
                    parts.append(chunk)
            return b"".join(parts)

        while len(self._buffer) < size:
            chunk = self._recv_chunk()
            if chunk is None:
                break
            if chunk:
                self._buffer.extend(chunk)
        out = bytes(self._buffer[:size])
        del self._buffer[:size]
        return out

    def close(self) -> None:
        """Cancel the underlying RPC stream and discard buffered bytes."""

        self._closed = True
        self._buffer.clear()
        cancel = getattr(self._stream, "cancel", None)
        if callable(cancel):
            cancel()

    def _recv_chunk(self) -> bytes | None:
        if self._closed:
            return None
        try:
            msg = next(self._stream)
        except StopIteration:
            self._closed = True
            return None
        except grpc.RpcError as error:
            self._closed = True
            raise _map_grpc_error(error) from error
        if msg.WhichOneof("result") == "meta":
            raise RuntimeError("s3: read stream yielded metadata after the first frame")
        return bytes(msg.data)


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        method: str,
        timeout: float | None,
        metadata: Any,
        credentials: Any,
        wait_for_ready: bool | None,
        compression: Any,
    ) -> None:
        self.method = method
        self.timeout = timeout
        self.metadata = metadata
        self.credentials = credentials
        self.wait_for_ready = wait_for_ready
        self.compression = compression


class _ClientCallDetailsFields(Protocol):
    method: str
    timeout: float | None
    metadata: Any
    credentials: Any
    wait_for_ready: bool | None
    compression: Any


class _RelayTokenInterceptor(
    grpc.UnaryUnaryClientInterceptor,
    grpc.UnaryStreamClientInterceptor,
    grpc.StreamUnaryClientInterceptor,
):
    def __init__(self, token: str) -> None:
        self._token = token

    def _details(self, client_call_details: grpc.ClientCallDetails) -> grpc.ClientCallDetails:
        details = cast(_ClientCallDetailsFields, client_call_details)
        metadata = list(details.metadata or [])
        metadata.append((_S3_RELAY_TOKEN_HEADER, self._token))
        return _ClientCallDetails(
            details.method,
            details.timeout,
            metadata,
            details.credentials,
            details.wait_for_ready,
            details.compression,
        )

    def intercept_unary_unary(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request: Any,
    ) -> Any:
        return continuation(self._details(client_call_details), request)

    def intercept_unary_stream(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request: Any,
    ) -> Any:
        return continuation(self._details(client_call_details), request)

    def intercept_stream_unary(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request_iterator: Any,
    ) -> Any:
        return continuation(self._details(client_call_details), request_iterator)


def _s3_channel(target: str, *, token: str = "") -> Any:
    scheme, address = _parse_s3_target(target)
    if scheme == "unix":
        channel = insecure_internal_channel(f"unix:{address}")
    elif scheme == "tcp":
        channel = insecure_internal_channel(address)
    elif scheme == "tls":
        channel = secure_internal_channel(address)
    else:
        raise RuntimeError(f"unsupported s3 transport scheme {scheme!r}")
    token = token.strip()
    if not token:
        return channel
    return grpc.intercept_channel(channel, _RelayTokenInterceptor(token))


def _parse_s3_target(raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError("s3 transport target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(f"s3 tcp target {raw!r} is missing host:port")
        return "tcp", address
    if target.startswith("tls://"):
        address = target.removeprefix("tls://").strip()
        if not address:
            raise RuntimeError(f"s3 tls target {raw!r} is missing host:port")
        return "tls", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(f"s3 unix target {raw!r} is missing a socket path")
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(f"unsupported s3 target scheme {parsed.scheme!r}")
    return "unix", target


class S3:
    """Client for a host-provided Gestalt S3 runtime."""

    def __init__(self, name: str | None = None) -> None:
        env_name = s3_socket_env(name)
        target = os.environ.get(env_name, "")
        if not target:
            raise RuntimeError(f"{env_name} is not set")
        token = os.environ.get(s3_socket_token_env(name), "")
        self._channel = _s3_channel(target, token=token)
        self._stub = pb_grpc.S3Stub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def object(self, bucket: str, key: str) -> S3Object:
        """Return an object helper for the latest version."""

        return S3Object(self, ObjectRef(bucket=bucket, key=key))

    def object_version(self, bucket: str, key: str, version_id: str) -> S3Object:
        """Return an object helper pinned to a specific version."""

        return S3Object(self, ObjectRef(bucket=bucket, key=key, version_id=version_id))

    def head_object(self, ref: ObjectRef) -> ObjectMeta:
        """Fetch metadata for an object without reading its body."""

        resp = _grpc_call(self._stub.HeadObject, pb.HeadObjectRequest(ref=_object_ref_to_proto(ref)))
        return _object_meta_from_proto(resp.meta)

    def read_object(
        self,
        ref: ObjectRef,
        opts: ReadOptions | None = None,
    ) -> tuple[ObjectMeta, S3ReadStream]:
        """Open a streaming read for an object."""

        request = pb.ReadObjectRequest(ref=_object_ref_to_proto(ref))
        if opts is not None:
            if opts.range is not None:
                request.range.CopyFrom(_byte_range_to_proto(opts.range))
            request.if_match = opts.if_match
            request.if_none_match = opts.if_none_match
            if opts.if_modified_since is not None:
                request.if_modified_since.CopyFrom(_timestamp_to_proto(opts.if_modified_since))
            if opts.if_unmodified_since is not None:
                request.if_unmodified_since.CopyFrom(
                    _timestamp_to_proto(opts.if_unmodified_since)
                )

        stream = self._stub.ReadObject(request)
        try:
            first = next(stream)
        except StopIteration as error:
            raise RuntimeError("s3: read stream ended before metadata") from error
        except grpc.RpcError as error:
            raise _map_grpc_error(error) from error
        if first.WhichOneof("result") != "meta":
            raise RuntimeError("s3: read stream did not start with metadata")
        return _object_meta_from_proto(first.meta), S3ReadStream(stream)

    def write_object(
        self,
        ref: ObjectRef,
        body: ObjectBody = None,
        opts: WriteOptions | None = None,
    ) -> ObjectMeta:
        """Write an object body and return the resulting metadata."""

        open_request = pb.WriteObjectOpen(ref=_object_ref_to_proto(ref))
        if opts is not None:
            open_request.content_type = opts.content_type
            open_request.cache_control = opts.cache_control
            open_request.content_disposition = opts.content_disposition
            open_request.content_encoding = opts.content_encoding
            open_request.content_language = opts.content_language
            open_request.metadata.update(dict(opts.metadata))
            open_request.if_match = opts.if_match
            open_request.if_none_match = opts.if_none_match
        response = _grpc_call(
            self._stub.WriteObject,
            _write_request_iter(open_request=open_request, body=body),
        )
        return _object_meta_from_proto(response.meta)

    def delete_object(self, ref: ObjectRef) -> None:
        """Delete an object."""

        _grpc_call(self._stub.DeleteObject, pb.DeleteObjectRequest(ref=_object_ref_to_proto(ref)))

    def list_objects(self, opts: ListOptions) -> ListPage:
        """List objects within a bucket."""

        resp = _grpc_call(
            self._stub.ListObjects,
            pb.ListObjectsRequest(
                bucket=opts.bucket,
                prefix=opts.prefix,
                delimiter=opts.delimiter,
                continuation_token=opts.continuation_token,
                start_after=opts.start_after,
                max_keys=opts.max_keys,
            ),
        )
        return _list_page_from_proto(resp)

    def copy_object(
        self,
        source: ObjectRef,
        destination: ObjectRef,
        opts: CopyOptions | None = None,
    ) -> ObjectMeta:
        """Copy an object and return metadata for the destination."""

        request = pb.CopyObjectRequest(
            source=_object_ref_to_proto(source),
            destination=_object_ref_to_proto(destination),
        )
        if opts is not None:
            request.if_match = opts.if_match
            request.if_none_match = opts.if_none_match
        resp = _grpc_call(self._stub.CopyObject, request)
        return _object_meta_from_proto(resp.meta)

    def presign_object(
        self,
        ref: ObjectRef,
        opts: PresignOptions | None = None,
    ) -> PresignResult:
        """Generate a presigned URL for an object operation."""

        request = pb.PresignObjectRequest(ref=_object_ref_to_proto(ref))
        if opts is not None:
            request.method = _presign_method_to_proto(opts.method)
            if opts.expires is not None:
                request.expires_seconds = int(opts.expires.total_seconds())
            request.content_type = opts.content_type
            request.content_disposition = opts.content_disposition
            request.headers.update(dict(opts.headers))
        resp = _grpc_call(self._stub.PresignObject, request)
        result = _presign_result_from_proto(resp)
        if result.method is None and opts is not None:
            result.method = _normalize_presign_method(opts.method)
        return result

    def __enter__(self) -> S3:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


class S3Object:
    """Convenience wrapper for a single S3 object reference."""

    def __init__(self, client: S3, ref: ObjectRef) -> None:
        self._client = client
        self.ref = ref

    def stat(self) -> ObjectMeta:
        """Fetch object metadata."""

        return self._client.head_object(self.ref)

    def exists(self) -> bool:
        """Return whether the object exists."""

        try:
            self.stat()
            return True
        except S3NotFoundError:
            return False

    def stream(self, opts: ReadOptions | None = None) -> tuple[ObjectMeta, S3ReadStream]:
        """Open a streaming read for the object."""

        return self._client.read_object(self.ref, opts)

    def bytes(self, opts: ReadOptions | None = None) -> BytesData:
        """Read the full object body as bytes."""

        _meta, stream = self.stream(opts)
        with stream:
            return stream.read()

    def text(self, opts: ReadOptions | None = None, *, encoding: str = "utf-8") -> str:
        """Read the full object body as text."""

        return self.bytes(opts).decode(encoding)

    def json(self, opts: ReadOptions | None = None) -> Any:
        """Read and decode the full object body as JSON."""

        return json.loads(self.bytes(opts))

    def write(
        self,
        body: ObjectBody = None,
        opts: WriteOptions | None = None,
    ) -> ObjectMeta:
        """Write an object body."""

        return self._client.write_object(self.ref, body, opts)

    def write_bytes(
        self,
        body: BytesLike,
        opts: WriteOptions | None = None,
    ) -> ObjectMeta:
        """Write bytes to the object."""

        return self.write(body, opts)

    def write_text(
        self,
        body: str,
        opts: WriteOptions | None = None,
        *,
        encoding: str = "utf-8",
    ) -> ObjectMeta:
        """Encode and write text to the object."""

        return self.write(body.encode(encoding), opts)

    def write_json(self, value: Any, opts: WriteOptions | None = None) -> ObjectMeta:
        """Encode and write JSON to the object."""

        payload = json.dumps(value).encode("utf-8")
        if opts is None:
            opts = WriteOptions(content_type="application/json")
        elif not opts.content_type:
            opts = WriteOptions(
                content_type="application/json",
                cache_control=opts.cache_control,
                content_disposition=opts.content_disposition,
                content_encoding=opts.content_encoding,
                content_language=opts.content_language,
                metadata=dict(opts.metadata),
                if_match=opts.if_match,
                if_none_match=opts.if_none_match,
            )
        return self.write(payload, opts)

    def delete(self) -> None:
        """Delete the object."""

        self._client.delete_object(self.ref)

    def presign(self, opts: PresignOptions | None = None) -> PresignResult:
        """Generate a presigned URL for this object."""

        return self._client.presign_object(self.ref, opts)


def _write_request_iter(
    *,
    open_request: Any,
    body: ObjectBody,
) -> Iterator[Any]:
    yield pb.WriteObjectRequest(open=open_request)
    for chunk in _body_chunks(body):
        if chunk:
            yield pb.WriteObjectRequest(data=chunk)


def _body_chunks(
    body: ObjectBody,
) -> Iterator[bytes]:
    if body is None:
        return
    if isinstance(body, (bytes, bytearray, memoryview)):
        data = bytes(body)
        for start in range(0, len(data), _WRITE_CHUNK_SIZE):
            yield data[start : start + _WRITE_CHUNK_SIZE]
        return
    if isinstance(body, io.IOBase):
        while True:
            chunk = body.read(_WRITE_CHUNK_SIZE)
            if chunk in (b"", None):
                return
            yield _ensure_bytes(chunk)
    reader = getattr(body, "read", None)
    if callable(reader):
        while True:
            chunk = reader(_WRITE_CHUNK_SIZE)
            if chunk in (b"", None):
                return
            yield _ensure_bytes(chunk)
    for chunk in body:
        piece = _ensure_bytes(chunk)
        if piece:
            yield piece


def _ensure_bytes(value: Any) -> bytes:
    if isinstance(value, bytes):
        return value
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        return value.tobytes()
    raise TypeError("s3: body chunks must be bytes")


def _grpc_call(fn: Any, request: Any) -> Any:
    try:
        return fn(request)
    except grpc.RpcError as error:
        raise _map_grpc_error(error) from error


def _map_grpc_error(error: grpc.RpcError) -> Exception:
    code = error.code()  # ty: ignore[unresolved-attribute]
    details = error.details()  # ty: ignore[unresolved-attribute]
    if code == grpc.StatusCode.NOT_FOUND:
        return S3NotFoundError(details)
    if code == grpc.StatusCode.FAILED_PRECONDITION:
        return S3PreconditionFailedError(details)
    if code == grpc.StatusCode.OUT_OF_RANGE:
        return S3InvalidRangeError(details)
    return error


def _object_ref_to_proto(ref: ObjectRef) -> Any:
    return pb.S3ObjectRef(bucket=ref.bucket, key=ref.key, version_id=ref.version_id)


def _object_meta_from_proto(meta: Any) -> ObjectMeta:
    last_modified: _dt.datetime | None = None
    if meta.HasField("last_modified"):
        last_modified = meta.last_modified.ToDatetime(tzinfo=_UTC)
    return ObjectMeta(
        ref=ObjectRef(
            bucket=meta.ref.bucket,
            key=meta.ref.key,
            version_id=meta.ref.version_id,
        ),
        etag=meta.etag,
        size=meta.size,
        content_type=meta.content_type,
        last_modified=last_modified,
        metadata=dict(meta.metadata),
        storage_class=meta.storage_class,
    )


def _byte_range_to_proto(range_value: ByteRange) -> Any:
    out = pb.ByteRange()
    if range_value.start is not None:
        out.start = range_value.start
    if range_value.end is not None:
        out.end = range_value.end
    return out


def _timestamp_to_proto(value: _dt.datetime) -> Any:
    if value.tzinfo is None:
        value = value.replace(tzinfo=_UTC)
    else:
        value = value.astimezone(_UTC)
    out = timestamp_pb2.Timestamp()
    out.FromDatetime(value)
    return out


def _list_page_from_proto(resp: Any) -> ListPage:
    return ListPage(
        objects=[_object_meta_from_proto(item) for item in resp.objects],
        common_prefixes=list(resp.common_prefixes),
        next_continuation_token=resp.next_continuation_token,
        has_more=resp.has_more,
    )


def _presign_method_to_proto(method: PresignMethod | str | None) -> Any:
    normalized = _presign_method_value(method)
    return {
        PresignMethod.GET.value: pb.PRESIGN_METHOD_GET,
        PresignMethod.PUT.value: pb.PRESIGN_METHOD_PUT,
        PresignMethod.DELETE.value: pb.PRESIGN_METHOD_DELETE,
        PresignMethod.HEAD.value: pb.PRESIGN_METHOD_HEAD,
    }.get(normalized, pb.PRESIGN_METHOD_UNSPECIFIED)


def _presign_method_from_proto(value: Any) -> PresignMethod | str | None:
    return {
        pb.PRESIGN_METHOD_GET: PresignMethod.GET,
        pb.PRESIGN_METHOD_PUT: PresignMethod.PUT,
        pb.PRESIGN_METHOD_DELETE: PresignMethod.DELETE,
        pb.PRESIGN_METHOD_HEAD: PresignMethod.HEAD,
    }.get(value)


def _normalize_presign_method(method: PresignMethod | str | None) -> PresignMethod | str | None:
    normalized = _presign_method_value(method)
    return {
        PresignMethod.GET.value: PresignMethod.GET,
        PresignMethod.PUT.value: PresignMethod.PUT,
        PresignMethod.DELETE.value: PresignMethod.DELETE,
        PresignMethod.HEAD.value: PresignMethod.HEAD,
    }.get(normalized, method if method else None)


def _presign_method_value(method: PresignMethod | str | None) -> str:
    if isinstance(method, PresignMethod):
        return method.value.upper()
    return str(method or "").strip().upper()


def _presign_result_from_proto(resp: Any) -> PresignResult:
    expires_at: _dt.datetime | None = None
    if resp.HasField("expires_at"):
        expires_at = resp.expires_at.ToDatetime(tzinfo=_UTC)
    return PresignResult(
        url=resp.url,
        method=_presign_method_from_proto(resp.method),
        expires_at=expires_at,
        headers=dict(resp.headers),
    )
