export function clearSession(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem("user_email");
}

export function isAuthenticated(): boolean {
  return getUserEmail() !== null;
}

export function getUserEmail(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("user_email");
}

export function setUserEmail(email: string): void {
  if (typeof window === "undefined") return;
  localStorage.setItem("user_email", email);
}
