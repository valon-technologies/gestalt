package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorizationstate"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/encoding/protojson"
)

func bootstrapAuthorizationSeedIfEmpty(
	ctx context.Context,
	providerName string,
	provider core.AuthorizationProvider,
	cfg config.AuthorizationConfig,
	users authorizationUserResolver,
) error {
	seedFile := strings.TrimSpace(cfg.SeedFile)
	if seedFile == "" {
		return nil
	}
	empty, err := authorizationProviderStoreIsEmpty(ctx, provider)
	if err != nil {
		return fmt.Errorf("check authorization store: %w", err)
	}
	if !empty {
		return nil
	}

	req, err := loadAuthorizationSeedFile(seedFile)
	if err != nil {
		return err
	}
	configRelationships, err := staticAuthorizationRelationships(ctx, cfg, users)
	if err != nil {
		return fmt.Errorf("resolve configured authorization relationships: %w", err)
	}
	req.Relationships = append(append([]*proto.Relationship(nil), req.GetRelationships()...), configRelationships...)
	if _, err := authorizationstate.Apply(ctx, provider, req); err != nil {
		return fmt.Errorf("apply authorization seed %q: %w", seedFile, err)
	}
	slog.InfoContext(ctx, "authorization state seeded: empty store loaded seed file",
		"provider", providerName,
		"seed_file", seedFile,
		"resource_type_count", len(req.GetModel().GetResourceTypes()),
		"relationship_count", len(req.GetRelationships()),
	)
	return nil
}

func authorizationProviderStoreIsEmpty(ctx context.Context, provider core.AuthorizationProvider) (bool, error) {
	resourceTypes, err := provider.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
		PageSize: 1,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, err
	}
	if len(resourceTypes.GetResourceTypes()) > 0 {
		return false, nil
	}
	relationships, err := provider.ListRelationships(ctx, &proto.ListRelationshipsRequest{
		PageSize: 1,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, err
	}
	return len(relationships.GetRelationships()) == 0, nil
}

func loadAuthorizationSeedFile(path string) (*proto.SetAuthorizationStateRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read authorization seed file %q: %w", path, err)
	}
	var req proto.SetAuthorizationStateRequest
	if err := protojson.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse authorization seed file %q: %w", path, err)
	}
	return gproto.Clone(&req).(*proto.SetAuthorizationStateRequest), nil
}
