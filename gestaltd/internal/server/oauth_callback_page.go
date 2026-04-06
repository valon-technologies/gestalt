package server

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
)

var integrationOAuthErrorPage = template.Must(template.New("integration-oauth-error").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body {
      margin: 0;
      font: 16px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f7f4ee;
      color: #221c15;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 20px;
    }
    main {
      width: min(100%, 560px);
      background: #fff;
      border: 1px solid #ddd4c6;
      border-radius: 16px;
      padding: 24px;
      box-shadow: 0 16px 40px rgba(34, 28, 21, 0.08);
    }
    h1 {
      margin: 0;
      font-size: 1.5rem;
      line-height: 1.25;
    }
    p {
      margin: 12px 0 0;
      color: #5f5448;
    }
    a {
      color: #7b5228;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <p><a href="/integrations">Open integrations</a></p>
  </main>
</body>
</html>
`))

type integrationOAuthErrorPageView struct {
	Title   string
	Message string
}

func requestAcceptsHTML(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func writeIntegrationOAuthErrorPage(w http.ResponseWriter, status int, title, message string) {
	var buf bytes.Buffer
	if err := integrationOAuthErrorPage.Execute(&buf, integrationOAuthErrorPageView{
		Title:   title,
		Message: message,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render oauth error page")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
