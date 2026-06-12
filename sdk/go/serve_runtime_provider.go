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

// agentWorkspaceFromProto is still used by agent_conversions.go.
func agentWorkspaceFromProto(workspace *proto.AgentWorkspace) *AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &AgentWorkspace{
		Checkouts: make([]AgentWorkspaceGitCheckout, 0, len(workspace.GetCheckouts())),
		CWD:       workspace.GetCwd(),
	}
	for _, checkout := range workspace.GetCheckouts() {
		if checkout == nil {
			continue
		}
		out.Checkouts = append(out.Checkouts, AgentWorkspaceGitCheckout{
			URL:  checkout.GetUrl(),
			Ref:  checkout.GetRef(),
			Path: checkout.GetPath(),
		})
	}
	return out
}

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
		AppName:  req.GetAppName(),
		Template: req.GetTemplate(),
		Image:    req.GetImage(),
		Metadata: cloneStringMap(req.GetMetadata()),
	}
	if auth := req.GetImagePullAuth(); auth != nil {
		rootReq.ImagePullAuth = &RuntimeImagePullAuth{DockerConfigJSON: auth.DockerConfigJSON}
	}
	session, err := h.provider.StartSession(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("runtime start session", err)
	}
	return rootRuntimeSessionToClient(session), nil
}

func (h runtimeHandler) GetSession(ctx context.Context, req *client.GetRuntimeSessionRequest) (*client.RuntimeSession, error) {
	session, err := h.provider.GetSession(ctx, req.GetSessionID())
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
	if err := h.provider.StopSession(ctx, req.GetSessionID()); err != nil {
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
		SessionID:      req.GetSessionID(),
		AgentSessionID: req.GetAgentSessionID(),
		Workspace:      clientAgentWorkspaceToRoot(req.GetWorkspace()),
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
		SessionID:      req.GetSessionID(),
		AgentSessionID: req.GetAgentSessionID(),
	}
	if err := workspaceProvider.RemoveWorkspace(ctx, rootReq); err != nil {
		return providerRPCError("runtime remove workspace", err)
	}
	return nil
}

func (h runtimeHandler) StartApp(ctx context.Context, req *client.StartHostedAppRequest) (*client.HostedApp, error) {
	rootReq := StartHostedAppRequest{
		SessionID:     req.GetSessionID(),
		AppName:       req.GetAppName(),
		Command:       req.GetCommand(),
		Args:          append([]string(nil), req.GetArgs()...),
		Env:           cloneStringMap(req.GetEnv()),
		AllowedHosts:  append([]string(nil), req.GetAllowedHosts()...),
		DefaultAction: req.GetDefaultAction(),
		HostBinary:    req.GetHostBinary(),
		Workdir:       req.GetWorkdir(),
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
		pageSize = int(req.GetPageSize())
		pageToken = strings.TrimSpace(req.GetPageToken())
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
		Checkouts: make([]AgentWorkspaceGitCheckout, 0, len(workspace.GetCheckouts())),
		CWD:       workspace.GetCwd(),
	}
	for _, checkout := range workspace.GetCheckouts() {
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
