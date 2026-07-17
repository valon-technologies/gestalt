/**
 * Authentication types and adapters for the server-side Gestalt client.
 */

export type AuthProvider = {
  /** Returns an Authorization header value, or undefined when unauthenticated. */
  getAuthorization(): Promise<string | undefined>;
};

export interface BearerAuth {
  readonly kind: "bearer";
  readonly token: () => string | Promise<string>;
}

export interface Unauthenticated {
  readonly kind: "unauthenticated";
}

export type Auth = BearerAuth | Unauthenticated;

export function bearer(token: () => string | Promise<string>): BearerAuth {
  return { kind: "bearer", token };
}

export function unauthenticated(): Unauthenticated {
  return { kind: "unauthenticated" };
}

export function authToProvider(auth: Auth): AuthProvider {
  if (auth.kind === "bearer") {
    return {
      async getAuthorization() {
        const value = (await auth.token()).trim();
        return value ? `Bearer ${value}` : undefined;
      },
    };
  }
  return {
    async getAuthorization() {
      return undefined;
    },
  };
}
