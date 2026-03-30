package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/plugins/bindings/internal/httpjson"
)

var _ core.Binding = (*Binding)(nil)

type webhookConfig struct {
	Path            string   `yaml:"path"`
	Provider        string   `yaml:"provider"`
	Instance        string   `yaml:"instance"`
	Operation       string   `yaml:"operation"`
	AuthMode        string   `yaml:"auth_mode"`
	SigningSecret   string   `yaml:"signing_secret"`
	SignatureHeader string   `yaml:"signature_header"`
	UserHeader      string   `yaml:"user_header"`
	TrustedSources  []string `yaml:"trusted_sources"`
}

type Binding struct {
	name    string
	cfg     webhookConfig
	invoker invocation.Invoker
}

func New(name string, cfg webhookConfig, invoker invocation.Invoker) *Binding {
	if cfg.AuthMode == AuthModeTrustedUserHeader && len(cfg.TrustedSources) == 0 {
		log.Printf("warning: webhook binding %q uses trusted_user_header auth without trusted_sources; any client can set the user header", name)
	}
	return &Binding{name: name, cfg: cfg, invoker: invoker}
}

func (b *Binding) Name() string { return b.name }

func (b *Binding) Start(_ context.Context) error {
	return nil
}

func (b *Binding) Close() error { return nil }

func (b *Binding) Routes() []core.Route {
	return []core.Route{
		{
			Method:  http.MethodPost,
			Pattern: b.cfg.Path,
			Handler: http.HandlerFunc(b.handle),
			Public:  true,
		},
	}
}

func (b *Binding) handle(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var userID string

	switch b.cfg.AuthMode {
	case AuthModeSigned:
		header := b.cfg.SignatureHeader
		if header == "" {
			header = DefaultSignatureHeader
		}
		sig := r.Header.Get(header)
		if sig == "" {
			httpjson.WriteError(w, http.StatusUnauthorized, "missing signature")
			return
		}
		if err := verifySignature([]byte(b.cfg.SigningSecret), rawBody, sig); err != nil {
			httpjson.WriteError(w, http.StatusUnauthorized, "invalid signature")
			return
		}

	case AuthModeTrustedUserHeader:
		if len(b.cfg.TrustedSources) > 0 {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if host == "" {
				host = r.RemoteAddr
			}
			remoteIP := net.ParseIP(host)
			trusted := false
			for _, src := range b.cfg.TrustedSources {
				if srcIP := net.ParseIP(src); srcIP != nil && remoteIP != nil && srcIP.Equal(remoteIP) {
					trusted = true
					break
				}
			}
			if !trusted {
				httpjson.WriteError(w, http.StatusForbidden, "untrusted source")
				return
			}
		}
		userID = r.Header.Get(b.cfg.UserHeader)
		if userID == "" {
			httpjson.WriteError(w, http.StatusUnauthorized, "missing user header")
			return
		}
	}

	var body map[string]any
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &body); err != nil {
			httpjson.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if b.cfg.Provider == "" || b.cfg.Operation == "" {
		httpjson.WriteJSON(w, http.StatusOK, body)
		return
	}

	p := &principal.Principal{
		UserID: userID,
	}
	ctx := principal.WithPrincipal(r.Context(), p)

	result, err := b.invoker.Invoke(ctx, p, b.cfg.Provider, b.cfg.Instance, b.cfg.Operation, body)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, "upstream invocation failed")
		return
	}

	w.Header().Set("Content-Type", core.ContentTypeJSON)
	w.WriteHeader(result.Status)
	_, _ = fmt.Fprint(w, result.Body)
}
