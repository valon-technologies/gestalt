import { defineIdentityProvider } from "../../../src/index.ts";

let configuredIssuer = "https://issuer.example.test";
const fixtureCode = "fixture-auth-code";
const fixtureAccessToken = "fixture-access-token";

export const provider = defineIdentityProvider({
  displayName: "Fixture Auth",
  description: "Auth fixture used by SDK tests",
  configure(_name, config) {
    configuredIssuer = String(config.issuer ?? configuredIssuer);
  },
  authorize(request) {
    const redirect = new URL(request.redirectUri);
    redirect.searchParams.set("code", fixtureCode);
    redirect.searchParams.set("state", request.state);
    return {
      redirectUri: redirect.toString(),
    };
  },
  token(request) {
    if (request.code !== fixtureCode) {
      throw new Error("invalid authorization code");
    }
    return {
      accessToken: fixtureAccessToken,
      tokenType: "Bearer",
      expiresIn: 5400,
      scope: request.grantType === "authorization_code" ? "openid email" : "",
      grantId: "grant-fixture-1",
    };
  },
  introspect(request) {
    if (!request.token || request.token !== fixtureAccessToken) {
      return { active: false };
    }
    return {
      active: true,
      subject: "user:fixture@example.com",
      scope: "openid email",
      clientId: "gestaltd",
      audience: [configuredIssuer],
    };
  },
  userInfo(_request, call) {
    if (!call.callerBearerToken || call.callerBearerToken !== fixtureAccessToken) {
      throw new Error("userinfo not found");
    }
    return {
      subjectId: "user:fixture@example.com",
      email: "fixture@example.com",
      name: "Fixture User",
    };
  },
  listGrants(_request, call) {
    if (!call.callerBearerToken) {
      throw new Error("caller bearer token required");
    }
    return { grantIds: ["grant-fixture-1"] };
  },
  getGrant(request, call) {
    if (!call.callerBearerToken) {
      throw new Error("caller bearer token required");
    }
    if (request.grantId !== "grant-fixture-1") {
      throw new Error("grant not found");
    }
    return {
      scopes: [{ scope: "openid", resource: [] }],
      createdAt: 1_700_000_000,
      expiresAt: 1_800_000_000,
    };
  },
  revokeGrant(request, call) {
    if (!call.callerBearerToken) {
      throw new Error("caller bearer token required");
    }
    if (request.grantId !== "grant-fixture-1") {
      throw new Error("grant not found");
    }
  },
});
