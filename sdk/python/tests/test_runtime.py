import datetime as dt
import json
import pathlib
import tempfile
import unittest
from unittest import mock

import grpc

from gestalt import (
    AuthenticatedUser,
    AuthProvider,
    BeginLoginRequest,
    BeginLoginResponse,
    Catalog,
    CatalogOperation,
    DatastoreProvider,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    OAuthRegistration,
    OAuthRegistrationStore,
    Plugin,
    ProviderKind,
    ProviderMetadata,
    Request,
    SessionTTLProvider,
    StoredAPIToken,
    StoredIntegrationToken,
    StoredUser,
    WarningsProvider,
    _bootstrap,
    _runtime,
)
from gestalt.gen.v1 import auth_pb2, datastore_pb2, plugin_pb2, runtime_pb2

UTC = dt.timezone.utc


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
                runtime_kind="auth",
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
                        "runtime_kind": "datastore",
                    }
                ),
                encoding="utf-8",
            )

            with mock.patch.object(_runtime.sys, "_MEIPASS", str(bundle_dir), create=True):
                runtime_args = _runtime._parse_runtime_args([])

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="provider",
                plugin_name="released-plugin",
                runtime_kind="datastore",
            ),
        )

    def test_defaults_runtime_kind_to_integration(self) -> None:
        runtime_args = _runtime._parse_runtime_args(["/tmp/plugin", "example.plugin:PLUGIN"])
        self.assertIsNotNone(runtime_args)
        assert runtime_args is not None
        self.assertEqual(runtime_args.runtime_kind, "integration")

    def test_returns_none_without_args_or_bundled_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.object(_runtime.sys, "_MEIPASS", tmpdir, create=True):
                self.assertIsNone(_runtime._parse_runtime_args([]))


class ManifestNameTests(unittest.TestCase):
    def test_display_name_variants(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            temp_root = pathlib.Path(tmpdir)

            manifest_path = temp_root / "plugin.yaml"
            manifest_path.write_text('display_name: "Released Plugin"\n', encoding="utf-8")

            manifest_dir = temp_root / "plugin.json"
            manifest_dir.mkdir()
            (manifest_dir / "plugin.yaml").write_text(
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
    def test_connection_param_returns_value_or_empty_string(self) -> None:
        request = Request(connection_params={"region": "us-east-1"})

        self.assertEqual(request.connection_param("region"), "us-east-1")
        self.assertEqual(request.connection_param("missing"), "")


class MainEntrypointTests(unittest.TestCase):
    def test_writes_catalog_when_env_is_set(self) -> None:
        plugin = Plugin("test-plugin")

        @plugin.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            catalog_path = pathlib.Path(tmpdir) / "catalog.yaml"
            with mock.patch.object(_runtime, "_load_target", return_value=plugin), mock.patch.dict(
                _runtime.os.environ,
                {_runtime.ENV_WRITE_CATALOG: str(catalog_path)},
                clear=True,
            ):
                result = _runtime.main(["/tmp/plugin", "example.plugin:PLUGIN"])

            self.assertEqual(result, 0)
            self.assertTrue(catalog_path.exists())

    def test_returns_usage_error_for_bad_args(self) -> None:
        result = _runtime.main(["/only-one-arg"])
        self.assertEqual(result, 2)

    def test_provider_servicer_reports_and_serves_session_catalogs(self) -> None:
        plugin = Plugin("source-name")

        @plugin.session_catalog
        def dynamic_catalog(request: Request) -> Catalog:
            return Catalog(
                name="session-source",
                display_name=request.connection_param("tenant"),
                operations=[CatalogOperation(id="private_search", method="POST")],
            )

        servicer = _runtime._provider_servicer(plugin=plugin)
        metadata = servicer.GetMetadata(mock.Mock(), mock.Mock())
        response = servicer.GetSessionCatalog(
            plugin_pb2.GetSessionCatalogRequest(
                token="secret-token",
                connection_params={"tenant": "acme"},
            ),
            mock.Mock(),
        )

        self.assertTrue(metadata.supports_session_catalog)
        self.assertEqual(
            json.loads(response.catalog_json),
            {
                "name": "session-source",
                "displayName": "acme",
                "operations": [
                    {
                        "id": "private_search",
                        "method": "POST",
                    }
                ],
            },
        )

    def test_provider_servicer_rejects_missing_session_catalog_support(self) -> None:
        plugin = Plugin("source-name")
        servicer = _runtime._provider_servicer(plugin=plugin)
        context = mock.Mock()

        servicer.GetSessionCatalog(plugin_pb2.GetSessionCatalogRequest(), context)

        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "provider does not support session catalogs",
        )


class AuthRuntimeTests(unittest.TestCase):
    class StubAuthProvider(
        AuthProvider,
        ExternalTokenValidator,
        SessionTTLProvider,
        MetadataProvider,
        WarningsProvider,
        HealthChecker,
    ):
        def __init__(self) -> None:
            self.configured: list[tuple[str, dict[str, object]]] = []

        def configure(self, name: str, config: dict[str, object]) -> None:
            self.configured.append((name, dict(config)))

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.AUTH,
                name="stub-auth",
                display_name="Stub Auth",
                description="test auth provider",
                version="1.2.3",
            )

        def warnings(self) -> list[str]:
            return ["set AUTH_ENV"]

        def health_check(self) -> None:
            return None

        def begin_login(self, request: BeginLoginRequest) -> BeginLoginResponse:
            return BeginLoginResponse(
                authorization_url=f"https://auth.example.test/login?state={request.host_state}",
                provider_state=b"provider-state",
            )

        def complete_login(self, request: _runtime.CompleteLoginRequest) -> AuthenticatedUser:
            return AuthenticatedUser(
                email=request.query.get("email", ""),
                display_name="Runtime User",
            )

        def validate_external_token(self, token: str) -> AuthenticatedUser | None:
            if token == "known-token":
                return AuthenticatedUser(email="token@example.com")
            return None

        def session_ttl(self) -> dt.timedelta:
            return dt.timedelta(minutes=45)

    def test_runtime_metadata_and_auth_servicer(self) -> None:
        provider = self.StubAuthProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.AUTH,
        )
        meta = runtime_servicer.GetPluginMetadata(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.PluginKind.PLUGIN_KIND_AUTH)
        self.assertEqual(meta.name, "stub-auth")
        self.assertEqual(list(meta.warnings), ["set AUTH_ENV"])

        auth_servicer = _runtime._auth_servicer(provider=provider)
        login = auth_servicer.BeginLogin(
            auth_pb2.BeginLoginRequest(
                callback_url="https://cb.example.test",
                host_state="host-state",
                scopes=["profile"],
                options={"prompt": "consent"},
            ),
            mock.Mock(),
        )
        self.assertEqual(login.authorization_url, "https://auth.example.test/login?state=host-state")
        self.assertEqual(bytes(login.plugin_state), b"provider-state")

        user = auth_servicer.CompleteLogin(
            auth_pb2.CompleteLoginRequest(
                query={"email": "user@example.com"},
                plugin_state=b"provider-state",
                callback_url="https://cb.example.test",
            ),
            mock.Mock(),
        )
        self.assertEqual(user.email, "user@example.com")
        self.assertEqual(user.display_name, "Runtime User")

        validated = auth_servicer.ValidateExternalToken(
            auth_pb2.ValidateExternalTokenRequest(token="known-token"),
            mock.Mock(),
        )
        self.assertEqual(validated.email, "token@example.com")

        session_settings = auth_servicer.GetSessionSettings(mock.Mock(), mock.Mock())
        self.assertEqual(session_settings.session_ttl_seconds, 45 * 60)

    def test_auth_validator_missing_or_unknown_token(self) -> None:
        class NoValidator(AuthProvider):
            def begin_login(self, request: BeginLoginRequest) -> BeginLoginResponse:
                return BeginLoginResponse(authorization_url="https://example.test")

            def complete_login(self, request: _runtime.CompleteLoginRequest) -> AuthenticatedUser:
                return AuthenticatedUser(email="user@example.com")

        no_validator_servicer = _runtime._auth_servicer(
            provider=NoValidator(),
        )
        context = mock.Mock()
        no_validator_servicer.ValidateExternalToken(
            auth_pb2.ValidateExternalTokenRequest(token="missing"),
            context,
        )
        context.abort.assert_called_once_with(
            grpc.StatusCode.UNIMPLEMENTED,
            "auth provider does not support external token validation",
        )

        unknown_context = mock.Mock()
        servicer = _runtime._auth_servicer(provider=self.StubAuthProvider())
        servicer.ValidateExternalToken(
            auth_pb2.ValidateExternalTokenRequest(token="unknown"),
            unknown_context,
        )
        unknown_context.abort.assert_called_once_with(
            grpc.StatusCode.NOT_FOUND,
            "token not recognized",
        )


class DatastoreRuntimeTests(unittest.TestCase):
    class StubDatastoreProvider(
        DatastoreProvider,
        MetadataProvider,
        HealthChecker,
        WarningsProvider,
        OAuthRegistrationStore,
    ):
        def __init__(self) -> None:
            self.migrated = False
            self.users: dict[str, StoredUser] = {}
            self.tokens: dict[str, StoredIntegrationToken] = {}

        def metadata(self) -> ProviderMetadata:
            return ProviderMetadata(
                kind=ProviderKind.DATASTORE,
                name="stub-datastore",
                display_name="Stub Datastore",
            )

        def warnings(self) -> list[str]:
            return ["warning-1"]

        def health_check(self) -> None:
            return None

        def migrate(self) -> None:
            self.migrated = True

        def get_user(self, id: str) -> StoredUser | None:
            return self.users.get(id)

        def find_or_create_user(self, email: str) -> StoredUser:
            user = self.users.get(email)
            if user is None:
                user = StoredUser(
                    id=email,
                    email=email,
                    created_at=dt.datetime.fromtimestamp(1, tz=UTC),
                    updated_at=dt.datetime.fromtimestamp(2, tz=UTC),
                )
                self.users[email] = user
            return user

        def put_integration_token(self, token: StoredIntegrationToken) -> None:
            self.tokens[token.user_id] = token

        def get_integration_token(
            self,
            user_id: str,
            _integration: str,
            _connection: str,
            _instance: str,
        ) -> StoredIntegrationToken | None:
            return self.tokens.get(user_id)

        def list_integration_tokens(
            self,
            user_id: str,
            _integration: str,
            _connection: str,
        ) -> list[StoredIntegrationToken]:
            token = self.tokens.get(user_id)
            return [] if token is None else [token]

        def delete_integration_token(self, id: str) -> None:
            self.tokens.pop(id, None)

        def put_api_token(self, token: StoredAPIToken) -> None:
            self.api_token = token

        def get_api_token_by_hash(self, hashed_token: str) -> StoredAPIToken | None:
            token = getattr(self, "api_token", None)
            if token is not None and token.hashed_token == hashed_token:
                return token
            return None

        def list_api_tokens(self, user_id: str) -> list[StoredAPIToken]:
            token = getattr(self, "api_token", None)
            if token is None or token.user_id != user_id:
                return []
            return [token]

        def revoke_api_token(self, _user_id: str, _id: str) -> None:
            self.api_token = None

        def revoke_all_api_tokens(self, _user_id: str) -> int:
            self.api_token = None
            return 1

        def get_oauth_registration(
            self,
            auth_server_url: str,
            redirect_uri: str,
        ) -> OAuthRegistration | None:
            if auth_server_url == "https://issuer.example.test":
                return OAuthRegistration(
                    auth_server_url=auth_server_url,
                    redirect_uri=redirect_uri,
                    client_id="client-123",
                    discovered_at=dt.datetime.fromtimestamp(3, tz=UTC),
                )
            return None

        def put_oauth_registration(self, registration: OAuthRegistration) -> None:
            self.registration = registration

        def delete_oauth_registration(self, auth_server_url: str, redirect_uri: str) -> None:
            self.deleted_registration = (auth_server_url, redirect_uri)

    def test_datastore_servicer_round_trip(self) -> None:
        provider = self.StubDatastoreProvider()
        servicer = _runtime._datastore_servicer(provider=provider)

        servicer.Migrate(mock.Mock(), mock.Mock())
        self.assertTrue(provider.migrated)

        created = servicer.FindOrCreateUser(
            datastore_pb2.FindOrCreateUserRequest(email="user@example.com"),
            mock.Mock(),
        )
        self.assertEqual(created.email, "user@example.com")

        token = StoredIntegrationToken(
            id="tok-1",
            user_id="user@example.com",
            integration="github",
            connection="default",
            instance="prod",
            access_token_sealed=b"access",
            refresh_token_sealed=b"refresh",
            created_at=dt.datetime.fromtimestamp(4, tz=UTC),
            updated_at=dt.datetime.fromtimestamp(5, tz=UTC),
        )
        servicer.PutStoredIntegrationToken(
            _runtime._stored_integration_token_to_proto(token),
            mock.Mock(),
        )
        listed = servicer.ListStoredIntegrationTokens(
            datastore_pb2.ListStoredIntegrationTokensRequest(
                user_id="user@example.com",
                integration="github",
                connection="default",
            ),
            mock.Mock(),
        )
        self.assertEqual(len(listed.tokens), 1)
        self.assertEqual(listed.tokens[0].id, "tok-1")

        registration = servicer.GetOAuthRegistration(
            datastore_pb2.GetOAuthRegistrationRequest(
                auth_server_url="https://issuer.example.test",
                redirect_uri="https://cb.example.test",
            ),
            mock.Mock(),
        )
        self.assertEqual(registration.client_id, "client-123")

    def test_datastore_healthcheck_requires_checker(self) -> None:
        class NoHealthDatastore(DatastoreProvider):
            def migrate(self) -> None:
                return None

            def get_user(self, _id: str) -> StoredUser | None:
                return None

            def find_or_create_user(self, email: str) -> StoredUser:
                raise NotImplementedError

            def put_integration_token(self, token: StoredIntegrationToken) -> None:
                raise NotImplementedError

            def get_integration_token(
                self,
                user_id: str,
                integration: str,
                connection: str,
                instance: str,
            ) -> StoredIntegrationToken | None:
                raise NotImplementedError

            def list_integration_tokens(
                self,
                user_id: str,
                integration: str,
                connection: str,
            ) -> list[StoredIntegrationToken]:
                raise NotImplementedError

            def delete_integration_token(self, id: str) -> None:
                raise NotImplementedError

            def put_api_token(self, token: StoredAPIToken) -> None:
                raise NotImplementedError

            def get_api_token_by_hash(self, hashed_token: str) -> StoredAPIToken | None:
                raise NotImplementedError

            def list_api_tokens(self, user_id: str) -> list[StoredAPIToken]:
                raise NotImplementedError

            def revoke_api_token(self, user_id: str, id: str) -> None:
                raise NotImplementedError

            def revoke_all_api_tokens(self, user_id: str) -> int:
                raise NotImplementedError

        servicer = _runtime._runtime_servicer(
            provider=NoHealthDatastore(),
            kind=ProviderKind.DATASTORE,
        )
        health = servicer.HealthCheck(mock.Mock(), mock.Mock())
        self.assertFalse(health.ready)
        self.assertEqual(health.message, "datastore provider must implement HealthChecker")


class GrpcHandlerDecoratorTests(unittest.TestCase):
    """Verify _grpc_handler preserves abort status codes.

    In real gRPC, context.abort() raises an exception after setting the
    status code. The decorator must re-raise abort exceptions rather than
    catching them and overwriting the code with UNKNOWN.
    """

    def _make_aborting_context(self):
        """Create a mock context where abort() raises like real gRPC."""
        ctx = mock.Mock()
        ctx.code.return_value = None

        def fake_abort(code, details):
            ctx.code.return_value = code
            raise Exception(details)

        ctx.abort.side_effect = fake_abort
        return ctx

    def test_abort_preserves_not_found_status(self) -> None:
        servicer = _runtime._datastore_servicer(
            provider=DatastoreRuntimeTests.StubDatastoreProvider(),
        )
        ctx = self._make_aborting_context()
        with self.assertRaises(Exception):
            servicer.GetUser(datastore_pb2.GetUserRequest(id="missing"), ctx)
        ctx.abort.assert_called_once_with(
            grpc.StatusCode.NOT_FOUND, "user not found"
        )

    def test_provider_exception_maps_to_unknown(self) -> None:
        class FailingProvider(DatastoreRuntimeTests.StubDatastoreProvider):
            def get_user(self, _id: str) -> StoredUser | None:
                raise RuntimeError("db connection lost")

        servicer = _runtime._datastore_servicer(provider=FailingProvider())
        ctx = self._make_aborting_context()
        with self.assertRaises(Exception):
            servicer.GetUser(datastore_pb2.GetUserRequest(id="any"), ctx)
        ctx.abort.assert_called_once_with(
            grpc.StatusCode.UNKNOWN, "get user: db connection lost"
        )


if __name__ == "__main__":
    unittest.main()
