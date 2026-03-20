package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
)

var _ core.Binding = (*Binding)(nil)

type webhookConfig struct {
	Path            string `yaml:"path"`
	Provider        string `yaml:"provider"`
	Operation       string `yaml:"operation"`
	AuthMode        string `yaml:"auth_mode"`
	SigningSecret   string `yaml:"signing_secret"`
	SignatureHeader string `yaml:"signature_header"`
	UserHeader      string `yaml:"user_header"`
}

type Binding struct {
	name    string
	cfg     webhookConfig
	invoker invocation.Invoker
}

func New(name string, cfg webhookConfig, invoker invocation.Invoker) *Binding {
	return &Binding{name: name, cfg: cfg, invoker: invoker}
}

func (b *Binding) Name() string           { return b.name }
func (b *Binding) Kind() core.BindingKind { return core.BindingTrigger }

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
		},
	}
}

func (b *Binding) handle(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
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
			writeError(w, http.StatusUnauthorized, "missing signature")
			return
		}
		if err := verifySignature([]byte(b.cfg.SigningSecret), rawBody, sig); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}

	case AuthModeTrustedUserHeader:
		userID = r.Header.Get(b.cfg.UserHeader)
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "missing user header")
			return
		}
	}

	var body map[string]any
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if b.cfg.Provider == "" || b.cfg.Operation == "" {
		writeJSON(w, http.StatusOK, body)
		return
	}

	p := &principal.Principal{
		UserID: userID,
	}
	ctx := principal.WithPrincipal(r.Context(), p)

	result, err := b.invoker.Invoke(ctx, p, b.cfg.Provider, b.cfg.Operation, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream invocation failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)
	_, _ = fmt.Fprint(w, result.Body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
