package agentic

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/agentic/sandbox"
	pb "github.com/valon-technologies/gestalt/plugins/agentic/sandbox/pb"
)

var (
	_ core.Runtime  = (*Runtime)(nil)
	_ Dispatcher    = (*Runtime)(nil)
	_ StoreProvider = (*Runtime)(nil)
)

type runtimeConfig struct {
	StorePath     string `yaml:"store_path"`
	PythonCommand string `yaml:"python_command"`
	SandboxScript string `yaml:"sandbox_script"`
	MaxSandboxes  int    `yaml:"max_sandboxes"`
	IdleTimeout   string `yaml:"idle_timeout"`
	GRPCPort      int    `yaml:"grpc_port"`
}

type Runtime struct {
	name       string
	cfg        runtimeConfig
	deps       bootstrap.RuntimeDeps
	store      Store
	pool       *sandbox.Pool
	grpcServer *grpc.Server
	listener   net.Listener
}

func NewRuntime(name string, cfg runtimeConfig, deps bootstrap.RuntimeDeps) (*Runtime, error) {
	store, err := NewSQLiteStore(cfg.StorePath)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	return &Runtime{name: name, cfg: cfg, deps: deps, store: store}, nil
}

func (r *Runtime) Name() string { return r.name }
func (r *Runtime) Store() Store { return r.store }

func (r *Runtime) Start(_ context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", r.cfg.GRPCPort))
	if err != nil {
		return err
	}
	r.listener = lis
	r.grpcServer = grpc.NewServer()
	pb.RegisterToolServiceServer(r.grpcServer, sandbox.NewToolServer(r.deps.Invoker, r.deps.CapabilityLister))
	go func() { _ = r.grpcServer.Serve(lis) }()

	idleTimeout, _ := time.ParseDuration(r.cfg.IdleTimeout)
	if idleTimeout == 0 {
		idleTimeout = 5 * time.Minute
	}
	toolAddr := fmt.Sprintf("localhost:%d", r.cfg.GRPCPort)
	r.pool = sandbox.NewPool(r.cfg.PythonCommand, r.cfg.SandboxScript, toolAddr, r.cfg.MaxSandboxes, idleTimeout)

	log.Printf("agentic runtime %q started: grpc=:%d, store=%s", r.name, r.cfg.GRPCPort, r.cfg.StorePath)
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if r.pool != nil {
		r.pool.Shutdown(ctx)
	}
	if r.grpcServer != nil {
		r.grpcServer.GracefulStop()
	}
	if r.store != nil {
		_ = r.store.Close()
	}
	return nil
}

func (r *Runtime) SendMessage(ctx context.Context, conversationID, content, userID string) (<-chan ChatEvent, error) {
	conv, err := r.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	agent, err := r.store.GetAgent(ctx, conv.AgentID)
	if err != nil {
		return nil, fmt.Errorf("loading agent %s: %w", conv.AgentID, err)
	}

	userMsg := &Message{
		ConversationID: conversationID,
		Role:           RoleUser,
		Content:        content,
	}
	if err := r.store.AppendMessage(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("storing user message: %w", err)
	}

	proc, err := r.pool.Acquire()
	if err != nil {
		return nil, fmt.Errorf("acquiring sandbox: %w", err)
	}

	var allowedTools []string
	for _, p := range agent.Providers {
		allowedTools = append(allowedTools, p+"_*")
	}

	req := &pb.ConversationRequest{
		ConversationId: conversationID,
		UserMessage:    content,
		UserId:         userID,
		Model:          agent.Model,
		SystemPrompt:   agent.SystemPrompt,
		AllowedTools:   allowedTools,
		Settings: &pb.AgentSettings{
			Temperature: agent.Temperature,
			MaxTokens:   int32(agent.MaxTokens),
		},
	}

	events, err := proc.Converse(ctx, req)
	if err != nil {
		r.pool.Release(proc)
		return nil, fmt.Errorf("starting conversation: %w", err)
	}

	out := make(chan ChatEvent, 64)
	go func() {
		defer close(out)
		defer r.pool.Release(proc)

		var fullText string
		var inputTokens, outputTokens int

		for ev := range events {
			if ev.TextDelta != "" {
				out <- ChatEvent{
					Type:           ChatEventTextDelta,
					ConversationID: conversationID,
					Text:           ev.TextDelta,
				}
			}
			if ev.ToolName != "" {
				out <- ChatEvent{
					Type:           ChatEventToolUse,
					ConversationID: conversationID,
					ToolCallID:     ev.ToolCallID,
					ToolName:       ev.ToolName,
					ToolInput:      ev.ToolInput,
				}
			}
			if ev.ToolResult != "" {
				out <- ChatEvent{
					Type:           ChatEventToolResult,
					ConversationID: conversationID,
					ToolCallID:     ev.ToolCallID,
					Text:           ev.ToolResult,
				}
			}
			if ev.Error != "" {
				out <- ChatEvent{
					Type:           ChatEventError,
					ConversationID: conversationID,
					Error:          ev.Error,
				}
			}
			if ev.Done && ev.Error == "" {
				fullText = ev.FullText
				inputTokens = int(ev.InputTokens)
				outputTokens = int(ev.OutputTokens)
			}
		}

		if fullText != "" {
			assistantMsg := &Message{
				ID:             uuid.NewString(),
				ConversationID: conversationID,
				Role:           RoleAssistant,
				Content:        fullText,
				InputTokens:    inputTokens,
				OutputTokens:   outputTokens,
			}
			_ = r.store.AppendMessage(context.Background(), assistantMsg)
		}

		out <- ChatEvent{
			Type:           ChatEventComplete,
			ConversationID: conversationID,
			InputTokens:    inputTokens,
			OutputTokens:   outputTokens,
		}
	}()

	return out, nil
}

func (r *Runtime) CancelConversation(_ context.Context, _ string) error {
	return nil
}
