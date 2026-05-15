import datetime as dt
import importlib
import json
import pathlib
import socket
import sys
import tempfile
import threading
import unittest
from typing import Any, cast
from unittest import mock

import grpc
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from gestalt import (
    AgentProvider,
    AuthenticationProvider,
    BeginLoginRequest,
    BoundWorkflowEventTrigger,
    BoundWorkflowRun,
    BoundWorkflowSchedule,
    CacheEntry,
    CacheProvider,
    Catalog,
    CatalogOperation,
    CompleteLoginRequest,
    ConnectedToken,
    ExternalTokenValidator,
    GetPluginRuntimeSupportRequest,
    HealthChecker,
    MetadataProvider,
    PauseWorkflowProviderEventTriggerRequest,
    PauseWorkflowProviderScheduleRequest,
    Plugin,
    PluginProviderAdapter,
    PluginRuntimeProvider,
    PluginRuntimeSupport,
    ProviderKind,
    ProviderMetadata,
    Request,
    ResumeWorkflowProviderEventTriggerRequest,
    ResumeWorkflowProviderScheduleRequest,
    S3Provider,
    SessionTTLProvider,
    StartWorkflowProviderRunRequest,
    WarningsProvider,
    WorkflowProvider,
    _bootstrap,
    _grpc_transport,
    _runtime,
)
from gestalt._gen.v1 import agent_pb2_grpc as _agent_pb2_grpc
from gestalt._gen.v1 import authentication_pb2 as _authentication_pb2
from gestalt._gen.v1 import cache_pb2 as _cache_pb2
from gestalt._gen.v1 import plugin_pb2 as _plugin_pb2
from gestalt._gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc
from gestalt._gen.v1 import pluginruntime_pb2 as _pluginruntime_pb2
from gestalt._gen.v1 import pluginruntime_pb2_grpc as _pluginruntime_pb2_grpc
from gestalt._gen.v1 import runtime_pb2 as _runtime_pb2
from gestalt._gen.v1 import s3_pb2_grpc as _s3_pb2_grpc
from gestalt._gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt._gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

agent_pb2_grpc: Any = _agent_pb2_grpc
authentication_pb2: Any = _authentication_pb2
cache_pb2: Any = _cache_pb2
duration_pb2: Any = _duration_pb2
empty_pb2: Any = _empty_pb2
plugin_pb2: Any = _plugin_pb2
plugin_pb2_grpc: Any = _plugin_pb2_grpc
pluginruntime_pb2: Any = _pluginruntime_pb2
pluginruntime_pb2_grpc: Any = _pluginruntime_pb2_grpc
runtime_pb2: Any = _runtime_pb2
s3_pb2_grpc: Any = _s3_pb2_grpc
struct_pb2: Any = _struct_pb2
timestamp_pb2: Any = _timestamp_pb2
workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc

UTC = dt.timezone.utc


def _ts(epoch_seconds: int) -> Any:
    ts = timestamp_pb2.Timestamp()
    ts.FromDatetime(dt.datetime.fromtimestamp(epoch_seconds, tz=UTC))
    return ts


class AbortCalled(RuntimeError):
    pass


class AbortContext:
    def __init__(self) -> None:
        self._code: grpc.StatusCode | None = None
        self.details: str | None = None

    def abort(self, code: grpc.StatusCode, details: str) -> None:
        self._code = code
        self.details = details
        raise AbortCalled(details)

    def code(self) -> grpc.StatusCode | None:
        return self._code


class ParseRuntimeArgsTests(unittest.TestCase):
    def test_explicit_root_and_target(self) -> None:
        runtime_args = _runtime._parse_runtime_args(
            ["/tmp/plugin", "example.plugin:PLUGIN", "auth"]
        )

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="example.plugin:PLUGIN",
                root=pathlib.Path("/tmp/plugin"),
                runtime_kind="authentication",
            ),
        )

    def test_rejects_single_argument(self) -> None:
        self.assertIsNone(_runtime._parse_runtime_args(["/tmp/plugin"]))

    def test_bundled_config_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            bundle_dir = pathlib.Path(tmpdir)
            (bundle_dir / _bootstrap.BUNDLED_CONFIG_NAME).write_text(
                json.dumps(
                    {
                        "target": "provider",
                        "plugin_name": "released-plugin",
                        "runtime_kind": "secrets",
                    }
                ),
                encoding="utf-8",
            )

            with mock.patch.object(
                _runtime.sys, "_MEIPASS", str(bundle_dir), create=True
            ):
                runtime_args = _runtime._parse_runtime_args([])

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="provider",
                plugin_name="released-plugin",
                runtime_kind="secrets",
            ),
        )

    def test_defaults_runtime_kind_to_integration(self) -> None:
        runtime_args = _runtime._parse_runtime_args(
            ["/tmp/plugin", "example.plugin:PLUGIN"]
        )
        self.assertIsNotNone(runtime_args)
        assert runtime_args is not None
        self.assertEqual(runtime_args.runtime_kind, "integration")

    def test_returns_none_without_args_or_bundled_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.object(_runtime.sys, "_MEIPASS", tmpdir, create=True):
                self.assertIsNone(_runtime._parse_runtime_args([]))


class RuntimeKindNormalizationTests(unittest.TestCase):
    def test_normalized_runtime_kind_recognizes_cache(self) -> None:
        self.assertEqual(
            _runtime._normalized_runtime_kind("cache"),
            ProviderKind.CACHE,
        )

    def test_normalized_runtime_kind_defaults_none_to_integration(self) -> None:
        self.assertEqual(
            _runtime._normalized_runtime_kind(None),
            ProviderKind.INTEGRATION,
        )

    def test_normalized_runtime_kind_rejects_unknown_values(self) -> None:
        with self.assertRaisesRegex(ValueError, "unsupported runtime kind"):
            _runtime._normalized_runtime_kind("typo")

    def test_normalized_runtime_kind_rejects_unsupported_types(self) -> None:
        with self.assertRaisesRegex(TypeError, "unsupported runtime kind"):
            _runtime._normalized_runtime_kind(object())


class DurationConversionTests(unittest.TestCase):
    def test_duration_to_timedelta_truncates_submicrosecond_nanos(self) -> None:
        self.assertEqual(
            _runtime._duration_to_timedelta(duration_pb2.Duration(nanos=5_999)),
            dt.timedelta(microseconds=5),
        )


class ProviderSocketTargetTests(unittest.TestCase):
    def test_parse_provider_socket_target_defaults_plain_paths_to_unix(self) -> None:
        self.assertEqual(
            _runtime._parse_provider_socket_target("/tmp/provider.sock"),
            ("unix", "/tmp/provider.sock"),
        )

    def test_parse_provider_socket_target_accepts_unix_and_tcp_targets(self) -> None:
        self.assertEqual(
            _runtime._parse_provider_socket_target("unix:///tmp/provider.sock"),
            ("unix", "/tmp/provider.sock"),
        )
        self.assertEqual(
            _runtime._parse_provider_socket_target("tcp://127.0.0.1:50051"),
            ("tcp", "127.0.0.1:50051"),
        )

    def test_parse_provider_socket_target_rejects_unsupported_schemes(self) -> None:
        with self.assertRaisesRegex(
            RuntimeError,
            "unsupported provider socket target scheme 'tls'",
        ):
            _runtime._parse_provider_socket_target("tls://127.0.0.1:50051")


class InternalGrpcTransportTests(unittest.TestCase):
    def test_internal_channels_raise_message_size_limits(self) -> None:
        options = dict(_grpc_transport._INTERNAL_CHANNEL_OPTIONS)

        self.assertEqual(
            options["grpc.max_receive_message_length"],
            _grpc_transport.INTERNAL_GRPC_MAX_MESSAGE_BYTES,
        )
        self.assertEqual(
            options["grpc.max_send_message_length"],
            _grpc_transport.INTERNAL_GRPC_MAX_MESSAGE_BYTES,
        )


class RuntimeServeTransportTests(unittest.TestCase):
    def test_runtime_serve_supports_tcp_provider_sockets(self) -> None:
        plugin = Plugin("tcp-runtime")

        @plugin.operation
        def ping(request: Request) -> dict[str, str]:
            return {"idempotency_key": request.idempotency_key}

        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            host, port = sock.getsockname()
        address = f"{host}:{port}"
        server_holder: dict[str, grpc.Server] = {}
        ready = threading.Event()
        failures: list[BaseException] = []

        def capture_shutdown(server: grpc.Server, _close_provider: Any) -> None:
            server_holder["server"] = server
            ready.set()

        def run_server() -> None:
            try:
                with mock.patch.object(
                    _runtime,
                    "_register_shutdown_handlers",
                    side_effect=capture_shutdown,
                ):
                    _runtime.serve(plugin)
            except BaseException as exc:  # pragma: no cover - surfaced via assertions
                failures.append(exc)
                ready.set()

        with mock.patch.dict(
            _runtime.os.environ,
            {_runtime.ENV_PROVIDER_SOCKET: f"tcp://{address}"},
            clear=False,
        ):
            thread = threading.Thread(target=run_server, daemon=True)
            thread.start()
            self.assertTrue(ready.wait(timeout=5))
            self.assertEqual(failures, [])
            self.assertIn("server", server_holder)

            channel = grpc.insecure_channel(address)
            self.addCleanup(channel.close)
            grpc.channel_ready_future(channel).result(timeout=5)
            stub = plugin_pb2_grpc.IntegrationProviderStub(channel)

            metadata = stub.GetMetadata(empty_pb2.Empty(), timeout=5)
            started = stub.StartProvider(
                plugin_pb2.StartProviderRequest(
                    name="tcp-runtime",
                    protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
                ),
                timeout=5,
            )
            result = stub.Execute(
                plugin_pb2.ExecuteRequest(
                    operation="ping",
                    idempotency_key=" transport-tool-123 ",
                ),
                timeout=5,
            )

            self.assertEqual(
                metadata.min_protocol_version,
                _runtime.CURRENT_PROTOCOL_VERSION,
            )
            self.assertEqual(
                metadata.max_protocol_version,
                _runtime.CURRENT_PROTOCOL_VERSION,
            )
            self.assertEqual(
                started.protocol_version,
                _runtime.CURRENT_PROTOCOL_VERSION,
            )
            self.assertEqual(
                json.loads(result.body),
                {"idempotency_key": "transport-tool-123"},
            )

            server_holder["server"].stop(grace=0).wait()
            thread.join(timeout=5)
            self.assertFalse(thread.is_alive())
            self.assertEqual(failures, [])


class PublicImportTests(unittest.TestCase):
    def test_indexeddb_stays_lazy_until_requested(self) -> None:
        sys.modules.pop("gestalt._indexeddb", None)

        gestalt_module = importlib.import_module("gestalt")
        gestalt_module.__dict__.pop("IndexedDB", None)

        self.assertNotIn("gestalt._indexeddb", sys.modules)
        self.assertEqual(gestalt_module.IndexedDB.__module__, "gestalt._indexeddb")
        self.assertIn("gestalt._indexeddb", sys.modules)


class ManifestNameTests(unittest.TestCase):
    def test_display_name_variants(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            temp_root = pathlib.Path(tmpdir)

            manifest_path = temp_root / "manifest.yaml"
            manifest_path.write_text(
                'display_name: "Released Plugin"\n', encoding="utf-8"
            )

            manifest_dir = temp_root / "plugin.json"
            manifest_dir.mkdir()
            (manifest_dir / "manifest.yaml").write_text(
                'display_name: "Directory Manifest"\n',
                encoding="utf-8",
            )

            ascii_slug_manifest_path = temp_root / "ascii-slug.yaml"
            ascii_slug_manifest_path.write_text(
                'display_name: "Crème Brûlée"\n',
                encoding="utf-8",
            )

            tagged_manifest_path = temp_root / "tagged.yaml"
            tagged_manifest_path.write_text(
                "source: !env github.com/acme/plugins/tagged-provider\n"
                "display_name: !env ${PLUGIN_NAME}\n",
                encoding="utf-8",
            )

            cases = [
                (manifest_path, "Released-Plugin"),
                (manifest_dir, "Directory-Manifest"),
                (ascii_slug_manifest_path, "Cr-me-Br-l-e"),
                (tagged_manifest_path, "tagged-provider"),
            ]
            for manifest_input, expected_name in cases:
                with self.subTest(manifest_input=str(manifest_input)):
                    plugin = Plugin.from_manifest(manifest_input)
                    self.assertEqual(plugin.name, expected_name)


class RequestTests(unittest.TestCase):
    def test_connection_param_returns_value_or_none(self) -> None:
        request = Request(connection_params={"region": "us-east-1"})

        self.assertEqual(request.connection_param("region"), "us-east-1")
        self.assertIsNone(request.connection_param("missing"))
        self.assertEqual(request.subject.id, "")
        self.assertEqual(request.subject.email, "")
        self.assertEqual(request.agent_subject.id, "")
        self.assertEqual(request.agent_subject.email, "")
        self.assertEqual(request.credential.mode, "")
        self.assertEqual(request.access.role, "")
        self.assertEqual(request.workflow, {})
        self.assertEqual(request.idempotency_key, "")
        self.assertEqual(request.invocation_token, "")


class MainEntrypointTests(unittest.TestCase):
    def test_writes_catalog_when_env_is_set(self) -> None:
        plugin = Plugin("test-plugin")

        @plugin.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            catalog_path = pathlib.Path(tmpdir) / "catalog.yaml"
            with (
                mock.patch.object(_runtime, "_load_target", return_value=plugin),
                mock.patch.dict(
                    _runtime.os.environ,
                    {_runtime.ENV_WRITE_CATALOG: str(catalog_path)},
                    clear=True,
                ),
            ):
                result = _runtime.main(["/tmp/plugin", "example.plugin:PLUGIN"])

            self.assertEqual(result, 0)
            self.assertTrue(catalog_path.exists())

    def test_returns_usage_error_for_bad_args(self) -> None:
        result = _runtime.main(["/only-one-arg"])
        self.assertEqual(result, 2)

    def test_provider_servicer_reports_and_serves_session_catalogs(self) -> None:
        plugin = Plugin("source-name")
        configured: list[tuple[str, dict[str, Any]]] = []

        @plugin.configure
        def configure(name: str, config: dict[str, Any]) -> None:
            configured.append((name, dict(config)))

        @plugin.operation
        def whoami(request: Request) -> dict[str, Any]:
            return {
                "token": request.token,
                "subject_id": request.subject.id,
                "subject_kind": request.subject.kind,
                "subject_email": request.subject.email,
                "agent_subject_id": request.agent_subject.id,
                "agent_subject_email": request.agent_subject.email,
                "external_type": request.external_identity.type,
                "external_id": request.external_identity.id,
                "agent_external_type": request.agent_external_identity.type,
                "agent_external_id": request.agent_external_identity.id,
                "credential_mode": request.credential.mode,
                "credential_subject_id": request.credential.subject_id,
                "access_policy": request.access.policy,
                "access_role": request.access.role,
                "host_base_url": request.host.public_base_url,
                "idempotency_key": request.idempotency_key,
                "invocation_token": request.invocation_token,
                "workflow_run_id": str(request.workflow.get("runId", "")),
                "workflow_trigger_kind": str(
                    request.workflow.get("trigger", {}).get("kind", "")
                ),
                "workflow": request.workflow,
                "tool_refs_set": request.tool_refs_set,
                "tool_ref_plugin": request.tool_refs[0].plugin
                if request.tool_refs
                else "",
                "tool_ref_operation": request.tool_refs[0].operation
                if request.tool_refs
                else "",
                "tool_ref_run_as": request.tool_refs[0].run_as.subject_id
                if request.tool_refs and request.tool_refs[0].run_as
                else "",
                "tool_ref_external_id": request.tool_refs[0].run_as_external_identity.id
                if request.tool_refs and request.tool_refs[0].run_as_external_identity
                else "",
            }

        @plugin.session_catalog
        def dynamic_catalog(request: Request) -> Catalog:
            cat = Catalog(
                name="session-source",
                display_name="|".join(
                    [
                        request.connection_param("tenant") or "",
                        request.subject.id,
                        request.credential.mode,
                        request.access.role,
                        request.host.public_base_url,
                        str(request.workflow.get("runId", "")),
                    ]
                ),
            )
            cat.operations.append(CatalogOperation(id="private_search", method="POST"))
            cat.operations[0].allowed_roles.extend(["viewer", "admin"])
            return cat

        @plugin.post_connect
        def dynamic_post_connect(token: ConnectedToken) -> dict[str, str]:
            return {
                "subject": token.subject_id,
                "connection": token.connection,
                "instance": token.instance,
                "metadata_team": (token.metadata or {}).get("team_id", ""),
                "created_tz": token.created_at.tzname() if token.created_at else "",
                "updated_tz": token.updated_at.tzname() if token.updated_at else "",
            }

        execute_workflow = struct_pb2.Struct()
        execute_workflow.update(
            {
                "runId": "run-123",
                "createdBy": {
                    "subjectId": "user:user-123",
                    "subjectKind": "user",
                    "displayName": "Ada",
                    "authSource": "api_token",
                },
                "trigger": {
                    "kind": "event",
                    "triggerId": "trigger-1",
                    "event": {
                        "id": "evt-1",
                        "source": "urn:test",
                        "specVersion": "1.0",
                        "type": "demo.refresh",
                        "dataContentType": "application/json",
                    },
                },
            }
        )
        catalog_workflow = struct_pb2.Struct()
        catalog_workflow.update({"runId": "run-456"})

        servicer = _runtime._provider_servicer(plugin=plugin)
        metadata = servicer.GetMetadata(mock.Mock(), mock.Mock())
        bad_context = AbortContext()
        with self.assertRaisesRegex(
            AbortCalled,
            "host requested protocol version",
        ):
            servicer.StartProvider(
                plugin_pb2.StartProviderRequest(
                    name="source-instance",
                    protocol_version=_runtime.CURRENT_PROTOCOL_VERSION + 1,
                ),
                bad_context,
            )
        self.assertEqual(bad_context.code(), grpc.StatusCode.FAILED_PRECONDITION)
        self.assertEqual(
            bad_context.details,
            f"host requested protocol version {_runtime.CURRENT_PROTOCOL_VERSION + 1}, provider requires {_runtime.CURRENT_PROTOCOL_VERSION}",
        )
        self.assertEqual(configured, [])

        start_request = plugin_pb2.StartProviderRequest(
            name="source-instance",
            protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
        )
        json_format.ParseDict({"region": "use1"}, start_request.config)
        start_response = servicer.StartProvider(start_request, mock.Mock())
        execute_response = servicer.Execute(
            plugin_pb2.ExecuteRequest(
                operation="whoami",
                token="secret-token",
                idempotency_key=" tool-call-123 ",
                invocation_token="opaque-invocation-token",
                context=plugin_pb2.RequestContext(
                    subject=plugin_pb2.SubjectContext(
                        id="user:user-123",
                        kind="user",
                        auth_source="api_token",
                        email="ada@example.com",
                    ),
                    agent_subject=plugin_pb2.SubjectContext(
                        id="user:user-456",
                        kind="user",
                        display_name="Grace Hopper",
                        auth_source="slack",
                        email="grace@example.com",
                    ),
                    agent_external_identity=plugin_pb2.ExternalIdentityContext(
                        type="github_identity",
                        id="user:12345678",
                    ),
                    external_identity=plugin_pb2.ExternalIdentityContext(
                        type="github_app_installation",
                        id="repo:acme/widgets",
                    ),
                    credential=plugin_pb2.CredentialContext(
                        mode="user",
                        subject_id="user:user-123",
                    ),
                    access=plugin_pb2.AccessContext(
                        policy="sample_policy",
                        role="admin",
                    ),
                    host=plugin_pb2.HostContext(
                        public_base_url="https://gestalt.example.test",
                    ),
                    workflow=execute_workflow,
                    tool_refs=[
                        plugin_pb2.AgentToolRef(
                            plugin="github",
                            operation="bot.getPullRequest",
                            run_as=plugin_pb2.AgentSubjectContext(
                                subject_id="service_account:github-review",
                                subject_kind="service_account",
                                credential_subject_id="service_account:github-review",
                                display_name="GitHub Review",
                                auth_source="managed_subject",
                            ),
                            run_as_external_identity=plugin_pb2.ExternalIdentityContext(
                                type="github_identity",
                                id="user:12345678",
                            ),
                        )
                    ],
                    tool_refs_set=True,
                ),
            ),
            mock.Mock(),
        )
        response = servicer.GetSessionCatalog(
            plugin_pb2.GetSessionCatalogRequest(
                token="secret-token",
                connection_params={"tenant": "acme"},
                context=plugin_pb2.RequestContext(
                    subject=plugin_pb2.SubjectContext(id="user:user-123", kind="user"),
                    credential=plugin_pb2.CredentialContext(mode="user"),
                    access=plugin_pb2.AccessContext(
                        policy="sample_policy",
                        role="viewer",
                    ),
                    host=plugin_pb2.HostContext(
                        public_base_url="https://gestalt.example.test",
                    ),
                    workflow=catalog_workflow,
                ),
            ),
            mock.Mock(),
        )
        post_connect_response = servicer.PostConnect(
            plugin_pb2.PostConnectRequest(
                token=plugin_pb2.PostConnectCredential(
                    subject_id="user:user-123",
                    connection="workspace",
                    instance="default",
                    metadata_json='{"team_id":"T123"}',
                    created_at=_ts(1_700_000_000),
                    updated_at=_ts(1_700_000_100),
                )
            ),
            mock.Mock(),
        )
        empty_timestamp_response = servicer.PostConnect(
            plugin_pb2.PostConnectRequest(
                token=plugin_pb2.PostConnectCredential(
                    subject_id="user:user-123",
                    connection="workspace",
                    instance="default",
                    metadata_json='{"team_id":"T123"}',
                )
            ),
            mock.Mock(),
        )

        self.assertTrue(metadata.supports_session_catalog)
        self.assertTrue(metadata.supports_post_connect)
        self.assertEqual(
            metadata.min_protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            metadata.max_protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            start_response.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            configured,
            [("source-instance", {"region": "use1"})],
        )
        self.assertEqual(
            json.loads(execute_response.body),
            {
                "token": "secret-token",
                "subject_id": "user:user-123",
                "subject_kind": "user",
                "subject_email": "ada@example.com",
                "agent_subject_id": "user:user-456",
                "agent_subject_email": "grace@example.com",
                "external_type": "github_app_installation",
                "external_id": "repo:acme/widgets",
                "agent_external_type": "github_identity",
                "agent_external_id": "user:12345678",
                "credential_mode": "user",
                "credential_subject_id": "user:user-123",
                "access_policy": "sample_policy",
                "access_role": "admin",
                "host_base_url": "https://gestalt.example.test",
                "idempotency_key": "tool-call-123",
                "workflow_run_id": "run-123",
                "workflow_trigger_kind": "event",
                "workflow": {
                    "runId": "run-123",
                    "createdBy": {
                        "subjectId": "user:user-123",
                        "subjectKind": "user",
                        "displayName": "Ada",
                        "authSource": "api_token",
                    },
                    "trigger": {
                        "kind": "event",
                        "triggerId": "trigger-1",
                        "event": {
                            "id": "evt-1",
                            "source": "urn:test",
                            "specVersion": "1.0",
                            "type": "demo.refresh",
                            "dataContentType": "application/json",
                        },
                    },
                },
                "tool_refs_set": True,
                "tool_ref_plugin": "github",
                "tool_ref_operation": "bot.getPullRequest",
                "tool_ref_run_as": "service_account:github-review",
                "tool_ref_external_id": "user:12345678",
                "invocation_token": "opaque-invocation-token",
            },
        )
        catalog = response.catalog
        self.assertEqual(catalog.name, "session-source")
        self.assertEqual(
            catalog.display_name,
            "acme|user:user-123|user|viewer|https://gestalt.example.test|run-456",
        )
        self.assertEqual(len(catalog.operations), 1)
        self.assertEqual(catalog.operations[0].id, "private_search")
        self.assertEqual(catalog.operations[0].method, "POST")
        self.assertEqual(list(catalog.operations[0].allowed_roles), ["viewer", "admin"])
        self.assertEqual(
            dict(post_connect_response.metadata),
            {
                "subject": "user:user-123",
                "connection": "workspace",
                "instance": "default",
                "metadata_team": "T123",
                "created_tz": "UTC",
                "updated_tz": "UTC",
            },
        )
        self.assertEqual(
            dict(empty_timestamp_response.metadata),
            {
                "subject": "user:user-123",
                "connection": "workspace",
                "instance": "default",
                "metadata_team": "T123",
                "created_tz": "",
                "updated_tz": "",
            },
        )

    def test_provider_servicer_sanitizes_unhandled_execute_exceptions(self) -> None:
        plugin = Plugin("source-name")

        @plugin.operation
        def broken() -> None:
            raise RuntimeError("sensitive details")

        servicer = _runtime._provider_servicer(plugin=plugin)
        execute_response = servicer.Execute(
            plugin_pb2.ExecuteRequest(operation="broken"),
            mock.Mock(),
        )

        self.assertEqual(execute_response.status, 500)
        self.assertEqual(json.loads(execute_response.body), {"error": "internal error"})

    def test_provider_servicer_rejects_missing_session_catalog_support(self) -> None:
        plugin = Plugin("source-name")
        servicer = _runtime._provider_servicer(plugin=plugin)
        context = mock.Mock()

        servicer.GetSessionCatalog(plugin_pb2.GetSessionCatalogRequest(), context)

        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "provider does not support session catalogs",
        )

    def test_provider_servicer_rejects_missing_post_connect_support(self) -> None:
        plugin = Plugin("source-name")
        servicer = _runtime._provider_servicer(plugin=plugin)
        context = mock.Mock()

        servicer.PostConnect(plugin_pb2.PostConnectRequest(), context)

        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "provider does not support post connect",
        )

    def test_provider_servicer_labels_metadata_failures(self) -> None:
        class BrokenMetadataPlugin(Plugin):
            def supports_post_connect(self) -> bool:
                raise RuntimeError("metadata exploded")

        plugin = BrokenMetadataPlugin("source-name")
        servicer = _runtime._provider_servicer(plugin=plugin)
        context = AbortContext()

        with self.assertRaisesRegex(
            AbortCalled, "provider metadata: metadata exploded"
        ):
            servicer.GetMetadata(mock.Mock(), context)

        self.assertEqual(context.code(), grpc.StatusCode.UNKNOWN)
        self.assertEqual(context.details, "provider metadata: metadata exploded")


class AuthenticationRuntimeTests(unittest.TestCase):
    class StubAuthenticationProvider(
        AuthenticationProvider,
        ExternalTokenValidator,
        SessionTTLProvider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def __init__(self) -> None:
            self.configured: list[tuple[str, dict[str, object]]] = []

        def configure(self, name: str, config: dict[str, Any]) -> None:
            self.configured.append((name, dict(config)))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.AUTHENTICATION,
                name="stub-auth",
                display_name="Stub Auth",
                description="test authentication provider",
                version="1.2.3",
            )

        def warnings(self) -> list[str]:
            return ["set AUTH_ENV"]

        def health_check(self) -> None:
            return None

        def begin_login(self, request: Any) -> Any:
            self.begin_login_request = request
            return authentication_pb2.BeginLoginResponse(
                authorization_url=f"https://auth.example.test/login?state={request.host_state}",
                provider_state=b"provider-state",
            )

        def complete_login(self, request: Any) -> Any:
            self.complete_login_request = request
            return authentication_pb2.AuthenticatedUser(
                email=request.query.get("email", ""),
                display_name="Runtime User",
            )

        def validate_external_token(self, token: str) -> Any:
            if token == "known-token":
                return authentication_pb2.AuthenticatedUser(email="token@example.com")
            return None

        def session_ttl(self) -> dt.timedelta:
            return dt.timedelta(minutes=45)

    class StartableAuthenticationProvider(StubAuthenticationProvider):
        def __init__(self) -> None:
            super().__init__()
            self.started = 0

        def start(self) -> None:
            self.started += 1

    def test_runtime_metadata_and_authentication_servicer(self) -> None:
        provider = self.StubAuthenticationProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.AUTHENTICATION,
        )
        bad_context = AbortContext()
        with self.assertRaisesRegex(
            AbortCalled,
            "host requested protocol version",
        ):
            runtime_servicer.ConfigureProvider(
                runtime_pb2.ConfigureProviderRequest(
                    name="fixture-auth",
                    protocol_version=_runtime.CURRENT_PROTOCOL_VERSION + 1,
                ),
                bad_context,
            )
        self.assertEqual(bad_context.code(), grpc.StatusCode.FAILED_PRECONDITION)
        self.assertEqual(
            bad_context.details,
            f"host requested protocol version {_runtime.CURRENT_PROTOCOL_VERSION + 1}, provider requires {_runtime.CURRENT_PROTOCOL_VERSION}",
        )
        self.assertEqual(provider.configured, [])

        configure_request = runtime_pb2.ConfigureProviderRequest(
            name="fixture-auth",
            protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
        )
        json_format.ParseDict(
            {"issuer": "https://login.example.test"},
            configure_request.config,
        )
        configure_response = runtime_servicer.ConfigureProvider(
            configure_request,
            mock.Mock(),
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(
            meta.kind,
            runtime_pb2.ProviderKind.PROVIDER_KIND_AUTHENTICATION,
        )
        self.assertEqual(meta.name, "stub-auth")
        self.assertEqual(list(meta.warnings), ["set AUTH_ENV"])
        self.assertEqual(
            meta.min_protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            meta.max_protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            configure_response.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(
            provider.configured,
            [("fixture-auth", {"issuer": "https://login.example.test"})],
        )

        auth_servicer = _runtime._authentication_servicer(provider=provider)
        login = auth_servicer.BeginLogin(
            authentication_pb2.BeginLoginRequest(
                callback_url="https://cb.example.test",
                host_state="host-state",
                scopes=["profile"],
                options={"prompt": "consent"},
            ),
            mock.Mock(),
        )
        self.assertEqual(
            login.authorization_url, "https://auth.example.test/login?state=host-state"
        )
        self.assertEqual(bytes(login.provider_state), b"provider-state")
        self.assertIsInstance(provider.begin_login_request, BeginLoginRequest)
        self.assertEqual(provider.begin_login_request.scopes, ["profile"])
        self.assertEqual(provider.begin_login_request.options, {"prompt": "consent"})

        user = auth_servicer.CompleteLogin(
            authentication_pb2.CompleteLoginRequest(
                query={"email": "user@example.com"},
                provider_state=b"provider-state",
                callback_url="https://cb.example.test",
            ),
            mock.Mock(),
        )
        self.assertEqual(user.email, "user@example.com")
        self.assertEqual(user.display_name, "Runtime User")
        self.assertIsInstance(provider.complete_login_request, CompleteLoginRequest)
        self.assertEqual(
            bytes(provider.complete_login_request.provider_state),
            b"provider-state",
        )

        validated = auth_servicer.ValidateExternalToken(
            authentication_pb2.ValidateExternalTokenRequest(token="known-token"),
            mock.Mock(),
        )
        self.assertEqual(validated.email, "token@example.com")

        session_settings = auth_servicer.GetSessionSettings(mock.Mock(), mock.Mock())
        self.assertEqual(session_settings.session_ttl_seconds, 45 * 60)

    def test_runtime_start_provider_is_separate_from_configure(self) -> None:
        provider = self.StartableAuthenticationProvider()
        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.AUTHENTICATION,
        )

        configured = runtime_servicer.ConfigureProvider(
            runtime_pb2.ConfigureProviderRequest(
                name="fixture-auth",
                protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
            ),
            mock.Mock(),
        )
        self.assertEqual(
            configured.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(provider.started, 0)

        started = runtime_servicer.StartProvider(empty_pb2.Empty(), mock.Mock())
        self.assertEqual(
            started.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )
        self.assertEqual(provider.started, 1)

    def test_runtime_start_provider_noops_without_start_hook(self) -> None:
        provider = self.StubAuthenticationProvider()
        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.AUTHENTICATION,
        )

        started = runtime_servicer.StartProvider(empty_pb2.Empty(), mock.Mock())
        self.assertEqual(
            started.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )

    def test_auth_validator_missing_or_unknown_token(self) -> None:
        class NoValidator(AuthenticationProvider):
            def begin_login(self, request: Any) -> Any:
                return authentication_pb2.BeginLoginResponse(
                    authorization_url="https://example.test"
                )

            def complete_login(self, request: Any) -> Any:
                return authentication_pb2.AuthenticatedUser(email="user@example.com")

        no_validator_servicer = _runtime._authentication_servicer(
            provider=NoValidator(),
        )
        context = mock.Mock()
        no_validator_servicer.ValidateExternalToken(
            authentication_pb2.ValidateExternalTokenRequest(token="missing"),
            context,
        )
        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "authentication provider does not support external token validation",
        )

        unknown_context = mock.Mock()
        servicer = _runtime._authentication_servicer(
            provider=self.StubAuthenticationProvider()
        )
        servicer.ValidateExternalToken(
            authentication_pb2.ValidateExternalTokenRequest(token="unknown"),
            unknown_context,
        )
        unknown_context.abort.assert_called_once_with(
            grpc.StatusCode.NOT_FOUND,
            "token not recognized",
        )


class CacheRuntimeTests(unittest.TestCase):
    class FallbackCacheProvider(CacheProvider):
        def __init__(self) -> None:
            self.values: dict[str, bytes] = {}

        def get(self, key: str) -> bytes | None:
            return self.values.get(key)

        def set(
            self,
            key: str,
            value: bytes,
            ttl: dt.timedelta | None = None,
        ) -> None:
            self.values[key] = bytes(value)

        def delete(self, key: str) -> bool:
            return self.values.pop(key, None) is not None

        def touch(self, key: str, ttl: dt.timedelta) -> bool:
            return key in self.values

    class StubCacheProvider(
        CacheProvider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def __init__(self) -> None:
            self.configured: list[tuple[str, dict[str, object]]] = []
            self.values: dict[str, bytes] = {}

        def configure(self, name: str, config: dict[str, Any]) -> None:
            self.configured.append((name, dict(config)))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.CACHE,
                name="stub-cache",
                display_name="Stub Cache",
                description="test cache provider",
                version="1.0.0",
            )

        def warnings(self) -> list[str]:
            return ["set CACHE_ENV"]

        def health_check(self) -> None:
            return None

        def get(self, key: str) -> bytes | None:
            return self.values.get(key)

        def set(
            self,
            key: str,
            value: bytes,
            ttl: dt.timedelta | None = None,
        ) -> None:
            self.values[key] = bytes(value)

        def delete(self, key: str) -> bool:
            return self.values.pop(key, None) is not None

        def touch(self, key: str, ttl: dt.timedelta) -> bool:
            return key in self.values

        def set_many(
            self,
            entries: list[Any],
            ttl: dt.timedelta | None = None,
        ) -> None:
            for entry in entries:
                self.values[entry.key] = bytes(entry.value)

        def get_many(self, keys: list[str]) -> dict[str, bytes]:
            return {
                key: value
                for key in keys
                if (value := self.values.get(key)) is not None
            }

        def delete_many(self, keys: list[str]) -> int:
            deleted = 0
            seen: set[str] = set()
            for key in keys:
                if key in seen:
                    continue
                seen.add(key)
                if self.values.pop(key, None) is not None:
                    deleted += 1
            return deleted

    def test_runtime_metadata_and_cache_servicer(self) -> None:
        provider = self.StubCacheProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.CACHE,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_CACHE)
        self.assertEqual(meta.name, "stub-cache")
        self.assertEqual(list(meta.warnings), ["set CACHE_ENV"])

        cache_servicer = _runtime._cache_servicer(provider=provider)
        cache_servicer.Set(
            cache_pb2.CacheSetRequest(
                key="session",
                value=b"alpha",
            ),
            mock.Mock(),
        )
        self.assertEqual(
            cache_servicer.Get(
                cache_pb2.CacheGetRequest(key="session"),
                mock.Mock(),
            ).value,
            b"alpha",
        )

        cache_servicer.SetMany(
            cache_pb2.CacheSetManyRequest(
                entries=[
                    cache_pb2.CacheSetEntry(key="a", value=b"one"),
                    cache_pb2.CacheSetEntry(key="b", value=b"two"),
                ]
            ),
            mock.Mock(),
        )
        many = cache_servicer.GetMany(
            cache_pb2.CacheGetManyRequest(keys=["session", "a", "missing"]),
            mock.Mock(),
        )
        self.assertEqual(
            [(entry.key, entry.found, bytes(entry.value)) for entry in many.entries],
            [
                ("session", True, b"alpha"),
                ("a", True, b"one"),
                ("missing", False, b""),
            ],
        )
        deleted = cache_servicer.DeleteMany(
            cache_pb2.CacheDeleteManyRequest(keys=["a", "missing", "a"]),
            mock.Mock(),
        )
        self.assertEqual(deleted.deleted, 1)
        self.assertTrue(
            cache_servicer.Touch(
                cache_pb2.CacheTouchRequest(key="session"),
                mock.Mock(),
            ).touched
        )
        self.assertFalse(
            cache_servicer.Touch(
                cache_pb2.CacheTouchRequest(key="missing"),
                mock.Mock(),
            ).touched
        )

    def test_cache_provider_batch_fallbacks(self) -> None:
        provider = self.FallbackCacheProvider()
        provider.set("session", b"alpha")
        provider.set_many(
            [
                CacheEntry(key="a", value=b"one"),
                CacheEntry(key="b", value=b"two"),
            ],
            ttl=dt.timedelta(minutes=5),
        )

        self.assertEqual(
            provider.get_many(["session", "a", "missing"]),
            {
                "session": b"alpha",
                "a": b"one",
            },
        )
        self.assertEqual(provider.delete_many(["a", "missing", "a"]), 1)
        self.assertEqual(
            provider.get_many(["session", "a", "b"]),
            {
                "session": b"alpha",
                "b": b"two",
            },
        )

    def test_runtime_servicer_labels_provider_identity_failures(self) -> None:
        class BrokenWarningsProvider(CacheProvider, WarningsProvider):
            def warnings(self) -> list[str]:
                raise RuntimeError("identity exploded")

            def get(self, key: str) -> bytes | None:
                return None

            def set(
                self,
                key: str,
                value: bytes,
                ttl: dt.timedelta | None = None,
            ) -> None:
                return None

            def delete(self, key: str) -> bool:
                return False

            def touch(self, key: str, ttl: dt.timedelta) -> bool:
                return False

        runtime_servicer = _runtime._runtime_servicer(
            provider=BrokenWarningsProvider(),
            kind=ProviderKind.CACHE,
        )
        context = AbortContext()

        with self.assertRaisesRegex(
            AbortCalled, "provider identity: identity exploded"
        ):
            runtime_servicer.GetProviderIdentity(mock.Mock(), context)

        self.assertEqual(context.code(), grpc.StatusCode.UNKNOWN)
        self.assertEqual(context.details, "provider identity: identity exploded")


class S3RuntimeTests(unittest.TestCase):
    class StubS3Provider(
        S3Provider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def configure(self, name: str, config: dict[str, Any]) -> None:
            self.configured = (name, dict(config))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.S3,
                name="stub-s3",
                display_name="Stub S3",
                description="test s3 provider",
                version="0.1.0",
            )

        def warnings(self) -> list[str]:
            return ["set S3_ENDPOINT"]

        def health_check(self) -> None:
            return None

    def test_runtime_metadata_and_s3_registration(self) -> None:
        provider = self.StubS3Provider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider, kind=ProviderKind.S3
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_S3)
        self.assertEqual(meta.name, "stub-s3")
        self.assertEqual(list(meta.warnings), ["set S3_ENDPOINT"])

        adapter = _runtime._s3_runtime_plugin(provider)
        server = mock.Mock()
        with mock.patch.object(s3_pb2_grpc, "add_S3Servicer_to_server") as add_s3:
            adapter.register_services(server, provider)
        add_s3.assert_called_once()
        wrapped, registered_server = add_s3.call_args.args
        self.assertIsNot(wrapped, provider)
        self.assertIs(getattr(wrapped, "_provider"), provider)
        self.assertIs(registered_server, server)

    def test_servable_target_wraps_s3_provider(self) -> None:
        provider = self.StubS3Provider()
        servable = _runtime._servable_target(provider, runtime_kind=ProviderKind.S3)
        self.assertIsInstance(servable, PluginProviderAdapter)
        servable = cast(PluginProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.S3)
        self.assertIs(servable.provider, provider)


class PluginRuntimeRuntimeTests(unittest.TestCase):
    class StubPluginRuntimeProvider(
        PluginRuntimeProvider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def configure(self, name: str, config: dict[str, Any]) -> None:
            self.configured = (name, dict(config))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.RUNTIME,
                name="stub-runtime",
                display_name="Stub Runtime",
                description="test runtime provider",
                version="0.3.0",
            )

        def warnings(self) -> list[str]:
            return ["set RUNTIME_ENDPOINT"]

        def health_check(self) -> None:
            return None

    def test_normalized_runtime_kind_recognizes_runtime(self) -> None:
        self.assertEqual(
            _runtime._normalized_runtime_kind("runtime"),
            ProviderKind.RUNTIME,
        )

    def test_runtime_metadata_and_plugin_runtime_registration(self) -> None:
        provider = self.StubPluginRuntimeProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.RUNTIME,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_RUNTIME)
        self.assertEqual(meta.name, "stub-runtime")
        self.assertEqual(list(meta.warnings), ["set RUNTIME_ENDPOINT"])

        adapter = _runtime._plugin_runtime_runtime_plugin(provider)
        server = mock.Mock()
        with mock.patch.object(
            pluginruntime_pb2_grpc,
            "add_PluginRuntimeProviderServicer_to_server",
        ) as add_runtime:
            adapter.register_services(server, provider)
        add_runtime.assert_called_once()
        wrapped, registered_server = add_runtime.call_args.args
        self.assertIsNot(wrapped, provider)
        self.assertIs(getattr(wrapped, "_provider"), provider)
        self.assertIs(registered_server, server)

    def test_plugin_runtime_registration_accepts_snake_case_handlers(self) -> None:
        class Provider(PluginRuntimeProvider):
            def get_support(self, request: Any) -> Any:
                self.request = request
                return PluginRuntimeSupport(
                    can_host_plugins=True,
                    supports_prepare_workspace=True,
                )

        provider = Provider()
        server = mock.Mock()
        with mock.patch.object(
            pluginruntime_pb2_grpc,
            "add_PluginRuntimeProviderServicer_to_server",
        ) as add_runtime:
            _runtime._register_plugin_runtime_services(server, provider)

        wrapped, _registered_server = add_runtime.call_args.args
        response = wrapped.GetSupport(empty_pb2.Empty(), object())
        self.assertIsInstance(provider.request, GetPluginRuntimeSupportRequest)
        self.assertEqual(
            response,
            pluginruntime_pb2.PluginRuntimeSupport(
                can_host_plugins=True,
                supports_prepare_workspace=True,
            ),
        )

    def test_servable_target_wraps_plugin_runtime_provider(self) -> None:
        provider = self.StubPluginRuntimeProvider()
        servable = _runtime._servable_target(
            provider,
            runtime_kind=ProviderKind.RUNTIME,
        )
        self.assertIsInstance(servable, PluginProviderAdapter)
        servable = cast(PluginProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.RUNTIME)
        self.assertIs(servable.provider, provider)


class AgentRuntimeTests(unittest.TestCase):
    class StubAgentProvider(AgentProvider):
        pass

    def test_agent_wrapper_accepts_snake_case_one_arg_handler(self) -> None:
        class Provider(AgentProvider):
            def create_session(self, request: Any) -> Any:
                return {"request": request}

        wrapped = _runtime._service_wrapper(
            Provider(),
            agent_pb2_grpc.AgentProviderServicer,
            (("CreateSession", "create_session"),),
        )

        self.assertEqual(
            wrapped.CreateSession("request", object()), {"request": "request"}
        )


class WorkflowRuntimeTests(unittest.TestCase):
    class StubWorkflowProvider(
        WorkflowProvider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def configure(self, name: str, config: dict[str, Any]) -> None:
            self.configured = (name, dict(config))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.WORKFLOW,
                name="stub-workflow",
                display_name="Stub Workflow",
                description="test workflow provider",
                version="0.2.0",
            )

        def warnings(self) -> list[str]:
            return ["set WORKFLOW_ENDPOINT"]

        def health_check(self) -> None:
            return None

    def test_normalized_runtime_kind_recognizes_workflow(self) -> None:
        self.assertEqual(
            _runtime._normalized_runtime_kind("workflow"),
            ProviderKind.WORKFLOW,
        )

    def test_runtime_metadata_and_workflow_registration(self) -> None:
        provider = self.StubWorkflowProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.WORKFLOW,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_WORKFLOW)
        self.assertEqual(meta.name, "stub-workflow")
        self.assertEqual(list(meta.warnings), ["set WORKFLOW_ENDPOINT"])

        adapter = _runtime._workflow_runtime_plugin(provider)
        server = mock.Mock()
        with mock.patch.object(
            workflow_pb2_grpc,
            "add_WorkflowProviderServicer_to_server",
        ) as add_workflow:
            adapter.register_services(server, provider)
        add_workflow.assert_called_once()
        wrapped, registered_server = add_workflow.call_args.args
        self.assertIsNot(wrapped, provider)
        self.assertIs(getattr(wrapped, "_provider"), provider)
        self.assertIs(registered_server, server)

    def test_workflow_wrapper_accepts_snake_case_one_arg_handler(self) -> None:
        class Provider(WorkflowProvider):
            def start_run(self, request: Any) -> Any:
                self.request = request
                return BoundWorkflowRun(id="run-native")

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        response = wrapped.StartRun(
            workflow_pb2.StartWorkflowProviderRunRequest(workflow_key="sync"),
            object(),
        )
        self.assertIsInstance(provider.request, StartWorkflowProviderRunRequest)
        self.assertEqual(provider.request.workflow_key, "sync")
        self.assertEqual(response.id, "run-native")

    def test_workflow_wrapper_returns_pause_resume_schedule_and_trigger(self) -> None:
        class Provider(WorkflowProvider):
            def pause_schedule(self, request: Any) -> Any:
                self.pause_schedule_request = request
                return BoundWorkflowSchedule(id=request.schedule_id, paused=True)

            def resume_schedule(self, request: Any) -> Any:
                self.resume_schedule_request = request
                return BoundWorkflowSchedule(id=request.schedule_id, paused=False)

            def pause_event_trigger(self, request: Any) -> Any:
                self.pause_trigger_request = request
                return BoundWorkflowEventTrigger(id=request.trigger_id, paused=True)

            def resume_event_trigger(self, request: Any) -> Any:
                self.resume_trigger_request = request
                return BoundWorkflowEventTrigger(id=request.trigger_id, paused=False)

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        paused_schedule = wrapped.PauseSchedule(
            workflow_pb2.PauseWorkflowProviderScheduleRequest(schedule_id="schedule-1"),
            object(),
        )
        resumed_schedule = wrapped.ResumeSchedule(
            workflow_pb2.ResumeWorkflowProviderScheduleRequest(
                schedule_id="schedule-1"
            ),
            object(),
        )
        paused_trigger = wrapped.PauseEventTrigger(
            workflow_pb2.PauseWorkflowProviderEventTriggerRequest(
                trigger_id="trigger-1"
            ),
            object(),
        )
        resumed_trigger = wrapped.ResumeEventTrigger(
            workflow_pb2.ResumeWorkflowProviderEventTriggerRequest(
                trigger_id="trigger-1"
            ),
            object(),
        )

        self.assertIsInstance(
            provider.pause_schedule_request,
            PauseWorkflowProviderScheduleRequest,
        )
        self.assertIsInstance(
            provider.resume_schedule_request,
            ResumeWorkflowProviderScheduleRequest,
        )
        self.assertIsInstance(
            provider.pause_trigger_request,
            PauseWorkflowProviderEventTriggerRequest,
        )
        self.assertIsInstance(
            provider.resume_trigger_request,
            ResumeWorkflowProviderEventTriggerRequest,
        )
        self.assertEqual(paused_schedule.id, "schedule-1")
        self.assertTrue(paused_schedule.paused)
        self.assertEqual(resumed_schedule.id, "schedule-1")
        self.assertFalse(resumed_schedule.paused)
        self.assertEqual(paused_trigger.id, "trigger-1")
        self.assertTrue(paused_trigger.paused)
        self.assertEqual(resumed_trigger.id, "trigger-1")
        self.assertFalse(resumed_trigger.paused)

    def test_workflow_wrapper_keeps_pascal_case_rpc_handlers(self) -> None:
        context = object()

        class Provider(WorkflowProvider):
            def StartRun(self, request: Any, rpc_context: Any) -> Any:
                self.called = (request, rpc_context)
                return workflow_pb2.BoundWorkflowRun(id="run-raw")

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)
        raw_request = workflow_pb2.StartWorkflowProviderRunRequest(workflow_key="sync")

        response = wrapped.StartRun(raw_request, context)
        self.assertEqual(provider.called, (raw_request, context))
        self.assertEqual(response.id, "run-raw")

    def test_servable_target_wraps_workflow_provider(self) -> None:
        provider = self.StubWorkflowProvider()
        servable = _runtime._servable_target(
            provider,
            runtime_kind=ProviderKind.WORKFLOW,
        )
        self.assertIsInstance(servable, PluginProviderAdapter)
        servable = cast(PluginProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.WORKFLOW)
        self.assertIs(servable.provider, provider)


if __name__ == "__main__":
    unittest.main()
