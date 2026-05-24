package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServeAppRuntimeProvider starts a gRPC server for a [AppRuntimeProvider].
func ServeAppRuntimeProvider(ctx context.Context, provider AppRuntimeProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindRuntime, provider))
		proto.RegisterAppRuntimeProviderServer(srv, pluginRuntimeProviderServer{provider: provider})
	})
}

type pluginRuntimeProviderServer struct {
	proto.UnimplementedAppRuntimeProviderServer
	provider AppRuntimeProvider
}

func (s pluginRuntimeProviderServer) GetSupport(ctx context.Context, _ *emptypb.Empty) (*proto.AppRuntimeSupport, error) {
	support, err := s.provider.GetSupport(ctx)
	if err != nil {
		return nil, providerRPCError("runtime get support", err)
	}
	return pluginRuntimeSupportToProto(support), nil
}

func (s pluginRuntimeProviderServer) StartSession(ctx context.Context, req *proto.StartAppRuntimeSessionRequest) (*proto.AppRuntimeSession, error) {
	session, err := s.provider.StartSession(ctx, startAppRuntimeSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start session", err)
	}
	return pluginRuntimeSessionToProto(session), nil
}

func (s pluginRuntimeProviderServer) GetSession(ctx context.Context, req *proto.GetAppRuntimeSessionRequest) (*proto.AppRuntimeSession, error) {
	session, err := s.provider.GetSession(ctx, req.GetSessionId())
	if err != nil {
		return nil, providerRPCError("runtime get session", err)
	}
	return pluginRuntimeSessionToProto(session), nil
}

func (s pluginRuntimeProviderServer) ListSessions(ctx context.Context, _ *proto.ListAppRuntimeSessionsRequest) (*proto.ListAppRuntimeSessionsResponse, error) {
	sessions, err := s.provider.ListSessions(ctx)
	if err != nil {
		return nil, providerRPCError("runtime list sessions", err)
	}
	resp := &proto.ListAppRuntimeSessionsResponse{Sessions: make([]*proto.AppRuntimeSession, 0, len(sessions))}
	for _, session := range sessions {
		resp.Sessions = append(resp.Sessions, pluginRuntimeSessionToProto(session))
	}
	return resp, nil
}

func (s pluginRuntimeProviderServer) StopSession(ctx context.Context, req *proto.StopAppRuntimeSessionRequest) (*emptypb.Empty, error) {
	if err := s.provider.StopSession(ctx, req.GetSessionId()); err != nil {
		return nil, providerRPCError("runtime stop session", err)
	}
	return &emptypb.Empty{}, nil
}

func (s pluginRuntimeProviderServer) PrepareWorkspace(ctx context.Context, req *proto.PrepareAppRuntimeWorkspaceRequest) (*proto.PrepareAppRuntimeWorkspaceResponse, error) {
	workspaceProvider, ok := s.provider.(AppRuntimeWorkspaceProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "runtime prepare workspace is not implemented")
	}
	resp, err := workspaceProvider.PrepareWorkspace(ctx, prepareAppRuntimeWorkspaceRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime prepare workspace", err)
	}
	return prepareAppRuntimeWorkspaceResponseToProto(resp), nil
}

func (s pluginRuntimeProviderServer) RemoveWorkspace(ctx context.Context, req *proto.RemoveAppRuntimeWorkspaceRequest) (*emptypb.Empty, error) {
	workspaceProvider, ok := s.provider.(AppRuntimeWorkspaceProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "runtime remove workspace is not implemented")
	}
	if err := workspaceProvider.RemoveWorkspace(ctx, removeAppRuntimeWorkspaceRequestFromProto(req)); err != nil {
		return nil, providerRPCError("runtime remove workspace", err)
	}
	return &emptypb.Empty{}, nil
}

func (s pluginRuntimeProviderServer) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	hostedApp, err := s.provider.StartApp(ctx, startHostedAppRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start app", err)
	}
	return hostedAppToProto(hostedApp), nil
}

func pluginRuntimeSupportToProto(support AppRuntimeSupport) *proto.AppRuntimeSupport {
	return &proto.AppRuntimeSupport{
		CanHostApps:              support.CanHostApps,
		EgressMode:               pluginRuntimeEgressModeToProto(support.EgressMode),
		SupportsPrepareWorkspace: support.SupportsPrepareWorkspace,
	}
}

func pluginRuntimeEgressModeToProto(mode AppRuntimeEgressMode) proto.AppRuntimeEgressMode {
	switch mode {
	case AppRuntimeEgressModeNone:
		return proto.AppRuntimeEgressMode_APP_RUNTIME_EGRESS_MODE_NONE
	case AppRuntimeEgressModeCIDR:
		return proto.AppRuntimeEgressMode_APP_RUNTIME_EGRESS_MODE_CIDR
	case AppRuntimeEgressModeHostname:
		return proto.AppRuntimeEgressMode_APP_RUNTIME_EGRESS_MODE_HOSTNAME
	default:
		return proto.AppRuntimeEgressMode_APP_RUNTIME_EGRESS_MODE_UNSPECIFIED
	}
}

func pluginRuntimeSessionToProto(session AppRuntimeSession) *proto.AppRuntimeSession {
	return &proto.AppRuntimeSession{
		Id:           session.ID,
		State:        session.State,
		Metadata:     cloneStringMap(session.Metadata),
		Lifecycle:    pluginRuntimeLifecycleToProto(session.Lifecycle),
		StateReason:  session.StateReason,
		StateMessage: session.StateMessage,
	}
}

func pluginRuntimeLifecycleToProto(lifecycle AppRuntimeSessionLifecycle) *proto.AppRuntimeSessionLifecycle {
	out := &proto.AppRuntimeSessionLifecycle{}
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

func startAppRuntimeSessionRequestFromProto(req *proto.StartAppRuntimeSessionRequest) StartAppRuntimeSessionRequest {
	if req == nil {
		return StartAppRuntimeSessionRequest{}
	}
	var pullAuth *AppRuntimeImagePullAuth
	if auth := req.GetImagePullAuth(); auth != nil {
		pullAuth = &AppRuntimeImagePullAuth{DockerConfigJSON: auth.GetDockerConfigJson()}
	}
	return StartAppRuntimeSessionRequest{
		AppName:    req.GetAppName(),
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
		AppName:    req.GetAppName(),
		Command:       req.GetCommand(),
		Args:          append([]string(nil), req.GetArgs()...),
		Env:           cloneStringMap(req.GetEnv()),
		AllowedHosts:  append([]string(nil), req.GetAllowedHosts()...),
		DefaultAction: req.GetDefaultAction(),
		HostBinary:    req.GetHostBinary(),
		Workdir:       req.GetWorkdir(),
	}
}

func prepareAppRuntimeWorkspaceRequestFromProto(req *proto.PrepareAppRuntimeWorkspaceRequest) PrepareAppRuntimeWorkspaceRequest {
	if req == nil {
		return PrepareAppRuntimeWorkspaceRequest{}
	}
	return PrepareAppRuntimeWorkspaceRequest{
		SessionID:      req.GetSessionId(),
		AgentSessionID: req.GetAgentSessionId(),
		Workspace:      agentWorkspaceFromProto(req.GetWorkspace()),
	}
}

func removeAppRuntimeWorkspaceRequestFromProto(req *proto.RemoveAppRuntimeWorkspaceRequest) RemoveAppRuntimeWorkspaceRequest {
	if req == nil {
		return RemoveAppRuntimeWorkspaceRequest{}
	}
	return RemoveAppRuntimeWorkspaceRequest{
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

func prepareAppRuntimeWorkspaceResponseToProto(resp PrepareAppRuntimeWorkspaceResponse) *proto.PrepareAppRuntimeWorkspaceResponse {
	return &proto.PrepareAppRuntimeWorkspaceResponse{
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
		AppName: app.AppName,
		DialTarget: app.DialTarget,
	}
}
