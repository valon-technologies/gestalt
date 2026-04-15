package server

import (
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

const browserLoginCallbackHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Completing login...</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f5f5f2;
      color: #111827;
    }
    main {
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
    }
    .card {
      width: min(420px, 100%);
      background: rgba(255, 255, 255, 0.96);
      border: 1px solid rgba(17, 24, 39, 0.08);
      border-radius: 16px;
      box-shadow: 0 12px 32px rgba(17, 24, 39, 0.08);
      padding: 24px;
    }
    h1 {
      margin: 0 0 8px;
      font-size: 20px;
    }
    p {
      margin: 0;
      line-height: 1.5;
      color: #4b5563;
    }
    .error {
      color: #b91c1c;
    }
    @media (prefers-color-scheme: dark) {
      body { background: #0f172a; color: #f8fafc; }
      .card {
        background: rgba(15, 23, 42, 0.94);
        border-color: rgba(148, 163, 184, 0.18);
        box-shadow: 0 16px 40px rgba(2, 6, 23, 0.4);
      }
      p { color: #cbd5e1; }
      .error { color: #fca5a5; }
    }
  </style>
</head>
<body>
  <main>
    <section class="card">
      <h1 id="title">Completing login...</h1>
      <p id="message">Finishing authentication.</p>
    </section>
  </main>
  <script>
    (async function () {
      const title = document.getElementById("title");
      const message = document.getElementById("message");
      const params = new URLSearchParams(window.location.search);
      const code = params.get("code");
      const rawState = params.get("state") || "";

      function fail(text) {
        title.textContent = "Login failed";
        message.textContent = text;
        message.className = "error";
      }

      function decodeHostState(raw) {
        if (!raw) return raw;
        try {
          const base64 = raw.replace(/-/g, "+").replace(/_/g, "/");
          const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
          const decoded = atob(padded);
          const bytes = Uint8Array.from(decoded, ch => ch.charCodeAt(0));
          const payload = JSON.parse(new TextDecoder().decode(bytes));
          if (payload && typeof payload.host_state === "string" && payload.host_state.length > 0) {
            return payload.host_state;
          }
        } catch (_) {}
        return raw;
      }

      function cliCallbackURL(hostState, authCode) {
        if (!hostState || !hostState.startsWith("cli:")) return null;
        const parts = hostState.split(":");
        const port = Number(parts[1]);
        const originalState = parts.slice(2).join(":");
        if (!Number.isInteger(port) || port < 1 || port > 65535 || !originalState) return null;
        const query = new URLSearchParams({ code: authCode, state: originalState });
        return "http://127.0.0.1:" + port + "/?" + query.toString();
      }

      if (!code) {
        fail("Missing authorization code");
        return;
      }

      const hostState = decodeHostState(rawState);
      const cliURL = cliCallbackURL(hostState, code);
      if (cliURL) {
        window.location.replace(cliURL);
        return;
      }

      const storedState = sessionStorage.getItem("oauth_state");
      if (!storedState || hostState !== storedState) {
        fail("Invalid OAuth state — possible CSRF attack");
        return;
      }
      sessionStorage.removeItem("oauth_state");

      try {
        const response = await fetch("__AUTH_CALLBACK_PATH__?" + params.toString(), {
          credentials: "include",
          headers: { "Accept": "application/json" },
        });
        const text = await response.text();
        let payload = {};
        try {
          payload = text ? JSON.parse(text) : {};
        } catch (_) {
          throw new Error(text || "Login failed");
        }
        if (!response.ok) {
          throw new Error(payload.error || text || "Login failed");
        }
        if (typeof payload.email === "string" && payload.email.length > 0) {
          localStorage.setItem("user_email", payload.email);
        }
        const nextPath = typeof payload.nextPath === "string" && payload.nextPath.length > 0 ? payload.nextPath : "/";
        window.location.replace(nextPath);
      } catch (err) {
        fail(err instanceof Error ? err.message : "Login failed");
      }
    })();
  </script>
</body>
</html>
`

var browserLoginCallbackHTML = strings.ReplaceAll(
	browserLoginCallbackHTMLTemplate,
	"__AUTH_CALLBACK_PATH__",
	config.AuthCallbackPath,
)

func (s *Server) browserLoginCallbackPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(browserLoginCallbackHTML))
}

func isBrowserDocumentRequest(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode")), "navigate") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest")), "document") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func (s *Server) pendingLoginState(r *http.Request) (*loginState, bool) {
	if s.encryptor == nil {
		return nil, false
	}
	cookie, err := r.Cookie(loginStateCookieName)
	if err != nil {
		return nil, false
	}
	state, err := decodeLoginState(s.encryptor, cookie.Value, s.now())
	if err != nil {
		return nil, false
	}
	return state, true
}

func (s *Server) shouldRedirectLegacyBrowserLoginCallback(r *http.Request) bool {
	if r.URL.Query().Get("cli") == "1" || !isBrowserDocumentRequest(r) {
		return false
	}
	if pending, ok := s.pendingLoginState(r); ok && pending.NextPath != "" {
		return false
	}
	return true
}

func browserCallbackRedirectLocation(r *http.Request) string {
	target := config.BrowserAuthCallbackPath
	if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
		target += "?" + rawQuery
	}
	return target
}
