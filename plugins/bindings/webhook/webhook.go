package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/principal"
)

var _ core.Binding = (*Binding)(nil)

type webhookConfig struct {
	Path      string `yaml:"path"`
	Provider  string `yaml:"provider"`
	Operation string `yaml:"operation"`
}

type Binding struct {
	name   string
	cfg    webhookConfig
	broker core.Broker
}

func New(name string, cfg webhookConfig, broker core.Broker) *Binding {
	return &Binding{name: name, cfg: cfg, broker: broker}
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
	var body map[string]any
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if b.cfg.Provider == "" || b.cfg.Operation == "" {
		writeJSON(w, http.StatusOK, body)
		return
	}

	p := &principal.Principal{
		CallSource:     "binding",
		CallSourceName: b.name,
	}
	ctx := principal.WithPrincipal(r.Context(), p)

	result, err := b.broker.Invoke(ctx, core.InvocationRequest{
		Provider:  b.cfg.Provider,
		Operation: b.cfg.Operation,
		Params:    body,
	})
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
