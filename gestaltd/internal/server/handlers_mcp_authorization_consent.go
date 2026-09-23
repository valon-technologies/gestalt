package server

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

const mcpOAuthConsentDefaultClientName = "This application"

// Chromium applies form-action to redirects after a form submission. OAuth
// callback URLs are client-controlled, so the consent page cannot use the
// global form-action restriction.
const mcpOAuthConsentContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"frame-ancestors 'self'"

type mcpOAuthConsentPageView struct {
	ClientName  string
	Email       string
	RedirectURI string
	Scope       string
	Consent     string
}

func mcpOAuthConsentRedirectHost(redirectURI string) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Host == "" {
		return redirectURI
	}
	return parsed.Host
}

func mcpOAuthConsentAccessText(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "It will have full MCP access."
	}
	return "It will have access to: " + scope
}

func renderMCPOAuthConsentPage(w http.ResponseWriter, view mcpOAuthConsentPageView) {
	clientName := strings.TrimSpace(view.ClientName)
	if clientName == "" {
		clientName = mcpOAuthConsentDefaultClientName
	}
	host := mcpOAuthConsentRedirectHost(view.RedirectURI)

	setMCPOAuthNoStoreHeaders(w)
	w.Header().Set("Content-Security-Policy", mcpOAuthConsentContentSecurityPolicy)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Authorize %s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f8fafc; color: #0f172a; }
    main { max-width: 32rem; padding: 2rem; background: #fff; border: 1px solid #e2e8f0; border-radius: 12px; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08); }
    h1 { margin: 0 0 0.75rem; font-size: 1.5rem; }
    p { margin: 0 0 1rem; line-height: 1.5; color: #334155; }
    .host { font-family: ui-monospace, monospace; background: #f1f5f9; padding: 0.125rem 0.375rem; border-radius: 6px; }
    .access { margin: 0 0 1.5rem; color: #334155; }
    .actions { display: flex; gap: 0.75rem; }
    button { flex: 1; padding: 0.625rem 1rem; border-radius: 8px; border: 1px solid transparent; font-weight: 600; cursor: pointer; font-size: 1rem; }
    button.approve { background: #0f172a; color: #fff; }
    button.approve:hover { background: #1e293b; }
    button.deny { background: #fff; color: #0f172a; border-color: #cbd5e1; }
    button.deny:hover { background: #f1f5f9; }
  </style>
</head>
<body>
  <main>
    <h1>%s wants to access your account</h1>
    <p>Signed in as <strong>%s</strong>. Approving sends an authorization code to <span class="host">%s</span>, which will be able to act on your behalf.</p>
    <p class="access">%s</p>
    <form method="POST" action="%s">
      <input type="hidden" name="consent" value="%s">
      <div class="actions">
        <button class="deny" type="submit" name="decision" value="%s">Deny</button>
        <button class="approve" type="submit" name="decision" value="%s">Approve</button>
      </div>
    </form>
  </main>
</body>
</html>`,
		html.EscapeString(clientName),
		html.EscapeString(clientName),
		html.EscapeString(view.Email),
		html.EscapeString(host),
		html.EscapeString(mcpOAuthConsentAccessText(view.Scope)),
		mcpConsentEndpointPath,
		html.EscapeString(view.Consent),
		mcpOAuthConsentDecisionDeny,
		mcpOAuthConsentDecisionApprove,
	)
}
