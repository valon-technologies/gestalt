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
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from gestalt import (
    CALLER_BEARER_TOKEN_METADATA_KEY,
    App,
    ApplyWorkflowProviderDefinitionRequest,
    AppProviderAdapter,
    AuthorizeRequest,
    CacheProvider,
    CacheSetEntry,
    Catalog,
    CatalogOperation,
    DeliverWorkflowProviderEventRequest,
    GetRuntimeSupportRequest,
    GetWorkflowProviderDefinitionRequest,
    GetWorkflowProviderRunEventsRequest,
    GetWorkflowProviderRunEventsResponse,
    GetWorkflowProviderRunOutputRequest,
    GetWorkflowProviderRunOutputResponse,
    HealthChecker,
    IdentityCallContext,
    IdentityProvider,
    ListWorkflowProviderDefinitionsRequest,
    ListWorkflowProviderDefinitionsResponse,
    ListWorkflowProviderRunsRequest,
    ListWorkflowProviderRunsResponse,
    MetadataProvider,
    ProviderKind,
    ProviderMetadata,
    Request,
    RuntimeProvider,
    RuntimeSupport,
    S3Provider,
    SetWorkflowProviderActivationPausedRequest,
    SetWorkflowProviderDefinitionPausedRequest,
    StartWorkflowProviderRunRequest,
    TokenRequest,
    WarningsProvider,
    WorkflowDefinition,
    WorkflowEvent,
    WorkflowProvider,
    WorkflowRun,
    WorkflowRunEvent,
    _bootstrap,
    _runtime,
    parse_subject_id,
)
from gestalt._gen.v1 import annotations_pb2 as _annotations_pb2
from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt._gen.v1 import app_pb2_grpc as _app_pb2_grpc
from gestalt._gen.v1 import cache_pb2 as _cache_pb2
from gestalt._gen.v1 import identity_pb2 as _identity_pb2
from gestalt._gen.v1 import runtime_pb2 as _runtime_pb2
from gestalt._gen.v1 import runtime_provider_pb2 as _runtime_provider_pb2
from gestalt._gen.v1 import runtime_provider_pb2_grpc as _runtime_provider_pb2_grpc
from gestalt._gen.v1 import s3_pb2_grpc as _s3_pb2_grpc
from gestalt._gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt._gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

identity_pb2: Any = _identity_pb2
cache_pb2: Any = _cache_pb2
empty_pb2: Any = _empty_pb2
app_pb2: Any = _app_pb2
app_pb2_grpc: Any = _app_pb2_grpc
runtime_provider_pb2: Any = _runtime_provider_pb2
runtime_provider_pb2_grpc: Any = _runtime_provider_pb2_grpc
runtime_pb2: Any = _runtime_pb2
annotations_pb2: Any = _annotations_pb2
s3_pb2_grpc: Any = _s3_pb2_grpc
struct_pb2: Any = _struct_pb2
workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc

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
            ["/tmp/plugin", "example.plugin:PLUGIN", "identity"]
        )

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="example.plugin:PLUGIN",
                root=pathlib.Path("/tmp/plugin"),
                runtime_kind="identity",
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
                        "app_name": "released-plugin",
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
                app_name="released-plugin",
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


class RuntimeServeTransportTests(unittest.TestCase):
    def test_runtime_serve_supports_tcp_provider_sockets(self) -> None:
        app = App("tcp-runtime")

        @app.operation
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
                    _runtime.serve(app)
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
            stub = app_pb2_grpc.AppProviderStub(channel)

            metadata = stub.GetMetadata(empty_pb2.Empty(), timeout=5)
            started = stub.StartProvider(
                app_pb2.StartProviderRequest(
                    name="tcp-runtime",
                    protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
                ),
                timeout=5,
            )
            result = stub.Execute(
                app_pb2.ExecuteRequest(
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

            manifest_dir = temp_root / "app.json"
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
                "source: !env github.com/acme/apps/tagged-provider\n"
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
                    app = App.from_manifest(manifest_input)
                    self.assertEqual(app.name, expected_name)


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
        self.assertIsNone(request.context)


class MainEntrypointTests(unittest.TestCase):
    def test_writes_catalog_when_env_is_set(self) -> None:
        app = App("test-plugin")

        @app.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            catalog_path = pathlib.Path(tmpdir) / "catalog.yaml"
            with (
                mock.patch.object(_runtime, "_load_target", return_value=app),
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
        app = App("source-name")
        configured: list[tuple[str, dict[str, Any]]] = []

        @app.configure
        def configure(name: str, config: dict[str, Any]) -> None:
            configured.append((name, dict(config)))

        @app.operation
        def whoami(request: Request) -> dict[str, Any]:
            workflow_trigger = request.workflow.get("trigger")
            workflow_trigger_kind = (
                workflow_trigger.get("kind", "")
                if isinstance(workflow_trigger, dict)
                else ""
            )
            return {
                "token": request.token,
                "subject_id": request.subject.id,
                "subject_kind": (parse_subject_id(request.subject.id) or ("", ""))[0],
                "subject_email": request.subject.email,
                "agent_subject_id": request.agent_subject.id,
                "agent_subject_email": request.agent_subject.email,
                "credential_mode": request.credential.mode,
                "credential_subject_id": request.credential.subject_id,
                "access_policy": request.access.policy,
                "access_role": request.access.role,
                "host_base_url": request.host.public_base_url,
                "idempotency_key": request.idempotency_key,
                "workflow_run_id": str(request.workflow.get("runId", "")),
                "workflow_trigger_kind": str(workflow_trigger_kind),
                "workflow": request.workflow,
                "tool_refs_set": request.tool_refs_set,
                "tool_ref_app": request.tool_refs[0].app if request.tool_refs else "",
                "tool_ref_operation": request.tool_refs[0].operation
                if request.tool_refs
                else "",
                "tool_ref_run_as": request.tool_refs[0].run_as.id
                if request.tool_refs and request.tool_refs[0].run_as
                else "",
            }

        @app.session_catalog
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
                    "activationId": "activation-1",
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

        servicer = _runtime._provider_servicer(app=app)
        metadata = servicer.GetMetadata(mock.Mock(), mock.Mock())
        bad_context = AbortContext()
        with self.assertRaisesRegex(
            AbortCalled,
            "host requested protocol version",
        ):
            servicer.StartProvider(
                app_pb2.StartProviderRequest(
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

        start_request = app_pb2.StartProviderRequest(
            name="source-instance",
            protocol_version=_runtime.CURRENT_PROTOCOL_VERSION,
        )
        json_format.ParseDict({"region": "use1"}, start_request.config)
        start_response = servicer.StartProvider(start_request, mock.Mock())
        execute_response = servicer.Execute(
            app_pb2.ExecuteRequest(
                operation="whoami",
                token="secret-token",
                idempotency_key=" tool-call-123 ",
                context=app_pb2.RequestContext(
                    subject=app_pb2.SubjectContext(
                        id="user:user-123",
                        email="ada@example.com",
                    ),
                    agent_subject=app_pb2.SubjectContext(
                        id="user:user-456",
                        email="grace@example.com",
                    ),
                    credential=app_pb2.CredentialContext(
                        mode="subject",
                        subject_id="user:user-123",
                    ),
                    access=app_pb2.AccessContext(
                        policy="sample_policy",
                        role="admin",
                    ),
                    host=app_pb2.HostContext(
                        public_base_url="https://gestalt.example.test",
                    ),
                    workflow=execute_workflow,
                    tool_refs=[
                        app_pb2.AgentToolRef(
                            app="github",
                            operation="bot.getPullRequest",
                            run_as=app_pb2.SubjectContext(
                                id="service_account:github-review",
                            ),
                        )
                    ],
                    tool_refs_set=True,
                ),
            ),
            mock.Mock(),
        )
        response = servicer.GetSessionCatalog(
            app_pb2.GetSessionCatalogRequest(
                token="secret-token",
                connection_params={"tenant": "acme"},
                context=app_pb2.RequestContext(
                    subject=app_pb2.SubjectContext(id="user:user-123"),
                    credential=app_pb2.CredentialContext(mode="subject"),
                    access=app_pb2.AccessContext(
                        policy="sample_policy",
                        role="viewer",
                    ),
                    host=app_pb2.HostContext(
                        public_base_url="https://gestalt.example.test",
                    ),
                    workflow=catalog_workflow,
                ),
            ),
            mock.Mock(),
        )
        self.assertTrue(metadata.supports_session_catalog)
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
                "credential_mode": "subject",
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
                        "activationId": "activation-1",
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
                "tool_ref_app": "github",
                "tool_ref_operation": "bot.getPullRequest",
                "tool_ref_run_as": "service_account:github-review",
            },
        )
        catalog = response.catalog
        self.assertEqual(catalog.name, "session-source")
        self.assertEqual(
            catalog.display_name,
            "acme|user:user-123|subject|viewer|https://gestalt.example.test|run-456",
        )
        self.assertEqual(len(catalog.operations), 1)
        self.assertEqual(catalog.operations[0].id, "private_search")
        self.assertEqual(catalog.operations[0].method, "POST")
        self.assertEqual(list(catalog.operations[0].allowed_roles), ["viewer", "admin"])
    def test_provider_servicer_sanitizes_unhandled_execute_exceptions(self) -> None:
        app = App("source-name")

        @app.operation
        def broken() -> None:
            raise RuntimeError("sensitive details")

        servicer = _runtime._provider_servicer(app=app)
        execute_response = servicer.Execute(
            app_pb2.ExecuteRequest(operation="broken"),
            mock.Mock(),
        )

        self.assertEqual(execute_response.status, 500)
        self.assertEqual(
            execute_response.headers["Content-Type"].values,
            ["application/json"],
        )
        self.assertEqual(json.loads(execute_response.body), {"error": "internal error"})

    def test_provider_servicer_rejects_missing_session_catalog_support(self) -> None:
        app = App("source-name")
        servicer = _runtime._provider_servicer(app=app)
        context = mock.Mock()

        servicer.GetSessionCatalog(app_pb2.GetSessionCatalogRequest(), context)

        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "provider does not support session catalogs",
        )

    def test_provider_servicer_labels_metadata_failures(self) -> None:
        class BrokenMetadataApp(App):
            def supports_session_catalog(self) -> bool:
                raise RuntimeError("metadata exploded")

        app = BrokenMetadataApp("source-name")
        servicer = _runtime._provider_servicer(app=app)
        context = AbortContext()

        with self.assertRaisesRegex(
            AbortCalled, "provider metadata: metadata exploded"
        ):
            servicer.GetMetadata(mock.Mock(), context)

        self.assertEqual(context.code(), grpc.StatusCode.UNKNOWN)
        self.assertEqual(context.details, "provider metadata: metadata exploded")


class AuthenticationRuntimeTests(unittest.TestCase):
    class StubIdentityProvider(
        IdentityProvider,
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
                kind=ProviderKind.IDENTITY,
                name="stub-auth",
                display_name="Stub Auth",
                description="test identity provider",
                version="1.2.3",
            )

        def warnings(self) -> list[str]:
            return ["set AUTH_ENV"]

        def health_check(self) -> None:
            return None

        def authorize(self, request: AuthorizeRequest) -> Any:
            self.authorize_request = request
            return identity_pb2.AuthorizeResponse(
                redirect_uri=f"https://auth.example.test/login?state={request.state}",
            )

        def token(self, request: TokenRequest) -> Any:
            self.token_request = request
            return identity_pb2.TokenResponse(
                access_token="fixture-access-token",
                token_type="Bearer",
                expires_in=5400,
                scope="openid email",
                grant_id="grant-fixture-1",
            )

        def introspect(self, request: Any) -> Any:
            if request.token == "fixture-access-token":
                return identity_pb2.IntrospectResponse(
                    active=True,
                    subject="user:fixture@example.com",
                    scope="openid email",
                    client_id="gestaltd",
                )
            return identity_pb2.IntrospectResponse(active=False)

        def list_grants(self, request: Any, call: IdentityCallContext) -> Any:
            self.list_grants_request = request
            self.list_grants_call = call
            return identity_pb2.ListGrantsResponse(
                grant_ids=["grant-fixture-1"],
            )

        def get_grant(self, request: Any, call: IdentityCallContext) -> Any:
            self.get_grant_request = request
            self.get_grant_call = call
            return identity_pb2.GetGrantResponse(
                scopes=[identity_pb2.GrantScope(scope="openid")],
                created_at=1_700_000_000,
                expires_at=1_800_000_000,
            )

        def revoke_grant(self, request: Any, call: IdentityCallContext) -> Any:
            self.revoke_grant_request = request
            self.revoke_grant_call = call
            return identity_pb2.RevokeGrantResponse()

    class StartableIdentityProvider(StubIdentityProvider):
        def __init__(self) -> None:
            super().__init__()
            self.started = 0

        def start(self) -> None:
            self.started += 1

    def test_runtime_metadata_and_authentication_servicer(self) -> None:
        provider = self.StubIdentityProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.IDENTITY,
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
            annotations_pb2.ProviderKind.PROVIDER_KIND_IDENTITY,
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

        auth_servicer = _runtime._identity_servicer(provider=provider)
        authorize = auth_servicer.Authorize(
            identity_pb2.AuthorizeRequest(
                response_type="code",
                client_id="gestaltd",
                redirect_uri="https://cb.example.test",
                scope="profile",
                state="host-state",
            ),
            mock.Mock(),
        )
        self.assertEqual(
            authorize.redirect_uri,
            "https://auth.example.test/login?state=host-state",
        )
        self.assertIsInstance(provider.authorize_request, AuthorizeRequest)
        self.assertEqual(provider.authorize_request.scope, "profile")
        self.assertEqual(provider.authorize_request.state, "host-state")

        token = auth_servicer.Token(
            identity_pb2.TokenRequest(
                grant_type="authorization_code",
                code="auth-code",
                redirect_uri="https://cb.example.test",
                client_id="gestaltd",
                state="host-state",
            ),
            mock.Mock(),
        )
        self.assertEqual(token.access_token, "fixture-access-token")
        self.assertEqual(token.grant_id, "grant-fixture-1")
        self.assertIsInstance(provider.token_request, TokenRequest)
        self.assertEqual(provider.token_request.code, "auth-code")

        introspection = auth_servicer.Introspect(
            identity_pb2.IntrospectRequest(token="fixture-access-token"),
            mock.Mock(),
        )
        self.assertTrue(introspection.active)
        self.assertEqual(introspection.subject, "user:fixture@example.com")

        grants = auth_servicer.ListGrants(
            identity_pb2.ListGrantsRequest(),
            mock.Mock(
                invocation_metadata=mock.Mock(
                    return_value=(
                        (CALLER_BEARER_TOKEN_METADATA_KEY, b"caller-bearer-token"),
                    )
                )
            ),
        )
        self.assertEqual(list(grants.grant_ids), ["grant-fixture-1"])
        self.assertIsInstance(provider.list_grants_call, IdentityCallContext)
        self.assertEqual(
            provider.list_grants_call.caller_bearer_token,
            "caller-bearer-token",
        )

        grant = auth_servicer.GetGrant(
            identity_pb2.GetGrantRequest(grant_id="grant-fixture-1"),
            mock.Mock(
                invocation_metadata=mock.Mock(
                    return_value=(
                        (CALLER_BEARER_TOKEN_METADATA_KEY, "caller-bearer-token"),
                    )
                )
            ),
        )
        self.assertEqual(grant.scopes[0].scope, "openid")
        self.assertEqual(provider.get_grant_call.caller_bearer_token, "caller-bearer-token")

        revoked = auth_servicer.RevokeGrant(
            identity_pb2.RevokeGrantRequest(grant_id="grant-fixture-1"),
            mock.Mock(
                invocation_metadata=mock.Mock(
                    return_value=(
                        (CALLER_BEARER_TOKEN_METADATA_KEY, "caller-bearer-token"),
                    )
                )
            ),
        )
        self.assertIsNotNone(revoked)
        self.assertEqual(
            provider.revoke_grant_call.caller_bearer_token,
            "caller-bearer-token",
        )

    def test_runtime_start_provider_is_separate_from_configure(self) -> None:
        provider = self.StartableIdentityProvider()
        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.IDENTITY,
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
        provider = self.StubIdentityProvider()
        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.IDENTITY,
        )

        started = runtime_servicer.StartProvider(empty_pb2.Empty(), mock.Mock())
        self.assertEqual(
            started.protocol_version,
            _runtime.CURRENT_PROTOCOL_VERSION,
        )

    def test_auth_introspect_inactive_token(self) -> None:
        servicer = _runtime._identity_servicer(
            provider=self.StubIdentityProvider()
        )
        introspection = servicer.Introspect(
            identity_pb2.IntrospectRequest(token="unknown"),
            mock.Mock(),
        )
        self.assertFalse(introspection.active)


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
        self.assertEqual(meta.kind, annotations_pb2.ProviderKind.PROVIDER_KIND_CACHE)
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
                CacheSetEntry(key="a", value=b"one"),
                CacheSetEntry(key="b", value=b"two"),
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
        self.assertEqual(meta.kind, annotations_pb2.ProviderKind.PROVIDER_KIND_S3)
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
        self.assertIsInstance(servable, AppProviderAdapter)
        servable = cast(AppProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.S3)
        self.assertIs(servable.provider, provider)


class RuntimeRuntimeTests(unittest.TestCase):
    class StubRuntimeProvider(
        RuntimeProvider,
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

    def test_runtime_metadata_and_runtime_provider_registration(self) -> None:
        provider = self.StubRuntimeProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.RUNTIME,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, annotations_pb2.ProviderKind.PROVIDER_KIND_RUNTIME)
        self.assertEqual(meta.name, "stub-runtime")
        self.assertEqual(list(meta.warnings), ["set RUNTIME_ENDPOINT"])

        adapter = _runtime._runtime_provider_runtime_app(provider)
        server = mock.Mock()
        with mock.patch.object(
            runtime_provider_pb2_grpc,
            "add_RuntimeServicer_to_server",
        ) as add_runtime:
            adapter.register_services(server, provider)
        add_runtime.assert_called_once()
        wrapped, registered_server = add_runtime.call_args.args
        self.assertIsNot(wrapped, provider)
        self.assertIs(getattr(wrapped, "_provider"), provider)
        self.assertIs(registered_server, server)

    def test_runtime_provider_registration_accepts_snake_case_handlers(self) -> None:
        class Provider(RuntimeProvider):
            def get_support(self, request: Any) -> Any:
                self.request = request
                return RuntimeSupport(
                    can_host_apps=True,
                    supports_prepare_workspace=True,
                )

        provider = Provider()
        server = mock.Mock()
        with mock.patch.object(
            runtime_provider_pb2_grpc,
            "add_RuntimeServicer_to_server",
        ) as add_runtime:
            _runtime._register_runtime_provider_services(server, provider)

        wrapped, _registered_server = add_runtime.call_args.args
        response = wrapped.GetSupport(empty_pb2.Empty(), object())
        self.assertIsInstance(provider.request, GetRuntimeSupportRequest)
        self.assertEqual(
            response,
            runtime_provider_pb2.RuntimeSupport(
                can_host_apps=True,
                supports_prepare_workspace=True,
            ),
        )

    def test_servable_target_wraps_runtime_provider(self) -> None:
        provider = self.StubRuntimeProvider()
        servable = _runtime._servable_target(
            provider,
            runtime_kind=ProviderKind.RUNTIME,
        )
        self.assertIsInstance(servable, AppProviderAdapter)
        servable = cast(AppProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.RUNTIME)
        self.assertIs(servable.provider, provider)


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

    def test_runtime_metadata_and_workflow_registration(self) -> None:
        provider = self.StubWorkflowProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.WORKFLOW,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, annotations_pb2.ProviderKind.PROVIDER_KIND_WORKFLOW)
        self.assertEqual(meta.name, "stub-workflow")
        self.assertEqual(list(meta.warnings), ["set WORKFLOW_ENDPOINT"])

        adapter = _runtime._workflow_runtime_adapter(provider)
        server = mock.Mock()
        with mock.patch.object(
            workflow_pb2_grpc,
            "add_WorkflowServicer_to_server",
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
                return WorkflowRun(id="run-native")

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        response = wrapped.StartRun(
            workflow_pb2.StartWorkflowProviderRunRequest(workflow_key="sync"),
            object(),
        )
        self.assertIsInstance(provider.request, StartWorkflowProviderRunRequest)
        self.assertEqual(provider.request.workflow_key, "sync")
        self.assertEqual(response.id, "run-native")

    def test_workflow_wrapper_maps_definition_contract(self) -> None:
        class Provider(WorkflowProvider):
            def apply_definition(self, request: Any) -> Any:
                self.apply_request = request
                assert request.spec is not None
                return WorkflowDefinition(
                    id=request.spec.id,
                    target=request.spec.target,
                    generation=3,
                )

            def get_definition(self, request: Any) -> Any:
                self.get_request = request
                return WorkflowDefinition(id=request.definition_id)

            def list_definitions(self, request: Any) -> Any:
                self.list_request = request
                return ListWorkflowProviderDefinitionsResponse(
                    definitions=[WorkflowDefinition(id="definition-1")]
                )

            def set_definition_paused(self, request: Any) -> Any:
                self.definition_pause_request = request
                return WorkflowDefinition(
                    id=request.definition_id,
                    paused=request.paused,
                )

            def set_activation_paused(self, request: Any) -> Any:
                self.activation_pause_request = request
                return WorkflowDefinition(
                    id=request.definition_id,
                    activations=[],
                    paused=False,
                )

            def delete_definition(self, request: Any) -> None:
                self.delete_request = request

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)
        target = workflow_pb2.BoundWorkflowTarget(
            steps=[
                workflow_pb2.WorkflowStep(
                    id="sync",
                    app=workflow_pb2.WorkflowStepAppCall(
                        name="demo",
                        operation="sync",
                    ),
                )
            ]
        )

        applied = wrapped.ApplyDefinition(
            workflow_pb2.ApplyWorkflowProviderDefinitionRequest(
                idempotency_key="definition-1",
                spec=workflow_pb2.WorkflowDefinitionSpec(
                    id="definition-1",
                    target=target,
                ),
            ),
            object(),
        )
        fetched = wrapped.GetDefinition(
            workflow_pb2.GetWorkflowProviderDefinitionRequest(
                definition_id="definition-1"
            ),
            object(),
        )
        listed = wrapped.ListDefinitions(
            workflow_pb2.ListWorkflowProviderDefinitionsRequest(),
            object(),
        )
        definition_paused = wrapped.SetDefinitionPaused(
            workflow_pb2.SetWorkflowProviderDefinitionPausedRequest(
                definition_id="definition-1",
                paused=True,
            ),
            object(),
        )
        activation_paused = wrapped.SetActivationPaused(
            workflow_pb2.SetWorkflowProviderActivationPausedRequest(
                definition_id="definition-1",
                activation_id="github",
                paused=True,
            ),
            object(),
        )
        deleted = wrapped.DeleteDefinition(
            workflow_pb2.DeleteWorkflowProviderDefinitionRequest(
                definition_id="definition-1"
            ),
            object(),
        )

        self.assertIsInstance(
            provider.apply_request,
            ApplyWorkflowProviderDefinitionRequest,
        )
        self.assertIsInstance(
            provider.get_request, GetWorkflowProviderDefinitionRequest
        )
        self.assertIsInstance(
            provider.list_request,
            ListWorkflowProviderDefinitionsRequest,
        )
        self.assertIsInstance(
            provider.definition_pause_request,
            SetWorkflowProviderDefinitionPausedRequest,
        )
        self.assertIsInstance(
            provider.activation_pause_request,
            SetWorkflowProviderActivationPausedRequest,
        )
        self.assertEqual(provider.delete_request.definition_id, "definition-1")
        self.assertEqual(applied.id, "definition-1")
        self.assertEqual(fetched.id, "definition-1")
        self.assertEqual([definition.id for definition in listed.definitions], ["definition-1"])
        self.assertEqual(definition_paused.id, "definition-1")
        self.assertTrue(definition_paused.paused)
        self.assertEqual(activation_paused.id, "definition-1")
        self.assertIsInstance(deleted, empty_pb2.Empty)

    def test_workflow_wrapper_returns_delivered_event(self) -> None:
        class Provider(WorkflowProvider):
            def deliver_event(self, request: Any) -> Any:
                self.request = request
                return WorkflowEvent(
                    id="published-py",
                    source=request.event.source,
                    type=request.event.type,
                )

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        response = wrapped.DeliverEvent(
            workflow_pb2.DeliverWorkflowProviderEventRequest(
                provider_name="workflow-runtime",
                event=workflow_pb2.WorkflowEvent(
                    source="github",
                    type="github.app.webhook",
                ),
            ),
            object(),
        )

        self.assertIsInstance(provider.request, DeliverWorkflowProviderEventRequest)
        self.assertEqual(provider.request.provider_name, "workflow-runtime")
        self.assertEqual(response.id, "published-py")
        self.assertEqual(response.source, "github")

    def test_workflow_wrapper_maps_list_runs_pagination(self) -> None:
        class Provider(WorkflowProvider):
            def list_runs(self, request: Any) -> Any:
                self.request = request
                return ListWorkflowProviderRunsResponse(
                    runs=[WorkflowRun(id="run-page")],
                    next_page_token="next-page",
                )

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        response = wrapped.ListRuns(
            workflow_pb2.ListWorkflowProviderRunsRequest(
                page_size=25,
                page_token="page-0",
                status=workflow_pb2.WORKFLOW_RUN_STATUS_RUNNING,
            ),
            object(),
        )

        self.assertIsInstance(provider.request, ListWorkflowProviderRunsRequest)
        self.assertEqual(provider.request.page_size, 25)
        self.assertEqual(provider.request.page_token, "page-0")
        self.assertEqual(
            provider.request.status, workflow_pb2.WORKFLOW_RUN_STATUS_RUNNING
        )
        self.assertEqual([run.id for run in response.runs], ["run-page"])
        self.assertEqual(response.next_page_token, "next-page")

    def test_workflow_wrapper_returns_run_events_and_output(self) -> None:
        class Provider(WorkflowProvider):
            def get_run_events(self, request: Any) -> Any:
                self.events_request = request
                return GetWorkflowProviderRunEventsResponse(
                    events=[
                        WorkflowRunEvent(
                            id="event-1",
                            run_id=request.run_id,
                            step_id="review",
                            type="step.succeeded",
                            data={"ok": True},
                        )
                    ]
                )

            def get_run_output(self, request: Any) -> Any:
                self.output_request = request
                return GetWorkflowProviderRunOutputResponse(output={"ok": True})

        provider = Provider()
        wrapped = _runtime._workflow_provider_servicer(provider)

        events = wrapped.GetRunEvents(
            workflow_pb2.GetWorkflowProviderRunEventsRequest(run_id="run-1"),
            object(),
        )
        output = wrapped.GetRunOutput(
            workflow_pb2.GetWorkflowProviderRunOutputRequest(run_id="run-1"),
            object(),
        )

        self.assertIsInstance(
            provider.events_request,
            GetWorkflowProviderRunEventsRequest,
        )
        self.assertIsInstance(
            provider.output_request,
            GetWorkflowProviderRunOutputRequest,
        )
        self.assertEqual(provider.events_request.run_id, "run-1")
        self.assertEqual(provider.output_request.run_id, "run-1")
        self.assertEqual([event.id for event in events.events], ["event-1"])
        self.assertEqual(events.events[0].data.fields["ok"].bool_value, True)
        self.assertEqual(output.output.struct_value.fields["ok"].bool_value, True)

    def test_workflow_wrapper_keeps_pascal_case_rpc_handlers(self) -> None:
        context = object()

        class Provider(WorkflowProvider):
            def StartRun(self, request: Any, rpc_context: Any) -> Any:
                self.called = (request, rpc_context)
                return workflow_pb2.WorkflowRun(id="run-raw")

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
        self.assertIsInstance(servable, AppProviderAdapter)
        servable = cast(AppProviderAdapter, servable)
        self.assertEqual(servable.kind, ProviderKind.WORKFLOW)
        self.assertIs(servable.provider, provider)


if __name__ == "__main__":
    unittest.main()
