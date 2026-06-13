package gestalt

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServeRuntimeProvider starts a gRPC server for a [RuntimeProvider].
func ServeRuntimeProvider(ctx context.Context, provider RuntimeProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindRuntime, provider))
		proto.RegisterRuntimeServer(srv, client.NewRuntimeProviderServer(runtimeHandler{provider: provider}))
	})
}

// runtimeHandler bridges the ergonomic [RuntimeProvider] facade onto the
// generated transport handler; wire conversion lives in the generated adapter.
// providerRPCError preserves root sentinel-error mapping.
type runtimeHandler struct {
	client.UnimplementedRuntimeProvider
	provider RuntimeProvider
}

const (
	defaultListRuntimeSessionsPageSize = 100
	maxListRuntimeSessionsPageSize     = 200
)

func (h runtimeHandler) GetSupport(ctx context.Context) (*client.RuntimeSupport, error) {
	support, err := h.provider.GetSupport(ctx)
	if err != nil {
		return nil, providerRPCError("runtime get support", err)
	}
	return &client.RuntimeSupport{
		CanHostApps:              support.CanHostApps,
		EgressMode:               rootRuntimeEgressModeToClient(support.EgressMode),
		SupportsPrepareWorkspace: support.SupportsPrepareWorkspace,
	}, nil
}

func (h runtimeHandler) StartSession(ctx context.Context, req *client.StartRuntimeSessionRequest) (*client.RuntimeSession, error) {
	rootReq := StartRuntimeSessionRequest{
		AppName:  req.AppName,
		Template: req.Template,
		Image:    req.Image,
		Metadata: cloneStringMap(req.Metadata),
	}
	if auth := req.ImagePullAuth; auth != nil {
		rootReq.ImagePullAuth = &RuntimeImagePullAuth{DockerConfigJSON: auth.DockerConfigJSON}
	}
	session, err := h.provider.StartSession(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("runtime start session", err)
	}
	return rootRuntimeSessionToClient(session), nil
}

func (h runtimeHandler) GetSession(ctx context.Context, req *client.GetRuntimeSessionRequest) (*client.RuntimeSession, error) {
	session, err := h.provider.GetSession(ctx, req.SessionID)
	if err != nil {
		return nil, providerRPCError("runtime get session", err)
	}
	return rootRuntimeSessionToClient(session), nil
}

func (h runtimeHandler) ListSessions(ctx context.Context, req *client.ListRuntimeSessionsRequest) (*client.ListRuntimeSessionsResponse, error) {
	rootReq, err := clientListRuntimeSessionsRequestToRoot(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	response, err := h.provider.ListSessions(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("runtime list sessions", err)
	}
	resp := &client.ListRuntimeSessionsResponse{
		Sessions:      make([]*client.RuntimeSession, 0, len(response.Sessions)),
		NextPageToken: response.NextPageToken,
	}
	for _, session := range response.Sessions {
		resp.Sessions = append(resp.Sessions, rootRuntimeSessionToClient(session))
	}
	return resp, nil
}

func (h runtimeHandler) StopSession(ctx context.Context, req *client.StopRuntimeSessionRequest) error {
	if err := h.provider.StopSession(ctx, req.SessionID); err != nil {
		return providerRPCError("runtime stop session", err)
	}
	return nil
}

func (h runtimeHandler) PrepareWorkspace(ctx context.Context, req *client.PrepareRuntimeWorkspaceRequest) (*client.PrepareRuntimeWorkspaceResponse, error) {
	workspaceProvider, ok := h.provider.(RuntimeWorkspaceProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "runtime prepare workspace is not implemented")
	}
	rootReq := PrepareRuntimeWorkspaceRequest{
		SessionID:      req.SessionID,
		AgentSessionID: req.AgentSessionID,
		Workspace:      clientAgentWorkspaceToRoot(req.Workspace),
	}
	resp, err := workspaceProvider.PrepareWorkspace(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("runtime prepare workspace", err)
	}
	return rootPrepareRuntimeWorkspaceResponseToClient(resp), nil
}

func (h runtimeHandler) RemoveWorkspace(ctx context.Context, req *client.RemoveRuntimeWorkspaceRequest) error {
	workspaceProvider, ok := h.provider.(RuntimeWorkspaceProvider)
	if !ok {
		return status.Error(codes.Unimplemented, "runtime remove workspace is not implemented")
	}
	rootReq := RemoveRuntimeWorkspaceRequest{
		SessionID:      req.SessionID,
		AgentSessionID: req.AgentSessionID,
	}
	if err := workspaceProvider.RemoveWorkspace(ctx, rootReq); err != nil {
		return providerRPCError("runtime remove workspace", err)
	}
	return nil
}

func (h runtimeHandler) StartApp(ctx context.Context, req *client.StartHostedAppRequest) (*client.HostedApp, error) {
	rootReq := StartHostedAppRequest{
		SessionID:     req.SessionID,
		AppName:       req.AppName,
		Command:       req.Command,
		Args:          append([]string(nil), req.Args...),
		Env:           cloneStringMap(req.Env),
		AllowedHosts:  append([]string(nil), req.AllowedHosts...),
		DefaultAction: req.DefaultAction,
		HostBinary:    req.HostBinary,
		Workdir:       req.Workdir,
	}
	hostedApp, err := h.provider.StartApp(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("runtime start app", err)
	}
	return &client.HostedApp{
		ID:         hostedApp.ID,
		SessionID:  hostedApp.SessionID,
		AppName:    hostedApp.AppName,
		DialTarget: hostedApp.DialTarget,
	}, nil
}

func rootRuntimeSessionToClient(session RuntimeSession) *client.RuntimeSession {
	lc := &client.RuntimeSessionLifecycle{
		StartedAt:          session.Lifecycle.StartedAt,
		RecommendedDrainAt: session.Lifecycle.RecommendedDrainAt,
		ExpiresAt:          session.Lifecycle.ExpiresAt,
	}
	return &client.RuntimeSession{
		ID:           session.ID,
		State:        session.State,
		Metadata:     cloneStringMap(session.Metadata),
		Lifecycle:    lc,
		StateReason:  session.StateReason,
		StateMessage: session.StateMessage,
	}
}

func clientListRuntimeSessionsRequestToRoot(req *client.ListRuntimeSessionsRequest) (ListRuntimeSessionsRequest, error) {
	pageSize := 0
	pageToken := ""
	if req != nil {
		pageSize = int(req.PageSize)
		pageToken = strings.TrimSpace(req.PageToken)
	}
	if pageSize < 0 {
		return ListRuntimeSessionsRequest{}, fmt.Errorf("page_size must be non-negative")
	}
	if pageSize == 0 && pageToken != "" {
		return ListRuntimeSessionsRequest{PageToken: pageToken}, nil
	}
	if pageSize == 0 {
		pageSize = defaultListRuntimeSessionsPageSize
	}
	if pageSize > maxListRuntimeSessionsPageSize {
		pageSize = maxListRuntimeSessionsPageSize
	}
	return ListRuntimeSessionsRequest{PageSize: pageSize, PageToken: pageToken}, nil
}

func clientAgentWorkspaceToRoot(workspace *client.AgentWorkspace) *AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &AgentWorkspace{
		Checkouts: make([]AgentWorkspaceGitCheckout, 0, len(workspace.Checkouts)),
		CWD:       workspace.Cwd,
	}
	for _, checkout := range workspace.Checkouts {
		if checkout == nil {
			continue
		}
		out.Checkouts = append(out.Checkouts, AgentWorkspaceGitCheckout{
			URL:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func rootPrepareRuntimeWorkspaceResponseToClient(resp PrepareRuntimeWorkspaceResponse) *client.PrepareRuntimeWorkspaceResponse {
	out := &client.PrepareRuntimeWorkspaceResponse{}
	if resp.Workspace != nil {
		out.Workspace = &client.PreparedAgentWorkspace{
			Root: resp.Workspace.Root,
			Cwd:  resp.Workspace.CWD,
		}
	}
	return out
}

func rootRuntimeEgressModeToClient(mode RuntimeEgressMode) client.RuntimeEgressMode {
	switch mode {
	case RuntimeEgressModeNone:
		return client.RuntimeEgressModeNone
	case RuntimeEgressModeCIDR:
		return client.RuntimeEgressModeCidr
	case RuntimeEgressModeHostname:
		return client.RuntimeEgressModeHostname
	default:
		return client.RuntimeEgressModeUnspecified
	}
}
