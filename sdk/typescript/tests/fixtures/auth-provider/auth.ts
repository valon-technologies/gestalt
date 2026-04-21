import { defineAuthenticationProvider } from "../../../src/index.ts";

let configuredIssuer = "https://issuer.example.test";

export const provider = defineAuthenticationProvider({
  displayName: "Fixture Auth",
  description: "Auth fixture used by SDK tests",
  configure(_name, config) {
    configuredIssuer = String(config.issuer ?? configuredIssuer);
  },
  beginAuthentication(request) {
    return {
      authorizationUrl: `${configuredIssuer}/authorize?state=upstream-state&host=${encodeURIComponent(request.hostState)}`,
      providerState: new Uint8Array([1, 2, 3]),
    };
  },
  completeAuthentication(request) {
    return {
      subject: request.query.code || "subject-1",
      email: "fixture@example.com",
      emailVerified: true,
      displayName: "Fixture Auth User",
      avatarUrl: `${configuredIssuer}/avatar.png`,
      claims: {
        issuer: configuredIssuer,
        callback: request.callbackUrl,
      },
    };
  },
  authenticate(request) {
    const token = request.token?.token ?? "";
    if (!token) {
      return null;
    }
    return {
      subject: token,
      email: `${token}@example.com`,
      displayName: "Validated User",
    };
  },
  sessionSettings() {
    return {
      sessionTtlSeconds: 5400,
    };
  },
});
