import {
  defineDatastoreProvider,
  type OAuthRegistration,
  type StoredAPIToken,
  type StoredIntegrationToken,
  type StoredUser,
} from "../../../src/index.ts";

const users = new Map<string, StoredUser>();
const integrationTokens = new Map<string, StoredIntegrationToken>();
const apiTokens = new Map<string, StoredAPIToken>();
const registrations = new Map<string, OAuthRegistration>();

function registrationKey(authServerUrl: string, redirectUri: string): string {
  return `${authServerUrl}::${redirectUri}`;
}

export const provider = defineDatastoreProvider({
  displayName: "Fixture Datastore",
  description: "Datastore fixture used by SDK tests",
  warnings: ["fixture datastore warning"],
  healthCheck() {},
  migrate() {},
  getUser(id) {
    return users.get(id) ?? null;
  },
  findOrCreateUser(email) {
    const existing = users.get(email);
    if (existing) {
      return existing;
    }
    const now = new Date("2026-04-08T12:00:00.000Z");
    const created: StoredUser = {
      id: email,
      email,
      displayName: email.toUpperCase(),
      createdAt: now,
      updatedAt: now,
    };
    users.set(email, created);
    return created;
  },
  putIntegrationToken(token) {
    integrationTokens.set(token.id, token);
  },
  getIntegrationToken(_userId, _integration, _connection, instance) {
    return integrationTokens.get(instance) ?? null;
  },
  listIntegrationTokens() {
    return [...integrationTokens.values()];
  },
  deleteIntegrationToken(id) {
    integrationTokens.delete(id);
  },
  putApiToken(token) {
    apiTokens.set(token.hashedToken, token);
  },
  getApiTokenByHash(hashedToken) {
    return apiTokens.get(hashedToken) ?? null;
  },
  listApiTokens() {
    return [...apiTokens.values()];
  },
  revokeApiToken(_userId, id) {
    for (const [key, token] of apiTokens.entries()) {
      if (token.id === id) {
        apiTokens.delete(key);
      }
    }
  },
  revokeAllApiTokens() {
    const count = apiTokens.size;
    apiTokens.clear();
    return count;
  },
  getOAuthRegistration(authServerUrl, redirectUri) {
    return registrations.get(registrationKey(authServerUrl, redirectUri)) ?? null;
  },
  putOAuthRegistration(registration) {
    registrations.set(registrationKey(registration.authServerUrl, registration.redirectUri), registration);
  },
  deleteOAuthRegistration(authServerUrl, redirectUri) {
    registrations.delete(registrationKey(authServerUrl, redirectUri));
  },
});
