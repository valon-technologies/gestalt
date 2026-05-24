package gestalt

import (
	"context"
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServeRuntimeProvider starts a gRPC server for a [RuntimeProvider].
func ServeRuntimeProvider(ctx context.Context, provider RuntimeProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindRuntime, provider))
		proto.RegisterRuntimeProviderServer(srv, runtimeProviderServer{provider: provider})
	})
}

type runtimeProviderServer struct {
	proto.UnimplementedRuntimeProviderServer
	provider RuntimeProvider
}

const (
	defaultListRuntimeSessionsPageSize = 100
	maxListRuntimeSessionsPageSize     = 200
)

func (s runtimeProviderServer) GetSupport(ctx context.Context, _ *emptypb.Empty) (*proto.RuntimeSupport, error) {
	support, err := s.provider.GetSupport(ctx)
	if err != nil {
		return nil, providerRPCError("runtime get support", err)
	}
	return runtimeSupportToProto(support), nil
}

func (s runtimeProviderServer) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	session, err := s.provider.StartSession(ctx, startRuntimeSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start session", err)
	}
	return runtimeSessionToProto(session), nil
}

func (s runtimeProviderServer) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	session, err := s.provider.GetSession(ctx, req.GetSessionId())
	if err != nil {
		return nil, providerRPCError("runtime get session", err)
	}
	return runtimeSessionToProto(session), nil
}

func (s runtimeProviderServer) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	request, err := listRuntimeSessionsRequestFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	response, err := s.provider.ListSessions(ctx, request)
	if err != nil {
		return nil, providerRPCError("runtime list sessions", err)
	}
	resp := &proto.ListRuntimeSessionsResponse{
		Sessions:      make([]*proto.RuntimeSession, 0, len(response.Sessions)),
		NextPageToken: response.NextPageToken,
	}
	for _, session := range response.Sessions {
		resp.Sessions = append(resp.Sessions, runtimeSessionToProto(session))
	}
	return resp, nil
}

func (s runtimeProviderServer) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) (*emptypb.Empty, error) {
	if err := s.provider.StopSession(ctx, req.GetSessionId()); err != nil {
		return nil, providerRPCError("runtime stop session", err)
	}
	return &emptypb.Empty{}, nil
}

func (s runtimeProviderServer) PrepareWorkspace(ctx context.Context, req *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
	workspaceProvider, ok := s.provider.(RuntimeWorkspaceProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "runtime prepare workspace is not implemented")
	}
	resp, err := workspaceProvider.PrepareWorkspace(ctx, prepareRuntimeWorkspaceRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime prepare workspace", err)
	}
	return prepareRuntimeWorkspaceResponseToProto(resp), nil
}

func (s runtimeProviderServer) RemoveWorkspace(ctx context.Context, req *proto.RemoveRuntimeWorkspaceRequest) (*emptypb.Empty, error) {
	workspaceProvider, ok := s.provider.(RuntimeWorkspaceProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "runtime remove workspace is not implemented")
	}
	if err := workspaceProvider.RemoveWorkspace(ctx, removeRuntimeWorkspaceRequestFromProto(req)); err != nil {
		return nil, providerRPCError("runtime remove workspace", err)
	}
	return &emptypb.Empty{}, nil
}

func (s runtimeProviderServer) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	hostedApp, err := s.provider.StartApp(ctx, startHostedAppRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start app", err)
	}
	return hostedAppToProto(hostedApp), nil
}

func runtimeSupportToProto(support RuntimeSupport) *proto.RuntimeSupport {
	return &proto.RuntimeSupport{
		CanHostApps:              support.CanHostApps,
		EgressMode:               runtimeEgressModeToProto(support.EgressMode),
		SupportsPrepareWorkspace: support.SupportsPrepareWorkspace,
	}
}

func runtimeEgressModeToProto(mode RuntimeEgressMode) proto.RuntimeEgressMode {
	switch mode {
	case RuntimeEgressModeNone:
		return proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_NONE
	case RuntimeEgressModeCIDR:
		return proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_CIDR
	case RuntimeEgressModeHostname:
		return proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	default:
		return proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_UNSPECIFIED
	}
}

func runtimeSessionToProto(session RuntimeSession) *proto.RuntimeSession {
	return &proto.RuntimeSession{
		Id:           session.ID,
		State:        session.State,
		Metadata:     cloneStringMap(session.Metadata),
		Lifecycle:    runtimeLifecycleToProto(session.Lifecycle),
		StateReason:  session.StateReason,
		StateMessage: session.StateMessage,
	}
}

func runtimeLifecycleToProto(lifecycle RuntimeSessionLifecycle) *proto.RuntimeSessionLifecycle {
	out := &proto.RuntimeSessionLifecycle{}
	if lifecycle.StartedAt != nil {
		out.StartedAt = timestamppb.New(*lifecycle.StartedAt)
	}
	if lifecycle.RecommendedDrainAt != nil {
		out.RecommendedDrainAt = timestamppb.New(*lifecycle.RecommendedDrainAt)
	}
	if lifecycle.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*lifecycle.ExpiresAt)
	}
	return out
}

func startRuntimeSessionRequestFromProto(req *proto.StartRuntimeSessionRequest) StartRuntimeSessionRequest {
	if req == nil {
		return StartRuntimeSessionRequest{}
	}
	var pullAuth *RuntimeImagePullAuth
	if auth := req.GetImagePullAuth(); auth != nil {
		pullAuth = &RuntimeImagePullAuth{DockerConfigJSON: auth.GetDockerConfigJson()}
	}
	return StartRuntimeSessionRequest{
		AppName:       req.GetAppName(),
		Template:      req.GetTemplate(),
		Image:         req.GetImage(),
		Metadata:      cloneStringMap(req.GetMetadata()),
		ImagePullAuth: pullAuth,
	}
}

func startHostedAppRequestFromProto(req *proto.StartHostedAppRequest) StartHostedAppRequest {
	if req == nil {
		return StartHostedAppRequest{}
	}
	return StartHostedAppRequest{
		SessionID:     req.GetSessionId(),
		AppName:       req.GetAppName(),
		Command:       req.GetCommand(),
		Args:          append([]string(nil), req.GetArgs()...),
		Env:           cloneStringMap(req.GetEnv()),
		AllowedHosts:  append([]string(nil), req.GetAllowedHosts()...),
		DefaultAction: req.GetDefaultAction(),
		HostBinary:    req.GetHostBinary(),
		Workdir:       req.GetWorkdir(),
	}
}

func listRuntimeSessionsRequestFromProto(req *proto.ListRuntimeSessionsRequest) (ListRuntimeSessionsRequest, error) {
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
		return ListRuntimeSessionsRequest{
			PageToken: pageToken,
		}, nil
	}
	if pageSize == 0 {
		pageSize = defaultListRuntimeSessionsPageSize
	}
	if pageSize > maxListRuntimeSessionsPageSize {
		pageSize = maxListRuntimeSessionsPageSize
	}
	return ListRuntimeSessionsRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
	}, nil
}

func prepareRuntimeWorkspaceRequestFromProto(req *proto.PrepareRuntimeWorkspaceRequest) PrepareRuntimeWorkspaceRequest {
	if req == nil {
		return PrepareRuntimeWorkspaceRequest{}
	}
	return PrepareRuntimeWorkspaceRequest{
		SessionID:      req.GetSessionId(),
		AgentSessionID: req.GetAgentSessionId(),
		Workspace:      agentWorkspaceFromProto(req.GetWorkspace()),
	}
}

func removeRuntimeWorkspaceRequestFromProto(req *proto.RemoveRuntimeWorkspaceRequest) RemoveRuntimeWorkspaceRequest {
	if req == nil {
		return RemoveRuntimeWorkspaceRequest{}
	}
	return RemoveRuntimeWorkspaceRequest{
		SessionID:      req.GetSessionId(),
		AgentSessionID: req.GetAgentSessionId(),
	}
}

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

func agentWorkspaceToProto(workspace *AgentWorkspace) *proto.AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &proto.AgentWorkspace{
		Checkouts: make([]*proto.AgentWorkspaceGitCheckout, 0, len(workspace.Checkouts)),
		Cwd:       workspace.CWD,
	}
	for i := range workspace.Checkouts {
		checkout := workspace.Checkouts[i]
		out.Checkouts = append(out.Checkouts, &proto.AgentWorkspaceGitCheckout{
			Url:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func prepareRuntimeWorkspaceResponseToProto(resp PrepareRuntimeWorkspaceResponse) *proto.PrepareRuntimeWorkspaceResponse {
	return &proto.PrepareRuntimeWorkspaceResponse{
		Workspace: preparedAgentWorkspaceToProto(resp.Workspace),
	}
}

func preparedAgentWorkspaceToProto(workspace *PreparedAgentWorkspace) *proto.PreparedAgentWorkspace {
	if workspace == nil {
		return nil
	}
	return &proto.PreparedAgentWorkspace{
		Root: workspace.Root,
		Cwd:  workspace.CWD,
	}
}

func hostedAppToProto(app HostedApp) *proto.HostedApp {
	return &proto.HostedApp{
		Id:         app.ID,
		SessionId:  app.SessionID,
		AppName:    app.AppName,
		DialTarget: app.DialTarget,
	}
}
