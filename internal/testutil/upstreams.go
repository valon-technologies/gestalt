package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type OAuthServerOptions struct {
	Email        string
	DisplayName  string
	PictureURL   string
	TokenExtras  map[string]any
	RefreshToken string
}

type OAuthServer struct {
	URL string

	server *httptest.Server

	email        string
	displayName  string
	pictureURL   string
	tokenExtras  map[string]any
	refreshToken string

	mu          sync.Mutex
	nextCodeID  int
	tokenByCode map[string]string
}

func NewOAuthServer(t *testing.T, opts OAuthServerOptions) *OAuthServer {
	t.Helper()

	srv := &OAuthServer{
		email:        opts.Email,
		displayName:  opts.DisplayName,
		pictureURL:   opts.PictureURL,
		tokenExtras:  cloneMap(opts.TokenExtras),
		refreshToken: opts.RefreshToken,
		tokenByCode:  make(map[string]string),
	}
	if srv.email == "" {
		srv.email = "integration-test@gestalt.dev"
	}
	if srv.displayName == "" {
		srv.displayName = "Integration Test User"
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			redirectURI := r.URL.Query().Get("redirect_uri")
			state := r.URL.Query().Get("state")
			if redirectURI == "" {
				http.Error(w, "missing redirect_uri", http.StatusBadRequest)
				return
			}
			code, accessToken := srv.issueCode()
			srv.mu.Lock()
			srv.tokenByCode[code] = accessToken
			srv.mu.Unlock()

			target := fmt.Sprintf("%s?code=%s", redirectURI, code)
			if state != "" {
				target += "&state=" + state
			}
			http.Redirect(w, r, target, http.StatusFound)
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid token request", http.StatusBadRequest)
				return
			}
			code := r.Form.Get("code")
			accessToken, ok := srv.accessTokenForCode(code)
			if !ok {
				writeJSONResponse(w, http.StatusBadRequest, map[string]any{
					"error":             "invalid_grant",
					"error_description": "unknown authorization code",
				})
				return
			}
			resp := map[string]any{
				"access_token": accessToken,
				"token_type":   "Bearer",
				"expires_in":   3600,
			}
			if srv.refreshToken != "" {
				resp["refresh_token"] = srv.refreshToken
			}
			for key, value := range srv.tokenExtras {
				resp[key] = value
			}
			writeJSONResponse(w, http.StatusOK, resp)
		case "/userinfo":
			token := bearerToken(r)
			if token == "" || !srv.hasAccessToken(token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"email":          srv.email,
				"name":           srv.displayName,
				"picture":        srv.pictureURL,
				"email_verified": true,
			})
		case "/.well-known/openid-configuration":
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"userinfo_endpoint":      server.URL + "/userinfo",
			})
		default:
			http.NotFound(w, r)
		}
	}))

	srv.server = server
	srv.URL = server.URL
	t.Cleanup(server.Close)
	return srv
}

func (s *OAuthServer) Close() {
	if s != nil && s.server != nil {
		s.server.Close()
	}
}

func (s *OAuthServer) issueCode() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextCodeID++
	code := fmt.Sprintf("code-%d", s.nextCodeID)
	accessToken := fmt.Sprintf("access-token-%d", s.nextCodeID)
	return code, accessToken
}

func (s *OAuthServer) accessTokenForCode(code string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokenByCode[code]
	return token, ok
}

func (s *OAuthServer) hasAccessToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, issued := range s.tokenByCode {
		if issued == token {
			return true
		}
	}
	return false
}

type DiscoveryServerOptions struct {
	Candidates []map[string]any
}

type DiscoveryServer struct {
	URL string

	server *httptest.Server
}

func NewDiscoveryServer(t *testing.T, opts DiscoveryServerOptions) *DiscoveryServer {
	t.Helper()

	candidates := opts.Candidates
	if len(candidates) == 0 {
		candidates = []map[string]any{
			{"id": "acct-a", "name": "Alpha", "region": "us-east-1"},
			{"id": "acct-b", "name": "Beta", "region": "us-west-2"},
		}
	}

	srv := &DiscoveryServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"accounts": candidates,
		})
	}))
	srv.server = server
	srv.URL = server.URL + "/accounts"
	t.Cleanup(server.Close)
	return srv
}

func (s *DiscoveryServer) Close() {
	if s != nil && s.server != nil {
		s.server.Close()
	}
}

type OpenAPIServer struct {
	URL string

	server *httptest.Server
}

func NewOpenAPIServer(t *testing.T, expectedBearer string) *OpenAPIServer {
	t.Helper()

	srv := &OpenAPIServer{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"openapi": "3.0.0",
				"info": map[string]any{
					"title":   "Warehouse API",
					"version": "1.0.0",
				},
				"servers": []map[string]any{{"url": server.URL}},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
				},
			})
		case "/items":
			if expectedBearer != "" {
				want := "Bearer " + expectedBearer
				if r.Header.Get("Authorization") != want {
					writeJSONResponse(w, http.StatusUnauthorized, map[string]any{
						"error": "unexpected authorization header",
					})
					return
				}
			}
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"id": "1", "name": "alpha"},
					{"id": "2", "name": "beta"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	srv.server = server
	srv.URL = server.URL + "/openapi.json"
	t.Cleanup(server.Close)
	return srv
}

func (s *OpenAPIServer) Close() {
	if s != nil && s.server != nil {
		s.server.Close()
	}
}

type MCPServer struct {
	URL string

	server *httptest.Server
}

func NewMCPServer(t *testing.T) *MCPServer {
	t.Helper()

	srv := mcpserver.NewMCPServer("test-remote", "1.0.0")
	srv.AddTool(
		mcpgo.NewTool("echo", mcpgo.WithDescription("Echo input")),
		func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText(fmt.Sprintf("%v", req.GetArguments()["message"])), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &MCPServer{URL: server.URL, server: server}
}

func (s *MCPServer) Close() {
	if s != nil && s.server != nil {
		s.server.Close()
	}
}

func writeJSONResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
