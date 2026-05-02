package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServePluginRuntimeProvider starts a gRPC server for a [PluginRuntimeProvider].
func ServePluginRuntimeProvider(ctx context.Context, provider PluginRuntimeProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindRuntime, provider))
		proto.RegisterPluginRuntimeProviderServer(srv, pluginRuntimeProviderServer{provider: provider})
	})
}

type pluginRuntimeProviderServer struct {
	proto.UnimplementedPluginRuntimeProviderServer
	provider PluginRuntimeProvider
}

func (s pluginRuntimeProviderServer) GetSupport(ctx context.Context, _ *emptypb.Empty) (*proto.PluginRuntimeSupport, error) {
	support, err := s.provider.GetSupport(ctx)
	if err != nil {
		return nil, providerRPCError("runtime get support", err)
	}
	return pluginRuntimeSupportToProto(support), nil
}

func (s pluginRuntimeProviderServer) StartSession(ctx context.Context, req *proto.StartPluginRuntimeSessionRequest) (*proto.PluginRuntimeSession, error) {
	session, err := s.provider.StartSession(ctx, startPluginRuntimeSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start session", err)
	}
	return pluginRuntimeSessionToProto(session), nil
}

func (s pluginRuntimeProviderServer) GetSession(ctx context.Context, req *proto.GetPluginRuntimeSessionRequest) (*proto.PluginRuntimeSession, error) {
	session, err := s.provider.GetSession(ctx, req.GetSessionId())
	if err != nil {
		return nil, providerRPCError("runtime get session", err)
	}
	return pluginRuntimeSessionToProto(session), nil
}

func (s pluginRuntimeProviderServer) ListSessions(ctx context.Context, _ *proto.ListPluginRuntimeSessionsRequest) (*proto.ListPluginRuntimeSessionsResponse, error) {
	sessions, err := s.provider.ListSessions(ctx)
	if err != nil {
		return nil, providerRPCError("runtime list sessions", err)
	}
	resp := &proto.ListPluginRuntimeSessionsResponse{Sessions: make([]*proto.PluginRuntimeSession, 0, len(sessions))}
	for _, session := range sessions {
		resp.Sessions = append(resp.Sessions, pluginRuntimeSessionToProto(session))
	}
	return resp, nil
}

func (s pluginRuntimeProviderServer) StopSession(ctx context.Context, req *proto.StopPluginRuntimeSessionRequest) (*emptypb.Empty, error) {
	if err := s.provider.StopSession(ctx, req.GetSessionId()); err != nil {
		return nil, providerRPCError("runtime stop session", err)
	}
	return &emptypb.Empty{}, nil
}

func (s pluginRuntimeProviderServer) StartPlugin(ctx context.Context, req *proto.StartHostedPluginRequest) (*proto.HostedPlugin, error) {
	plugin, err := s.provider.StartPlugin(ctx, startHostedPluginRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("runtime start plugin", err)
	}
	return hostedPluginToProto(plugin), nil
}

func pluginRuntimeSupportToProto(support PluginRuntimeSupport) *proto.PluginRuntimeSupport {
	return &proto.PluginRuntimeSupport{
		CanHostPlugins: support.CanHostPlugins,
		EgressMode:     pluginRuntimeEgressModeToProto(support.EgressMode),
	}
}

func pluginRuntimeEgressModeToProto(mode PluginRuntimeEgressMode) proto.PluginRuntimeEgressMode {
	switch mode {
	case PluginRuntimeEgressModeNone:
		return proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_NONE
	case PluginRuntimeEgressModeCIDR:
		return proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_CIDR
	case PluginRuntimeEgressModeHostname:
		return proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME
	default:
		return proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED
	}
}

func pluginRuntimeSessionToProto(session PluginRuntimeSession) *proto.PluginRuntimeSession {
	return &proto.PluginRuntimeSession{
		Id:           session.ID,
		State:        session.State,
		Metadata:     cloneStringMap(session.Metadata),
		Lifecycle:    pluginRuntimeLifecycleToProto(session.Lifecycle),
		StateReason:  session.StateReason,
		StateMessage: session.StateMessage,
	}
}

func pluginRuntimeLifecycleToProto(lifecycle PluginRuntimeSessionLifecycle) *proto.PluginRuntimeSessionLifecycle {
	out := &proto.PluginRuntimeSessionLifecycle{}
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

func startPluginRuntimeSessionRequestFromProto(req *proto.StartPluginRuntimeSessionRequest) StartPluginRuntimeSessionRequest {
	if req == nil {
		return StartPluginRuntimeSessionRequest{}
	}
	var pullAuth *PluginRuntimeImagePullAuth
	if auth := req.GetImagePullAuth(); auth != nil {
		pullAuth = &PluginRuntimeImagePullAuth{DockerConfigJSON: auth.GetDockerConfigJson()}
	}
	return StartPluginRuntimeSessionRequest{
		PluginName:    req.GetPluginName(),
		Template:      req.GetTemplate(),
		Image:         req.GetImage(),
		Metadata:      cloneStringMap(req.GetMetadata()),
		ImagePullAuth: pullAuth,
	}
}

func startHostedPluginRequestFromProto(req *proto.StartHostedPluginRequest) StartHostedPluginRequest {
	if req == nil {
		return StartHostedPluginRequest{}
	}
	return StartHostedPluginRequest{
		SessionID:     req.GetSessionId(),
		PluginName:    req.GetPluginName(),
		Command:       req.GetCommand(),
		Args:          append([]string(nil), req.GetArgs()...),
		Env:           cloneStringMap(req.GetEnv()),
		AllowedHosts:  append([]string(nil), req.GetAllowedHosts()...),
		DefaultAction: req.GetDefaultAction(),
		HostBinary:    req.GetHostBinary(),
	}
}

func hostedPluginToProto(plugin HostedPlugin) *proto.HostedPlugin {
	return &proto.HostedPlugin{
		Id:         plugin.ID,
		SessionId:  plugin.SessionID,
		PluginName: plugin.PluginName,
		DialTarget: plugin.DialTarget,
	}
}
