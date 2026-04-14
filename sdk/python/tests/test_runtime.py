import datetime as dt
import json
import pathlib
import tempfile
import unittest
from typing import Any
from unittest import mock

import grpc
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from gestalt import (
    AuthProvider,
    CacheEntry,
    CacheProvider,
    Catalog,
    CatalogOperation,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    Plugin,
    ProviderKind,
    ProviderMetadata,
    Request,
    SessionTTLProvider,
    WarningsProvider,
    _bootstrap,
    _runtime,
)
from gestalt.gen.v1 import auth_pb2 as _auth_pb2
from gestalt.gen.v1 import cache_pb2 as _cache_pb2
from gestalt.gen.v1 import plugin_pb2 as _plugin_pb2
from gestalt.gen.v1 import runtime_pb2 as _runtime_pb2

auth_pb2: Any = _auth_pb2
cache_pb2: Any = _cache_pb2
duration_pb2: Any = _duration_pb2
plugin_pb2: Any = _plugin_pb2
runtime_pb2: Any = _runtime_pb2
timestamp_pb2: Any = _timestamp_pb2

UTC = dt.timezone.utc


def _ts(epoch_seconds: int) -> Any:
    ts = timestamp_pb2.Timestamp()
    ts.FromDatetime(dt.datetime.fromtimestamp(epoch_seconds, tz=UTC))
    return ts


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

    def test_normalized_runtime_kind_falls_back_to_integration_for_unknown_values(self) -> None:
        self.assertEqual(
            _runtime._normalized_runtime_kind("typo"),
            ProviderKind.INTEGRATION,
        )
        self.assertEqual(
            _runtime._normalized_runtime_kind(object()),
            ProviderKind.INTEGRATION,
        )


class DurationConversionTests(unittest.TestCase):
    def test_duration_to_timedelta_truncates_submicrosecond_nanos(self) -> None:
        self.assertEqual(
            _runtime._duration_to_timedelta(duration_pb2.Duration(nanos=5_999)),
            dt.timedelta(microseconds=5),
        )


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
        self.assertEqual(request.credential.mode, "")
        self.assertEqual(request.access.role, "")


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

        @plugin.operation
        def whoami(request: Request) -> dict[str, str]:
            return {
                "token": request.token,
                "subject_id": request.subject.id,
                "subject_kind": request.subject.kind,
                "credential_mode": request.credential.mode,
                "credential_subject_id": request.credential.subject_id,
                "access_policy": request.access.policy,
                "access_role": request.access.role,
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
                    ]
                ),
            )
            cat.operations.append(CatalogOperation(id="private_search", method="POST"))
            cat.operations[0].allowed_roles.extend(["viewer", "admin"])
            return cat

        servicer = _runtime._provider_servicer(plugin=plugin)
        metadata = servicer.GetMetadata(mock.Mock(), mock.Mock())
        execute_response = servicer.Execute(
            plugin_pb2.ExecuteRequest(
                operation="whoami",
                token="secret-token",
                context=plugin_pb2.RequestContext(
                    subject=plugin_pb2.SubjectContext(
                        id="user:user-123",
                        kind="user",
                        auth_source="api_token",
                    ),
                    credential=plugin_pb2.CredentialContext(
                        mode="identity",
                        subject_id="identity:__identity__",
                    ),
                    access=plugin_pb2.AccessContext(
                        policy="sample_policy",
                        role="admin",
                    ),
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
                    credential=plugin_pb2.CredentialContext(mode="identity"),
                    access=plugin_pb2.AccessContext(
                        policy="sample_policy",
                        role="viewer",
                    ),
                ),
            ),
            mock.Mock(),
        )

        self.assertTrue(metadata.supports_session_catalog)
        self.assertEqual(
            json.loads(execute_response.body),
            {
                "token": "secret-token",
                "subject_id": "user:user-123",
                "subject_kind": "user",
                "credential_mode": "identity",
                "credential_subject_id": "identity:__identity__",
                "access_policy": "sample_policy",
                "access_role": "admin",
            },
        )
        catalog = response.catalog
        self.assertEqual(catalog.name, "session-source")
        self.assertEqual(catalog.display_name, "acme|user:user-123|identity|viewer")
        self.assertEqual(len(catalog.operations), 1)
        self.assertEqual(catalog.operations[0].id, "private_search")
        self.assertEqual(catalog.operations[0].method, "POST")
        self.assertEqual(list(catalog.operations[0].allowed_roles), ["viewer", "admin"])

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

        def configure(self, name: str, config: dict[str, Any]) -> None:
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

        def begin_login(self, request: Any) -> Any:
            return auth_pb2.BeginLoginResponse(
                authorization_url=f"https://auth.example.test/login?state={request.host_state}",
                provider_state=b"provider-state",
            )

        def complete_login(self, request: Any) -> Any:
            return auth_pb2.AuthenticatedUser(
                email=request.query.get("email", ""),
                display_name="Runtime User",
            )

        def validate_external_token(self, token: str) -> Any:
            if token == "known-token":
                return auth_pb2.AuthenticatedUser(email="token@example.com")
            return None

        def session_ttl(self) -> dt.timedelta:
            return dt.timedelta(minutes=45)

    def test_runtime_metadata_and_auth_servicer(self) -> None:
        provider = self.StubAuthProvider()

        runtime_servicer = _runtime._runtime_servicer(
            provider=provider,
            kind=ProviderKind.AUTH,
        )
        meta = runtime_servicer.GetProviderIdentity(mock.Mock(), mock.Mock())
        self.assertEqual(meta.kind, runtime_pb2.ProviderKind.PROVIDER_KIND_AUTH)
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
        self.assertEqual(
            login.authorization_url, "https://auth.example.test/login?state=host-state"
        )
        self.assertEqual(bytes(login.provider_state), b"provider-state")

        user = auth_servicer.CompleteLogin(
            auth_pb2.CompleteLoginRequest(
                query={"email": "user@example.com"},
                provider_state=b"provider-state",
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
            def begin_login(self, request: Any) -> Any:
                return auth_pb2.BeginLoginResponse(
                    authorization_url="https://example.test"
                )

            def complete_login(self, request: Any) -> Any:
                return auth_pb2.AuthenticatedUser(email="user@example.com")

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


if __name__ == "__main__":
    unittest.main()
