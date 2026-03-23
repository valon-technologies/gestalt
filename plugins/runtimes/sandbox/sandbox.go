package sandbox

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	internalsandbox "github.com/valon-technologies/gestalt/internal/sandbox"
	pb "github.com/valon-technologies/gestalt/internal/sandbox/pb"
)

var _ core.Runtime = (*Runtime)(nil)
var _ core.ChatDispatcher = (*Runtime)(nil)

type Runtime struct {
	name       string
	deps       bootstrap.RuntimeDeps
	pool       *internalsandbox.Pool
	grpcServer *grpc.Server
	listener   net.Listener
	cfg        runtimeConfig

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

type runtimeConfig struct {
	PythonCommand string `yaml:"python_command"`
	SandboxScript string `yaml:"sandbox_script"`
	MaxSandboxes  int    `yaml:"max_sandboxes"`
	IdleTimeout   string `yaml:"idle_timeout"`
	GRPCPort      int    `yaml:"grpc_port"`
}

func New(name string, cfg runtimeConfig, deps bootstrap.RuntimeDeps) *Runtime {
	return &Runtime{
		name:    name,
		deps:    deps,
		cfg:     cfg,
		cancels: make(map[string]context.CancelFunc),
	}
}

func (r *Runtime) Name() string { return r.name }

func (r *Runtime) Start(_ context.Context) error {
	addr := fmt.Sprintf(":%d", r.cfg.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("sandbox runtime %q: listen on %s: %w", r.name, addr, err)
	}
	r.listener = lis

	srv := grpc.NewServer()
	toolSrv := internalsandbox.NewToolServer(r.deps.Invoker, r.deps.CapabilityLister)
	pb.RegisterToolServiceServer(srv, toolSrv)
	r.grpcServer = srv

	go func() {
		if err := srv.Serve(lis); err != nil {
			log.Printf("sandbox runtime %q: grpc server error: %v", r.name, err)
		}
	}()

	idleTimeout, _ := time.ParseDuration(r.cfg.IdleTimeout)
	r.pool = internalsandbox.NewPool(
		r.cfg.PythonCommand,
		r.cfg.SandboxScript,
		r.listener.Addr().String(),
		r.cfg.MaxSandboxes,
		idleTimeout,
	)

	log.Printf("sandbox runtime %q started: grpc=%s, max_sandboxes=%d", r.name, r.listener.Addr(), r.cfg.MaxSandboxes)
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if r.pool != nil {
		r.pool.Shutdown(ctx)
	}
	if r.grpcServer != nil {
		r.grpcServer.GracefulStop()
	}
	log.Printf("sandbox runtime %q stopped", r.name)
	return nil
}

func (r *Runtime) SendMessage(ctx context.Context, conversationID, content, userID string) (<-chan core.ChatEvent, error) {
	store := r.deps.ChatStore
	if store == nil {
		return nil, fmt.Errorf("sandbox runtime %q: chat store not configured", r.name)
	}

	conv, err := store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("getting conversation: %w", err)
	}

	agent, err := store.GetAgent(ctx, conv.AgentID)
	if err != nil {
		return nil, fmt.Errorf("getting agent: %w", err)
	}

	userMsg := &core.Message{
		ID:             uuid.New().String(),
		ConversationID: conversationID,
		Role:           core.RoleUser,
		Content:        content,
		CreatedAt:      time.Now(),
	}
	if err := store.AppendMessage(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("appending user message: %w", err)
	}

	proc, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring sandbox: %w", err)
	}

	var allowedTools []string
	for _, p := range agent.Providers {
		allowedTools = append(allowedTools, p)
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

	convCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancels[conversationID] = cancel
	r.mu.Unlock()

	events, err := internalsandbox.Converse(proc, convCtx, req)
	if err != nil {
		cancel()
		r.pool.Release(proc)
		r.mu.Lock()
		delete(r.cancels, conversationID)
		r.mu.Unlock()
		return nil, fmt.Errorf("starting conversation: %w", err)
	}

	out := make(chan core.ChatEvent, 32)
	go func() {
		defer close(out)
		defer r.pool.Release(proc)
		defer func() {
			r.mu.Lock()
			delete(r.cancels, conversationID)
			r.mu.Unlock()
			cancel()
		}()

		var fullText strings.Builder
		var inputTokens, outputTokens int

		for event := range events {
			ce := convertEvent(conversationID, event)
			if ce == nil {
				continue
			}

			if ce.Type == core.ChatEventTextDelta {
				fullText.WriteString(ce.Text)
			}
			if ce.Type == core.ChatEventComplete {
				inputTokens = ce.InputTokens
				outputTokens = ce.OutputTokens
			}

			select {
			case out <- *ce:
			case <-convCtx.Done():
				return
			}

			if ce.Type == core.ChatEventComplete {
				msg := &core.Message{
					ID:             uuid.New().String(),
					ConversationID: conversationID,
					Role:           core.RoleAssistant,
					Content:        fullText.String(),
					InputTokens:    inputTokens,
					OutputTokens:   outputTokens,
					CreatedAt:      time.Now(),
				}
				_ = store.AppendMessage(context.Background(), msg)
			}
		}
	}()

	return out, nil
}

func (r *Runtime) CancelConversation(_ context.Context, conversationID string) error {
	r.mu.Lock()
	cancel, ok := r.cancels[conversationID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

func convertEvent(conversationID string, event *pb.ConversationEvent) *core.ChatEvent {
	switch e := event.GetEvent().(type) {
	case *pb.ConversationEvent_TextDelta:
		return &core.ChatEvent{
			Type:           core.ChatEventTextDelta,
			ConversationID: conversationID,
			Text:           e.TextDelta.GetContent(),
		}
	case *pb.ConversationEvent_ToolUse:
		return &core.ChatEvent{
			Type:           core.ChatEventToolUse,
			ConversationID: conversationID,
			ToolCallID:     e.ToolUse.GetToolCallId(),
			ToolName:       e.ToolUse.GetToolName(),
			ToolInput:      e.ToolUse.GetInputJson(),
		}
	case *pb.ConversationEvent_ToolResult:
		return &core.ChatEvent{
			Type:           core.ChatEventToolResult,
			ConversationID: conversationID,
			ToolCallID:     e.ToolResult.GetToolCallId(),
			Text:           e.ToolResult.GetContentJson(),
		}
	case *pb.ConversationEvent_TurnComplete:
		return &core.ChatEvent{
			Type:           core.ChatEventComplete,
			ConversationID: conversationID,
			Text:           e.TurnComplete.GetFullText(),
			InputTokens:    int(e.TurnComplete.GetInputTokens()),
			OutputTokens:   int(e.TurnComplete.GetOutputTokens()),
		}
	case *pb.ConversationEvent_Error:
		return &core.ChatEvent{
			Type:           core.ChatEventError,
			ConversationID: conversationID,
			Error:          e.Error.GetMessage(),
		}
	default:
		return nil
	}
}
