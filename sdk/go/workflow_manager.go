package gestalt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	gproto "google.golang.org/protobuf/proto"
)

const EnvWorkflowManagerSocket = proto.EnvWorkflowManagerSocket
const EnvWorkflowManagerSocketToken = EnvWorkflowManagerSocket + "_TOKEN"

type WorkflowManagerClient struct {
	client          proto.WorkflowManagerHostClient
	invocationToken string
}

var sharedWorkflowManagerTransport struct {
	mu     sync.Mutex
	target string
	token  string
	conn   *grpc.ClientConn
	client proto.WorkflowManagerHostClient
}

func WorkflowManager(invocationToken string) (*WorkflowManagerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("workflow manager: invocation token is not available")
	}
	target := os.Getenv(EnvWorkflowManagerSocket)
	if target == "" {
		return nil, fmt.Errorf("workflow manager: %s is not set", EnvWorkflowManagerSocket)
	}
	token := os.Getenv(EnvWorkflowManagerSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedWorkflowManagerClient(ctx, target, token)
	if err != nil {
		return nil, err
	}

	return &WorkflowManagerClient{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

func sharedWorkflowManagerClient(ctx context.Context, target, token string) (proto.WorkflowManagerHostClient, error) {
	sharedWorkflowManagerTransport.mu.Lock()
	if sharedWorkflowManagerTransport.conn != nil && sharedWorkflowManagerTransport.target == target && sharedWorkflowManagerTransport.token == token {
		client := sharedWorkflowManagerTransport.client
		sharedWorkflowManagerTransport.mu.Unlock()
		return client, nil
	}
	sharedWorkflowManagerTransport.mu.Unlock()

	network, address, err := parseWorkflowManagerTarget(target)
	if err != nil {
		return nil, err
	}
	opts := workflowManagerDialOptions(token)
	var conn *grpc.ClientConn
	switch network {
	case "unix":
		conn, err = grpc.DialContext(ctx, "passthrough:///localhost",
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", address)
				}),
				grpc.WithAuthority("localhost"),
				grpc.WithBlock(),
			}, opts...)...,
		)
	case "tcp":
		conn, err = grpc.DialContext(ctx, address,
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			}, opts...)...,
		)
	case "tls":
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, fmt.Errorf("workflow manager: parse tls target %q: %w", address, splitErr)
		}
		conn, err = grpc.DialContext(ctx, address,
			append([]grpc.DialOption{
				grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: host,
					NextProtos: []string{"h2"},
				})),
				grpc.WithBlock(),
			}, opts...)...,
		)
	default:
		return nil, fmt.Errorf("workflow manager: unsupported transport network %q", network)
	}
	if err != nil {
		return nil, fmt.Errorf("workflow manager: connect to host: %w", err)
	}

	client := proto.NewWorkflowManagerHostClient(conn)

	sharedWorkflowManagerTransport.mu.Lock()
	defer sharedWorkflowManagerTransport.mu.Unlock()

	if sharedWorkflowManagerTransport.conn != nil && sharedWorkflowManagerTransport.target == target && sharedWorkflowManagerTransport.token == token {
		_ = conn.Close()
		return sharedWorkflowManagerTransport.client, nil
	}
	if sharedWorkflowManagerTransport.conn != nil {
		_ = sharedWorkflowManagerTransport.conn.Close()
	}

	sharedWorkflowManagerTransport.target = target
	sharedWorkflowManagerTransport.token = token
	sharedWorkflowManagerTransport.conn = conn
	sharedWorkflowManagerTransport.client = client
	return client, nil
}

func workflowManagerDialOptions(token string) []grpc.DialOption {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(workflowManagerRelayPerRPCCredentials{token: token})}
}

type workflowManagerRelayPerRPCCredentials struct {
	token string
}

func (c workflowManagerRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (workflowManagerRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func parseWorkflowManagerTarget(raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("workflow manager: transport target is required")
	}
	switch {
	case strings.HasPrefix(target, "tcp://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("workflow manager: tcp target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("workflow manager: tls target %q is missing host:port", raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("workflow manager: unix target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	case strings.Contains(target, "://"):
		parsed, parseErr := url.Parse(target)
		if parseErr != nil {
			return "", "", fmt.Errorf("workflow manager: parse target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("workflow manager: unsupported target scheme %q", parsed.Scheme)
	default:
		return "unix", filepath.Clean(target), nil
	}
}

func WorkflowManagerFromContext(ctx context.Context) (*WorkflowManagerClient, error) {
	return WorkflowManager(InvocationTokenFromContext(ctx))
}

func (c *WorkflowManagerClient) Close() error {
	return nil
}

func (c *WorkflowManagerClient) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CreateSchedule(ctx, value)
}

func (c *WorkflowManagerClient) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerGetScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerGetScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetSchedule(ctx, value)
}

func (c *WorkflowManagerClient) UpdateSchedule(ctx context.Context, req *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerUpdateScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerUpdateScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateSchedule(ctx, value)
}

func (c *WorkflowManagerClient) DeleteSchedule(ctx context.Context, req *proto.WorkflowManagerDeleteScheduleRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerDeleteScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerDeleteScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	_, err := c.client.DeleteSchedule(ctx, value)
	return err
}

func (c *WorkflowManagerClient) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPauseScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPauseScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PauseSchedule(ctx, value)
}

func (c *WorkflowManagerClient) ResumeSchedule(ctx context.Context, req *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerResumeScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerResumeScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ResumeSchedule(ctx, value)
}

func (c *WorkflowManagerClient) CreateTrigger(ctx context.Context, req *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CreateEventTrigger(ctx, value)
}

func (c *WorkflowManagerClient) GetTrigger(ctx context.Context, req *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerGetEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerGetEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetEventTrigger(ctx, value)
}

func (c *WorkflowManagerClient) UpdateTrigger(ctx context.Context, req *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerUpdateEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerUpdateEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateEventTrigger(ctx, value)
}

func (c *WorkflowManagerClient) DeleteTrigger(ctx context.Context, req *proto.WorkflowManagerDeleteEventTriggerRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerDeleteEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerDeleteEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	_, err := c.client.DeleteEventTrigger(ctx, value)
	return err
}

func (c *WorkflowManagerClient) PauseTrigger(ctx context.Context, req *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPauseEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPauseEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PauseEventTrigger(ctx, value)
}

func (c *WorkflowManagerClient) ResumeTrigger(ctx context.Context, req *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerResumeEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerResumeEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ResumeEventTrigger(ctx, value)
}

func (c *WorkflowManagerClient) PublishEvent(ctx context.Context, req *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPublishEventRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPublishEventRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PublishEvent(ctx, value)
}
