"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { loginCallback } from "@/lib/api";
import { setSessionToken, setUserEmail } from "@/lib/auth";

function CallbackHandler() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const code = searchParams.get("code");
    const returnedState = searchParams.get("state");
    const savedState = sessionStorage.getItem("oauth_state");

    if (!code) {
      setError("Missing authorization code");
      return;
    }

    // CLI-initiated login encodes callback port in state as "cli:{port}:{original_state}".
    const CLI_STATE_PREFIX = "cli:";
    const MAX_PORT = 65535;
    if (returnedState?.startsWith(CLI_STATE_PREFIX)) {
      const parts = returnedState.split(":");
      if (parts.length >= 3) {
        const port = parseInt(parts[1], 10);
        const originalState = parts.slice(2).join(":");
        if (port > 0 && port <= MAX_PORT) {
          const params = new URLSearchParams({ state: originalState, code });
          window.location.href = `http://127.0.0.1:${port}/?${params}`;
          return;
        }
      }
      setError("Invalid CLI callback state");
      return;
    }

    if (!savedState || returnedState !== savedState) {
      setError("Invalid OAuth state — possible CSRF attack");
      return;
    }

    sessionStorage.removeItem("oauth_state");

    loginCallback(code, returnedState ?? undefined)
      .then((result) => {
        if (!result.token) {
          setError("Login failed — no session token returned");
          return;
        }
        setSessionToken(result.token);
        setUserEmail(result.email);
        router.replace("/");
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Login failed");
      });
  }, [router, searchParams]);

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-8 shadow-warm text-center">
          <p className="text-sm text-ember-500">{error}</p>
          <a href="/login" className="mt-4 inline-block text-sm text-timber-600 hover:underline">
            Back to login
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <p className="text-sm text-stone-400">Completing login...</p>
    </div>
  );
}

export default function AuthCallbackPage() {
  return (
    <Suspense fallback={<div className="flex min-h-screen items-center justify-center"><p className="text-sm text-stone-400">Loading...</p></div>}>
      <CallbackHandler />
    </Suspense>
  );
}
