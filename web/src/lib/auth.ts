export function clearSession(): void {
  localStorage.removeItem("user_email");
}

export function isAuthenticated(): boolean {
  return getUserEmail() !== null;
}

export function getUserEmail(): string | null {
  return localStorage.getItem("user_email");
}

export function setUserEmail(email: string): void {
  localStorage.setItem("user_email", email);
}
