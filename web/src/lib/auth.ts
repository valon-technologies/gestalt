export function getSessionToken(): string | null {
  return localStorage.getItem("session_token");
}

export function setSessionToken(token: string): void {
  localStorage.setItem("session_token", token);
}

export function clearSession(): void {
  localStorage.removeItem("session_token");
  localStorage.removeItem("user_email");
}

export function isAuthenticated(): boolean {
  return getSessionToken() !== null;
}

export function getUserEmail(): string | null {
  return localStorage.getItem("user_email");
}

export function setUserEmail(email: string): void {
  localStorage.setItem("user_email", email);
}
