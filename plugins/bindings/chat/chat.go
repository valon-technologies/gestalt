package chat

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
)

var _ core.Binding = (*Binding)(nil)

type bindingConfig struct {
	Runtime string `yaml:"runtime"`
}

type Binding struct {
	name       string
	cfg        bindingConfig
	chatStore  core.ChatStore
	dispatcher core.ChatDispatcher
}

func New(name string, cfg bindingConfig, chatStore core.ChatStore, dispatcher core.ChatDispatcher) *Binding {
	return &Binding{name: name, cfg: cfg, chatStore: chatStore, dispatcher: dispatcher}
}

func (b *Binding) Name() string           { return b.name }
func (b *Binding) Kind() core.BindingKind { return core.BindingSurface }
func (b *Binding) Start(_ context.Context) error { return nil }
func (b *Binding) Close() error                  { return nil }

func (b *Binding) Routes() []core.Route {
	return []core.Route{
		{Method: http.MethodGet, Pattern: "/agents", Handler: http.HandlerFunc(b.listAgents)},
		{Method: http.MethodPost, Pattern: "/agents", Handler: http.HandlerFunc(b.createAgent)},
		{Method: http.MethodGet, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.getAgent)},
		{Method: http.MethodPut, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.updateAgent)},
		{Method: http.MethodDelete, Pattern: "/agents/{id}", Handler: http.HandlerFunc(b.deleteAgent)},
		{Method: http.MethodGet, Pattern: "/conversations", Handler: http.HandlerFunc(b.listConversations)},
		{Method: http.MethodPost, Pattern: "/conversations", Handler: http.HandlerFunc(b.createConversation)},
		{Method: http.MethodGet, Pattern: "/conversations/{id}", Handler: http.HandlerFunc(b.getConversation)},
		{Method: http.MethodGet, Pattern: "/conversations/{id}/messages", Handler: http.HandlerFunc(b.listMessages)},
		{Method: http.MethodPost, Pattern: "/conversations/{id}/messages", Handler: http.HandlerFunc(b.sendMessage)},
	}
}
