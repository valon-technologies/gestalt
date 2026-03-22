// TODO: migrate to httpOnly cookies set by the server.

export function getSessionToken(): string | null {
  return sessionStorage.getItem("session_token");
}

export function setSessionToken(token: string): void {
  sessionStorage.setItem("session_token", token);
}

export function clearSession(): void {
  sessionStorage.removeItem("session_token");
  sessionStorage.removeItem("user_email");
}

export function isAuthenticated(): boolean {
  return getSessionToken() !== null;
}

export function getUserEmail(): string | null {
  return sessionStorage.getItem("user_email");
}

export function setUserEmail(email: string): void {
  sessionStorage.setItem("user_email", email);
}
