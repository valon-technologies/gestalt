"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { getAuthInfo, startLogin } from "@/lib/api";
import { isAuthenticated, setSessionToken, setUserEmail } from "@/lib/auth";
import Button from "@/components/Button";

export default function LoginPage() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [devEmail, setDevEmail] = useState("dev@toolshed.local");
  const [providerLabel, setProviderLabel] = useState("Sign in");

  useEffect(() => {
    if (isAuthenticated()) {
      router.replace("/");
    }
  }, [router]);

  useEffect(() => {
    getAuthInfo()
      .then((info) => setProviderLabel("Sign in with " + info.display_name))
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

  async function handleDevLogin() {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/dev-login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: devEmail }),
      });
      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || "Dev login failed");
      }
      const { email, token } = await res.json();
      setSessionToken(token);
      setUserEmail(email);
      router.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Dev login failed");
    } finally {
      setLoading(false);
    }
  }

  const isDev = process.env.NODE_ENV === "development";

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-8 shadow-warm">
        <h1 className="text-center text-2xl font-heading font-bold text-timber-800">
          Toolshed
        </h1>
        <p className="mt-2 text-center text-sm text-stone-500">
          Sign in to manage your integrations.
        </p>
        <p className="mt-2 text-center text-sm text-stone-500">
          Or read the{" "}
          <a href="/docs" className="font-medium text-timber-600 hover:text-timber-700">
            documentation
          </a>
          .
        </p>
        {error && (
          <p className="mt-4 text-center text-sm text-ember-500">{error}</p>
        )}
        <div className="mt-6">
          <Button onClick={handleLogin} disabled={loading} className="w-full">
            {loading ? "Redirecting..." : providerLabel}
          </Button>
        </div>

        {isDev && (
          <>
            <div className="mt-6 flex items-center gap-2">
              <div className="h-px flex-1 bg-stone-200" />
              <span className="text-xs text-stone-400">dev mode</span>
              <div className="h-px flex-1 bg-stone-200" />
            </div>
            <div className="mt-4">
              <input
                type="email"
                value={devEmail}
                onChange={(e) => setDevEmail(e.target.value)}
                placeholder="dev@toolshed.local"
                className="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25"
              />
              <Button
                onClick={handleDevLogin}
                disabled={loading}
                variant="secondary"
                className="mt-2 w-full"
              >
                Dev Login
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
