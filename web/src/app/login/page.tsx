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
    <div className="page-shell gradient-warm flex min-h-screen items-center justify-center px-4 py-12">
      <div className="w-full max-w-md animate-fade-in-up">
        <div className="surface-card p-8 md:p-10">
          <div className="text-center">
            <span className="label-text">Client Portal</span>
            <h1 className="page-title mt-4 text-[clamp(2.5rem,8vw,3.5rem)]">
              Gestalt
            </h1>
            <p className="mt-4 text-base leading-7 text-secondary">
              Sign in to manage integrations, credentials, and the operational
              surface around them.
            </p>
            <p className="mt-3 text-sm text-muted">
              Or read the{" "}
              <a
                href="/docs"
                className="font-bold text-primary underline underline-offset-4 decoration-base-300"
              >
                documentation
              </a>
              .
            </p>
          </div>
          {error && (
            <p className="mt-5 text-center text-sm text-ember-500">{error}</p>
          )}
          <div className="mt-8">
            <Button onClick={handleLogin} disabled={loading} className="w-full">
              {loading ? "Redirecting..." : authLabel}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
