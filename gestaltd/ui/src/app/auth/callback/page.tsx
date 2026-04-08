"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { loginCallback } from "@/lib/api";
import { setUserEmail } from "@/lib/auth";

const CLI_STATE_PREFIX = "cli:";
const CLI_CALLBACK_ORIGIN = "http://127.0.0.1";
const MAX_PORT = 65535;

type WrappedAuthState = {
  host_state?: string;
};

function decodeWrappedHostState(state: string | null): string | null {
  if (!state) {
    return state;
  }

  try {
    const padded = state
      .replace(/-/g, "+")
      .replace(/_/g, "/")
      .padEnd(Math.ceil(state.length / 4) * 4, "=");
    const binary = atob(padded);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    const parsed = JSON.parse(
      new TextDecoder().decode(bytes),
    ) as WrappedAuthState;
    return typeof parsed.host_state === "string" && parsed.host_state.length > 0
      ? parsed.host_state
      : state;
  } catch {
    return state;
  }
}

function getCliCallbackURL(state: string | null, code: string): string | null {
  if (!state?.startsWith(CLI_STATE_PREFIX)) {
    return null;
  }

  const [, rawPort, ...rest] = state.split(":");
  const port = Number(rawPort);
  const callbackState = rest.join(":");
  if (!Number.isInteger(port) || port < 1 || port > MAX_PORT || !callbackState) {
    return null;
  }

  return `${CLI_CALLBACK_ORIGIN}:${port}/?${new URLSearchParams({
    code,
    state: callbackState,
  })}`;
}

export default function AuthCallbackPage() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code");
    const rawState = params.get("state");
    const hostState = decodeWrappedHostState(rawState);
    const savedState = sessionStorage.getItem("oauth_state");

    if (!code) {
      setError("Missing authorization code");
      return;
    }

    const cliCallbackURL = getCliCallbackURL(hostState, code);
    if (cliCallbackURL) {
      window.location.href = cliCallbackURL;
      return;
    }

    if (hostState?.startsWith(CLI_STATE_PREFIX)) {
      setError("Invalid CLI callback state");
      return;
    }

    if (!savedState || hostState !== savedState) {
      setError("Invalid OAuth state — possible CSRF attack");
      return;
    }

    sessionStorage.removeItem("oauth_state");

    loginCallback(code, rawState ?? undefined)
      .then((result) => {
        setUserEmail(result.email);
        router.replace("/");
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Login failed");
      });
  }, [router]);

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="w-full max-w-sm rounded-lg border border-alpha bg-base-white p-8 shadow-dropdown text-center dark:bg-surface">
          <p className="text-sm text-ember-500">{error}</p>
          <a href="/login" className="mt-4 inline-block text-sm text-primary hover:underline">
            Back to login
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <p className="text-sm text-faint">Completing login...</p>
    </div>
  );
}
