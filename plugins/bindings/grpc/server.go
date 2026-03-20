package grpc

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	pb "github.com/valon-technologies/gestalt/proto/toolshed/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const userIDMetadataKey = "x-user-id"

type service struct {
	pb.UnimplementedToolshedServiceServer
	invoker        invocation.Invoker
	capLister      invocation.CapabilityLister
	providerLister invocation.ProviderLister
}

func registerService(srv *grpc.Server, invoker invocation.Invoker, capLister invocation.CapabilityLister, providerLister invocation.ProviderLister) {
	pb.RegisterToolshedServiceServer(srv, &service{
		invoker:        invoker,
		capLister:      capLister,
		providerLister: providerLister,
	})
}

func (s *service) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	if req.Provider == "" {
		return nil, status.Error(codes.InvalidArgument, "provider is required")
	}
	if req.Operation == "" {
		return nil, status.Error(codes.InvalidArgument, "operation is required")
	}

	userID := req.UserId
	if userID == "" {
		userID = userIDFromMetadata(ctx)
	}

	p := &principal.Principal{UserID: userID}
	ctx = principal.WithPrincipal(ctx, p)

	if req.RequestId != "" {
		ctx = invocation.ContextWithMeta(ctx, &invocation.InvocationMeta{
			RequestID: req.RequestId,
		})
	}

	params := structToMap(req.Params)

	result, err := s.invoker.Invoke(ctx, p, req.Provider, req.Operation, params)
	if err != nil {
		return nil, invokeErrorToStatus(err)
	}

	return &pb.InvokeResponse{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *service) ListCapabilities(_ context.Context, req *pb.ListCapabilitiesRequest) (*pb.ListCapabilitiesResponse, error) {
	if s.capLister == nil {
		return &pb.ListCapabilitiesResponse{}, nil
	}

	caps := s.capLister.ListCapabilities()

	filter := make(map[string]struct{}, len(req.Providers))
	for _, p := range req.Providers {
		filter[p] = struct{}{}
	}

	var pbCaps []*pb.Capability
	for _, cap := range caps {
		if len(filter) > 0 {
			if _, ok := filter[cap.Provider]; !ok {
				continue
			}
		}

		var pbParams []*pb.Parameter
		for _, param := range cap.Parameters {
			pbParams = append(pbParams, &pb.Parameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
			})
		}

		pbCaps = append(pbCaps, &pb.Capability{
			Provider:    cap.Provider,
			Operation:   cap.Operation,
			Description: cap.Description,
			Parameters:  pbParams,
		})
	}

	return &pb.ListCapabilitiesResponse{Capabilities: pbCaps}, nil
}

func (s *service) ListProviders(_ context.Context, _ *pb.ListProvidersRequest) (*pb.ListProvidersResponse, error) {
	if s.providerLister == nil {
		return &pb.ListProvidersResponse{}, nil
	}

	infos := s.providerLister.ListProviderInfos()

	pbProviders := make([]*pb.ProviderInfo, len(infos))
	for i, info := range infos {
		pbProviders[i] = &pb.ProviderInfo{
			Name:           info.Name,
			DisplayName:    info.DisplayName,
			Description:    info.Description,
			ConnectionMode: string(info.ConnectionMode),
		}
	}

	return &pb.ListProvidersResponse{Providers: pbProviders}, nil
}

func userIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get(userIDMetadataKey)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func invokeErrorToStatus(err error) error {
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, invocation.ErrOperationNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Errorf(codes.Unauthenticated, "%v", err)
	case errors.Is(err, invocation.ErrNoToken):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	default:
		return status.Errorf(codes.Internal, "upstream invocation failed")
	}
}
