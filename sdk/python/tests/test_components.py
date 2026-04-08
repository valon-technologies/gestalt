import datetime as dt
import unittest
from unittest import mock

from gestalt import (
    AuthenticatedUser,
    AuthProvider,
    BeginLoginRequest,
    BeginLoginResponse,
    DatastoreProvider,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    OAuthRegistration,
    OAuthRegistrationStore,
    ProviderKind,
    ProviderMetadata,
    SessionTTLProvider,
    StoredAPIToken,
    StoredIntegrationToken,
    StoredUser,
    WarningsProvider,
    _runtime,
)

UTC = dt.timezone.utc


class StubAuthProvider(
    AuthProvider,
    ExternalTokenValidator,
    MetadataProvider,
    SessionTTLProvider,
    WarningsProvider,
    HealthChecker,
):
    def metadata(self) -> ProviderMetadata:
        return ProviderMetadata(
            kind=ProviderKind.AUTH,
            name="stub-auth",
            display_name="Stub Auth",
            description="stub auth provider",
            version="1.0.0",
        )

    def warnings(self) -> list[str]:
        return ["auth warning"]

    def health_check(self) -> None:
        return None

    def begin_login(self, request: BeginLoginRequest) -> BeginLoginResponse:
        return BeginLoginResponse(
            authorization_url=f"https://auth.example.test?state={request.host_state}",
            provider_state=b"provider-state",
        )

    def complete_login(self, request):  # type: ignore[override]
        return AuthenticatedUser(
            subject="user-123",
            email=request.query.get("email", "user@example.com"),
            display_name="Stub User",
        )

    def validate_external_token(self, token: str) -> AuthenticatedUser | None:
        if token == "missing":
            return None
        return AuthenticatedUser(email=f"{token}@example.com")

    def session_ttl(self) -> dt.timedelta:
        return dt.timedelta(minutes=90)


class StubDatastoreProvider(
    DatastoreProvider,
    MetadataProvider,
    HealthChecker,
    WarningsProvider,
    OAuthRegistrationStore,
):
    def __init__(self) -> None:
        self.users: dict[str, StoredUser] = {}
        self.tokens: dict[str, StoredIntegrationToken] = {}
        self.registrations: dict[tuple[str, str], OAuthRegistration] = {}

    def metadata(self) -> ProviderMetadata:
        return ProviderMetadata(
            kind=ProviderKind.DATASTORE,
            name="stub-datastore",
            display_name="Stub Datastore",
        )

    def warnings(self) -> list[str]:
        return ["datastore warning"]

    def health_check(self) -> None:
        return None

    def migrate(self) -> None:
        return None

    def get_user(self, id: str) -> StoredUser | None:
        return self.users.get(id)

    def find_or_create_user(self, email: str) -> StoredUser:
        user = self.users.get(email)
        if user is None:
            now = dt.datetime.now(tz=UTC).replace(microsecond=0)
            user = StoredUser(id=email, email=email, created_at=now, updated_at=now)
            self.users[email] = user
        return user

    def put_integration_token(self, token: StoredIntegrationToken) -> None:
        self.tokens[token.user_id] = token

    def get_integration_token(
        self,
        user_id: str,
        integration: str,
        connection: str,
        instance: str,
    ) -> StoredIntegrationToken | None:
        del integration, connection, instance
        return self.tokens.get(user_id)

    def list_integration_tokens(
        self,
        user_id: str,
        integration: str,
        connection: str,
    ) -> list[StoredIntegrationToken]:
        del integration, connection
        token = self.tokens.get(user_id)
        return [] if token is None else [token]

    def delete_integration_token(self, id: str) -> None:
        self.tokens.pop(id, None)

    def put_api_token(self, token: StoredAPIToken) -> None:
        del token

    def get_api_token_by_hash(self, hashed_token: str) -> StoredAPIToken | None:
        return None if hashed_token == "missing" else StoredAPIToken(hashed_token=hashed_token)

    def list_api_tokens(self, user_id: str) -> list[StoredAPIToken]:
        del user_id
        return [StoredAPIToken(id="api-1", hashed_token="hash")]

    def revoke_api_token(self, user_id: str, id: str) -> None:
        del user_id, id

    def revoke_all_api_tokens(self, user_id: str) -> int:
        del user_id
        return 3

    def get_oauth_registration(
        self,
        auth_server_url: str,
        redirect_uri: str,
    ) -> OAuthRegistration | None:
        return self.registrations.get((auth_server_url, redirect_uri))

    def put_oauth_registration(self, registration: OAuthRegistration) -> None:
        self.registrations[(registration.auth_server_url, registration.redirect_uri)] = registration

    def delete_oauth_registration(self, auth_server_url: str, redirect_uri: str) -> None:
        self.registrations.pop((auth_server_url, redirect_uri), None)


class RuntimeProviderServicerTests(unittest.TestCase):
    def test_runtime_provider_servicer_reports_metadata_and_health(self) -> None:
        runtime = _runtime._runtime_imports()
        provider = StubAuthProvider()
        entrypoint = _runtime._auth_runtime_plugin(provider)

        servicer = _runtime._runtime_servicer(
            provider=entrypoint.provider,
            kind=ProviderKind.AUTH,
            runtime=runtime,
        )
        metadata = servicer.GetPluginMetadata(mock.Mock(), mock.Mock())
        health = servicer.HealthCheck(mock.Mock(), mock.Mock())

        self.assertEqual(metadata.kind, runtime.runtime_pb2.PLUGIN_KIND_AUTH)
        self.assertEqual(metadata.name, "stub-auth")
        self.assertEqual(metadata.display_name, "Stub Auth")
        self.assertEqual(metadata.warnings, ["auth warning"])
        self.assertTrue(health.ready)


class AuthProviderServicerTests(unittest.TestCase):
    def test_auth_provider_servicer_round_trips_requests(self) -> None:
        runtime = _runtime._runtime_imports()
        servicer = _runtime._auth_servicer(
            provider=StubAuthProvider(),
            runtime=runtime,
        )

        begin = servicer.BeginLogin(
            runtime.auth_pb2.BeginLoginRequest(host_state="state-123"),
            mock.Mock(),
        )
        user = servicer.CompleteLogin(
            runtime.auth_pb2.CompleteLoginRequest(query={"email": "ada@example.com"}),
            mock.Mock(),
        )
        validated = servicer.ValidateExternalToken(
            runtime.auth_pb2.ValidateExternalTokenRequest(token="token"),
            mock.Mock(),
        )
        settings = servicer.GetSessionSettings(mock.Mock(), mock.Mock())

        self.assertEqual(begin.authorization_url, "https://auth.example.test?state=state-123")
        self.assertEqual(begin.plugin_state, b"provider-state")
        self.assertEqual(user.email, "ada@example.com")
        self.assertEqual(validated.email, "token@example.com")
        self.assertEqual(settings.session_ttl_seconds, 5400)


class DatastoreProviderServicerTests(unittest.TestCase):
    def test_datastore_provider_servicer_round_trips_storage_calls(self) -> None:
        runtime = _runtime._runtime_imports()
        provider = StubDatastoreProvider()
        servicer = _runtime._datastore_servicer(
            provider=provider,
            runtime=runtime,
        )

        user = servicer.FindOrCreateUser(
            runtime.datastore_pb2.FindOrCreateUserRequest(email="ada@example.com"),
            mock.Mock(),
        )
        token = StoredIntegrationToken(
            id="tok-1",
            user_id=user.id,
            integration="github",
            connection="default",
            instance="prod",
            access_token_sealed=b"access",
            refresh_token_sealed=b"refresh",
        )
        servicer.PutStoredIntegrationToken(
            _runtime._stored_integration_token_to_proto(runtime, token),
            mock.Mock(),
        )
        fetched = servicer.GetStoredIntegrationToken(
            runtime.datastore_pb2.GetStoredIntegrationTokenRequest(
                user_id=user.id,
                integration="github",
                connection="default",
                instance="prod",
            ),
            mock.Mock(),
        )
        listed = servicer.ListStoredIntegrationTokens(
            runtime.datastore_pb2.ListStoredIntegrationTokensRequest(
                user_id=user.id,
            ),
            mock.Mock(),
        )

        registration = OAuthRegistration(
            auth_server_url="https://auth.example.test",
            redirect_uri="https://app.example.test/callback",
            client_id="client-123",
        )
        servicer.PutOAuthRegistration(
            _runtime._oauth_registration_to_proto(runtime, registration),
            mock.Mock(),
        )
        fetched_registration = servicer.GetOAuthRegistration(
            runtime.datastore_pb2.GetOAuthRegistrationRequest(
                auth_server_url="https://auth.example.test",
                redirect_uri="https://app.example.test/callback",
            ),
            mock.Mock(),
        )
        revoked = servicer.RevokeAllAPITokens(
            runtime.datastore_pb2.RevokeAllAPITokensRequest(user_id=user.id),
            mock.Mock(),
        )

        self.assertEqual(user.email, "ada@example.com")
        self.assertEqual(fetched.user_id, "ada@example.com")
        self.assertEqual(len(listed.tokens), 1)
        self.assertEqual(fetched_registration.client_id, "client-123")
        self.assertEqual(revoked.revoked, 3)


if __name__ == "__main__":
    unittest.main()
