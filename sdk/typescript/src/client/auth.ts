/**
 * Authentication providers for the public Gestalt transport client.
 */

export type AuthKind = "session" | "bearer" | "none";

export interface AuthProvider {
  readonly kind: AuthKind;
  /** Returns an Authorization header value, or undefined when cookies carry auth. */
  getAuthorization(): Promise<string | undefined> | string | undefined;
}

/** Bearer token authentication. */
export function bearer(
  token: string | (() => string | Promise<string>),
): AuthProvider {
  if (typeof token === "function") {
    return {
      kind: "bearer",
      async getAuthorization() {
        const value = (await token()).trim();
        return value ? `Bearer ${value}` : undefined;
      },
    };
  }
  const trimmed = token.trim();
  return {
    kind: "bearer",
    getAuthorization() {
      return trimmed ? `Bearer ${trimmed}` : undefined;
    },
  };
}

/** @deprecated Use {@link bearer}. */
export function bearerAuth(token: string): AuthProvider {
  return bearer(token);
}

/**
 * Session authentication for browser clients. Cookies are sent with
 * `credentials: "include"`; no Authorization header is attached.
 */
export function session(): AuthProvider {
  return {
    kind: "session",
    getAuthorization() {
      return undefined;
    },
  };
}

/** @deprecated Use {@link session}. */
export function sessionAuth(): AuthProvider {
  return session();
}

/** Unauthenticated requests: no Authorization header and no cookies. */
export function unauthenticated(): AuthProvider {
  return {
    kind: "none",
    getAuthorization() {
      return undefined;
    },
  };
}

export function restCredentials(auth: AuthProvider): RequestCredentials {
  return auth.kind === "session" ? "include" : "omit";
}
