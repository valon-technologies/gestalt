"use client";

import { useState, useEffect } from "react";
import { fetchAPI, getAuthInfo, startLogin } from "@/lib/api";
import { isAuthenticated, setSessionToken, setUserEmail } from "@/lib/auth";
import Button from "@/components/Button";

export default function LoginPage() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authConfig, setAuthConfig] = useState({ label: "Sign in", devMode: false });

  useEffect(() => {
    if (isAuthenticated()) {
      window.location.replace("/");
    }
  }, []);

  useEffect(() => {
    getAuthInfo()
      .then((info) => {
        setAuthConfig({
          label: "Sign in with " + info.display_name,
          devMode: info.dev_mode,
        });
      })
      .catch(() => {});
  }, []);

  async function handleLogin() {
    setLoading(true);
    setError(null);
    try {
      const state = crypto.randomUUID();
      sessionStorage.setItem("oauth_state", state);
      const { url } = await startLogin(state);
      window.location.href = url;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  async function handleDevLogin(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const devEmail = (new FormData(e.currentTarget).get("email") as string) || "dev@gestalt.local";
    setLoading(true);
    setError(null);
    try {
      const { email, token } = await fetchAPI<{
        email: string;
        token: string;
      }>("/api/dev-login", {
        method: "POST",
        body: JSON.stringify({ email: devEmail }),
      });
      setSessionToken(token);
      setUserEmail(email);
      window.location.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Dev login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-8 shadow-warm">
        <h1 className="text-center text-2xl font-heading font-bold text-timber-800">
          Gestalt
        </h1>
        <p className="mt-2 text-center text-sm text-stone-500">
          Sign in to manage your integrations.
        </p>
        <p className="mt-2 text-center text-sm text-stone-500">
          Or read the{" "}
          <a
            href="/docs"
            className="font-medium text-timber-600 hover:text-timber-700"
          >
            documentation
          </a>
          .
        </p>
        {error && (
          <p className="mt-4 text-center text-sm text-ember-500">{error}</p>
        )}
        <div className="mt-6">
          <Button onClick={handleLogin} disabled={loading} className="w-full">
            {loading ? "Redirecting..." : authConfig.label}
          </Button>
        </div>

        {authConfig.devMode && (
          <>
            <div className="mt-6 flex items-center gap-2">
              <div className="h-px flex-1 bg-stone-200" />
              <span className="text-xs text-stone-400">dev mode</span>
              <div className="h-px flex-1 bg-stone-200" />
            </div>
            <form onSubmit={handleDevLogin} className="mt-4">
              <input
                name="email"
                type="email"
                defaultValue="dev@gestalt.local"
                placeholder="dev@gestalt.local"
                className="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25"
              />
              <Button
                type="submit"
                disabled={loading}
                variant="secondary"
                className="mt-2 w-full"
              >
                Dev Login
              </Button>
            </form>
          </>
        )}
      </div>
    </div>
  );
}
