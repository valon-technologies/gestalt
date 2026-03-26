package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
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
		case "/.well-known/openid-configuration":
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"userinfo_endpoint":      server.URL + "/userinfo",
			})
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
		default:
			http.NotFound(w, r)
		}
	}))

	srv.server = server
	srv.URL = server.URL
	t.Cleanup(server.Close)
	return srv
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

type OpenAPIFixtureOptions struct {
	ExpectedBearerToken string
	DiscoveryCandidates []map[string]any
}

type OpenAPIFixture struct {
	Server  *httptest.Server
	SpecURL string

	expectedBearerToken string
	lastAuthorization   atomic.Value
	discoveryCandidates atomic.Value
}

func NewOpenAPIFixture(t *testing.T, opts OpenAPIFixtureOptions) *OpenAPIFixture {
	t.Helper()

	fixture := &OpenAPIFixture{
		expectedBearerToken: opts.ExpectedBearerToken,
	}
	if len(opts.DiscoveryCandidates) > 0 {
		fixture.discoveryCandidates.Store(opts.DiscoveryCandidates)
	}

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
			fixture.lastAuthorization.Store(r.Header.Get("Authorization"))
			if fixture.expectedBearerToken != "" {
				expected := "Bearer " + fixture.expectedBearerToken
				if r.Header.Get("Authorization") != expected {
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
		case "/discovery/accounts":
			candidates, _ := fixture.discoveryCandidates.Load().([]map[string]any)
			if len(candidates) == 0 {
				candidates = []map[string]any{
					{"id": "acct-1", "name": "Primary", "region": "us-east-1"},
				}
			}
			writeJSONResponse(w, http.StatusOK, map[string]any{
				"accounts": candidates,
			})
		default:
			http.NotFound(w, r)
		}
	}))

	fixture.Server = server
	fixture.SpecURL = server.URL + "/openapi.json"
	t.Cleanup(server.Close)
	return fixture
}

func (f *OpenAPIFixture) LastAuthorization() string {
	value := f.lastAuthorization.Load()
	if value == nil {
		return ""
	}
	auth, _ := value.(string)
	return auth
}

func writeJSONResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
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
