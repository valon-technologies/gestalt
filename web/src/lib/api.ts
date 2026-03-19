import { clearSession, getSessionToken } from "./auth";

export interface Integration {
  name: string;
  display_name?: string;
  description?: string;
  connected?: boolean;
}

export interface APIToken {
  id: string;
  name: string;
  scopes: string;
  created_at: string;
  expires_at?: string;
}

export interface CreateTokenResponse {
  id: string;
  name: string;
  token: string;
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

function getAuthHeaders(): Record<string, string> {
  const token = getSessionToken();
  if (!token) return {};
  return { Authorization: `Bearer ${token}` };
}

export async function fetchAPI<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  const res = await fetch(path, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...getAuthHeaders(),
      ...options?.headers,
    },
  });

  if (res.status === 401) {
    clearSession();
    window.location.href = "/login";
    throw new APIError(401, "Session expired");
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
  display_name: string;
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
): Promise<{ email: string; display_name: string; token?: string }> {
  const params = new URLSearchParams({ code });
  if (state) params.set("state", state);
  return fetchAPI(`/api/v1/auth/login/callback?${params}`);
}

export async function getIntegrations(): Promise<Integration[]> {
  return fetchAPI("/api/v1/integrations");
}

export async function startIntegrationOAuth(
  integration: string,
  scopes?: string[],
): Promise<{ url: string; state: string }> {
  return fetchAPI("/api/v1/auth/start-oauth", {
    method: "POST",
    body: JSON.stringify({ integration, scopes: scopes || [] }),
  });
}

export async function getTokens(): Promise<APIToken[]> {
  return fetchAPI("/api/v1/tokens");
}

export async function createToken(
  name: string,
): Promise<CreateTokenResponse> {
  return fetchAPI("/api/v1/tokens", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function revokeToken(id: string): Promise<void> {
  await fetchAPI(`/api/v1/tokens/${id}`, { method: "DELETE" });
}
