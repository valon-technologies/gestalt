"use client";

import { useState, useEffect } from "react";
import { getAuthInfo, startLogin } from "@/lib/api";
import { isAuthenticated, setUserEmail } from "@/lib/auth";
import { NONE_PROVIDER, DEFAULT_LOCAL_EMAIL } from "@/lib/constants";
import Button from "@/components/Button";

export default function LoginPage() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authLabel, setAuthLabel] = useState("Sign in");

  useEffect(() => {
    if (isAuthenticated()) {
      window.location.replace("/");
      return;
    }
    getAuthInfo()
      .then((info) => {
        if (info.provider === NONE_PROVIDER) {
          setUserEmail(DEFAULT_LOCAL_EMAIL);
          window.location.replace("/");
          return;
        }
        setAuthLabel("Sign in with " + info.display_name);
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

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-8 shadow-warm">
        <h1 className="text-center text-2xl font-heading font-bold text-timber-800 dark:text-timber-200">
          Gestalt
        </h1>
        <p className="mt-2 text-center text-sm text-stone-500 dark:text-stone-400">
          Sign in to manage your integrations.
        </p>
        <p className="mt-2 text-center text-sm text-stone-500 dark:text-stone-400">
          Or read the{" "}
          <a
            href="/docs"
            className="font-medium text-timber-600 hover:text-timber-700 dark:text-timber-400 dark:hover:text-timber-300"
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
            {loading ? "Redirecting..." : authLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
