package pluginapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

const declarativeHTTPTimeout = 30 * time.Second

type declarativeOp struct {
	method    string
	path      string
	paramLocs map[string]string
}

type DeclarativeProvider struct {
	name        string
	displayName string
	description string
	baseURL     string
	auth        *pluginmanifestv1.ProviderAuth
	operations  []core.Operation
	opDefs      map[string]*declarativeOp
	httpClient  *http.Client
}

func NewDeclarativeProvider(manifest *pluginmanifestv1.Manifest, httpClient *http.Client) (*DeclarativeProvider, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if manifest.Provider == nil || !manifest.Provider.IsDeclarative() {
		return nil, fmt.Errorf("manifest is not a declarative provider")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: declarativeHTTPTimeout}
	}

	p := &DeclarativeProvider{
		name:        manifest.Source,
		displayName: manifest.DisplayName,
		description: manifest.Description,
		baseURL:     manifest.Provider.BaseURL,
		auth:        manifest.Provider.Auth,
		opDefs:      make(map[string]*declarativeOp, len(manifest.Provider.Operations)),
		httpClient:  httpClient,
	}

	for i := range manifest.Provider.Operations {
		mop := &manifest.Provider.Operations[i]
		locs := make(map[string]string, len(mop.Parameters))
		for _, mp := range mop.Parameters {
			locs[mp.Name] = mp.In
		}
		p.opDefs[mop.Name] = &declarativeOp{
			method:    mop.Method,
			path:      mop.Path,
			paramLocs: locs,
		}
		coreOp := core.Operation{
			Name:        mop.Name,
			Description: mop.Description,
			Method:      mop.Method,
		}
		for _, mp := range mop.Parameters {
			coreOp.Parameters = append(coreOp.Parameters, core.Parameter{
				Name:        mp.Name,
				Type:        mp.Type,
				Description: mp.Description,
				Required:    mp.Required,
			})
		}
		p.operations = append(p.operations, coreOp)
	}

	return p, nil
}

func (p *DeclarativeProvider) Name() string        { return p.name }
func (p *DeclarativeProvider) DisplayName() string { return p.displayName }
func (p *DeclarativeProvider) Description() string { return p.description }

func (p *DeclarativeProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}

func (p *DeclarativeProvider) ListOperations() []core.Operation {
	out := make([]core.Operation, len(p.operations))
	copy(out, p.operations)
	return out
}

func (p *DeclarativeProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	op, ok := p.opDefs[operation]
	if !ok {
		return &core.OperationResult{
			Status: http.StatusNotFound,
			Body:   `{"error":"unknown operation"}`,
		}, nil
	}

	queryParams := make(map[string]any)
	bodyParams := make(map[string]any)

	isBodyMethod := op.method == http.MethodPost || op.method == http.MethodPut || op.method == http.MethodPatch

	for k, v := range params {
		loc, declared := op.paramLocs[k]
		if !declared {
			if isBodyMethod {
				loc = "body"
			} else {
				loc = "query"
			}
		}
		switch loc {
		case "query":
			queryParams[k] = v
		case "body", "path":
			bodyParams[k] = v
		}
	}

	req := apiexec.Request{
		Method:      op.method,
		BaseURL:     p.baseURL,
		Path:        op.path,
		Params:      bodyParams,
		QueryParams: queryParams,
		Token:       token,
		NoRetry:     true,
	}

	return apiexec.Do(ctx, p.httpClient, req)
}

func (p *DeclarativeProvider) SupportsManualAuth() bool {
	if p.auth == nil {
		return false
	}
	return p.auth.Type == pluginmanifestv1.AuthTypeManual || p.auth.Type == pluginmanifestv1.AuthTypeBearer
}

func (p *DeclarativeProvider) AuthTypes() []string {
	if p.auth == nil {
		return nil
	}
	switch p.auth.Type {
	case pluginmanifestv1.AuthTypeOAuth2:
		return []string{"oauth2"}
	case pluginmanifestv1.AuthTypeBearer:
		return []string{"manual"}
	case pluginmanifestv1.AuthTypeManual:
		return []string{"manual"}
	case pluginmanifestv1.AuthTypeNone:
		return nil
	}
	return nil
}

func (p *DeclarativeProvider) AuthorizationURL(state string, scopes []string) string {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return ""
	}
	return p.auth.AuthorizationURL
}

func (p *DeclarativeProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}
	return nil, fmt.Errorf("declarative OAuth code exchange not yet implemented")
}

func (p *DeclarativeProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}
	return nil, fmt.Errorf("declarative OAuth token refresh not yet implemented")
}
