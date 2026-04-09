import { clearSession } from "./auth";
import { HTTP_UNAUTHORIZED, LOGIN_PATH } from "./constants";

export interface ConnectionParamDef {
  required?: boolean;
  description?: string;
  default?: string;
}

export interface CredentialFieldDef {
  name: string;
  label?: string;
  description?: string;
}

export interface InstanceInfo {
  name: string;
  connection?: string;
}

export interface ConnectionDefInfo {
  name: string;
  authTypes: ("oauth" | "manual")[];
  credentialFields?: CredentialFieldDef[];
}

export interface Integration {
  name: string;
  displayName?: string;
  description?: string;
  iconSvg?: string;
  connected?: boolean;
  instances?: InstanceInfo[];
  authTypes?: ("oauth" | "manual")[];
  connectionParams?: Record<string, ConnectionParamDef>;
  connections?: ConnectionDefInfo[];
  credentialFields?: CredentialFieldDef[];
}

export interface APIToken {
  id: string;
  name: string;
  scopes: string;
  createdAt: string;
  expiresAt?: string;
}

export interface CreateTokenResponse {
  id: string;
  name: string;
  token: string;
}

export interface ConnectIntegrationResult {
  status: string;
  integration?: string;
  selectionUrl?: string;
  pendingToken?: string;
  candidates?: { id: string; name?: string }[];
}

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "APIError";
  }
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "";
export const PENDING_CONNECTION_PATH = "/api/v1/auth/pending-connection";

export function resolveAPIPath(path: string): string {
  if (/^[a-zA-Z][a-zA-Z\d+.-]*:/.test(path)) {
    return path;
  }
  return `${API_BASE}${path}`;
}

export async function fetchAPI<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  const res = await fetch(resolveAPIPath(path), {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

  if (res.status === HTTP_UNAUTHORIZED) {
    clearSession();
    if (window.location.pathname !== LOGIN_PATH) {
      window.location.href = LOGIN_PATH;
    }
    throw new APIError(HTTP_UNAUTHORIZED, "Session expired");
  }

  if (!res.ok) {
    const body = await res.text();
    let message: string;
    try {
      const parsed = JSON.parse(body);
      message = parsed.error || body;
    } catch {
      message = body;
    }
    throw new APIError(res.status, message);
  }

  return res.json() as Promise<T>;
}

export interface AuthInfo {
  provider: string;
  displayName: string;
  loginSupported: boolean;
}

export async function getAuthInfo(): Promise<AuthInfo> {
  return fetchAPI("/api/v1/auth/info");
}

export async function startLogin(state: string): Promise<{ url: string }> {
  return fetchAPI("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ state }),
  });
}

export async function loginCallback(
  code: string,
  state?: string,
): Promise<{ email: string; displayName: string }> {
  const params = new URLSearchParams({ code });
  if (state) params.set("state", state);
  return fetchAPI(`/api/v1/auth/login/callback?${params}`);
}

export async function logout(): Promise<void> {
  await fetchAPI("/api/v1/auth/logout", { method: "POST" });
}

export async function getIntegrations(): Promise<Integration[]> {
  return fetchAPI("/api/v1/integrations");
}

export async function startIntegrationOAuth(
  integration: string,
  scopes?: string[],
  connectionParams?: Record<string, string>,
  instance?: string,
  connection?: string,
): Promise<{ url: string; state: string }> {
  return fetchAPI("/api/v1/auth/start-oauth", {
    method: "POST",
    body: JSON.stringify({
      integration,
      instance,
      connection,
      scopes: scopes || [],
      connectionParams,
    }),
  });
}

export async function connectManualIntegration(
  integration: string,
  credential: string | Record<string, string>,
  connectionParams?: Record<string, string>,
  instance?: string,
  connection?: string,
): Promise<ConnectIntegrationResult> {
  const body: Record<string, unknown> = {
    integration,
    instance,
    connection,
    connectionParams,
  };
  if (typeof credential === "string") {
    body.credential = credential;
  } else {
    body.credentials = credential;
  }
  return fetchAPI("/api/v1/auth/connect-manual", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function disconnectIntegration(
  name: string,
  instance?: string,
  connection?: string,
): Promise<void> {
  const query = new URLSearchParams();
  if (instance) query.set("instance", instance);
  if (connection) query.set("connection", connection);
  const params = query.toString();
  await fetchAPI(
    `/api/v1/integrations/${encodeURIComponent(name)}${params ? `?${params}` : ""}`,
    {
      method: "DELETE",
    },
  );
}

export async function getTokens(): Promise<APIToken[]> {
  return fetchAPI("/api/v1/tokens");
}

export async function createToken(name: string): Promise<CreateTokenResponse> {
  return fetchAPI("/api/v1/tokens", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function revokeToken(id: string): Promise<void> {
  await fetchAPI(`/api/v1/tokens/${id}`, { method: "DELETE" });
}
