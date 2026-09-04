package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type seedTrackingAuthorizationProvider struct {
	recordingAuthorizationProvider
	setModelCalls int
	addCalls      int
	emptyStore    bool
	populated     bool
}

func (p *seedTrackingAuthorizationProvider) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if p.emptyStore {
		return nil, status.Error(codes.NotFound, "no active model")
	}
	if p.populated {
		if req.GetPageToken() != "" {
			return &proto.ListActiveModelResourceTypesResponse{}, nil
		}
		return &proto.ListActiveModelResourceTypesResponse{
			ResourceTypes: []*proto.AuthorizationModelResourceType{{
				Name:        "runtime",
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			}},
		}, nil
	}
	return p.recordingAuthorizationProvider.ListActiveModelResourceTypes(ctx, req)
}

func (p *seedTrackingAuthorizationProvider) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if p.emptyStore {
		return &proto.ListRelationshipsResponse{}, nil
	}
	return p.recordingAuthorizationProvider.ListRelationships(ctx, req)
}

func (p *seedTrackingAuthorizationProvider) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	p.setModelCalls++
	return p.recordingAuthorizationProvider.SetActiveModel(ctx, req)
}

func (p *seedTrackingAuthorizationProvider) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	p.addCalls++
	return p.recordingAuthorizationProvider.AddRelationship(ctx, req)
}

func TestBootstrapAuthorizationSeedIfEmptyAppliesSeedFile(t *testing.T) {
	t.Parallel()

	seedPath := writeAuthorizationSeedFile(t, `{
  "model": {
    "resourceTypes": [
      {
        "name": "app",
        "sourceLayer": "SOURCE_LAYER_STATIC_CONFIG",
        "relations": [
          {
            "name": "admin",
            "allowedTargets": [{ "subjectType": "subject" }]
          }
        ]
      }
    ]
  },
  "relationships": [
    {
      "tuple": {
        "resource": { "type": "app", "id": "home" },
        "relation": "admin",
        "target": {
          "subject": { "type": "subject", "id": "user:alice" }
        }
      }
    }
  ]
}`)

	provider := &seedTrackingAuthorizationProvider{emptyStore: true}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))
	cfg.Authorization.SeedFile = seedPath
	cfg.Authorization.Relationships = nil

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		nil,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("SetAuthorizationState should not be called in plan-only mode")
	}
	if provider.setModelCalls != 1 {
		t.Fatalf("SetActiveModel calls = %d, want 1", provider.setModelCalls)
	}
	if provider.addCalls != 1 {
		t.Fatalf("AddRelationship calls = %d, want 1", provider.addCalls)
	}
}

func TestBootstrapAuthorizationSeedIfEmptyRunsWhenApplyEnabled(t *testing.T) {
	t.Parallel()

	seedPath := writeAuthorizationSeedFile(t, `{
  "model": {
    "resourceTypes": [
      {
        "name": "app",
        "sourceLayer": "SOURCE_LAYER_STATIC_CONFIG",
        "relations": [
          {
            "name": "admin",
            "allowedTargets": [{ "subjectType": "subject" }]
          }
        ]
      }
    ]
  }
}`)

	provider := &seedTrackingAuthorizationProvider{emptyStore: true}
	cfg := authorizationBootstrapTestConfig(boolPtr(true))
	cfg.Authorization.SeedFile = seedPath
	cfg.Authorization.Models = nil

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		nil,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setModelCalls != 1 {
		t.Fatalf("SetActiveModel calls = %d, want 1", provider.setModelCalls)
	}
}

func TestBootstrapAuthorizationSeedIfEmptySkipsPopulatedStore(t *testing.T) {
	t.Parallel()

	seedPath := writeAuthorizationSeedFile(t, `{
  "model": {
    "resourceTypes": [
      {
        "name": "app",
        "sourceLayer": "SOURCE_LAYER_STATIC_CONFIG"
      }
    ]
  }
}`)

	provider := &seedTrackingAuthorizationProvider{populated: true}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))
	cfg.Authorization.SeedFile = seedPath

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		nil,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setModelCalls != 0 {
		t.Fatalf("SetActiveModel calls = %d, want 0", provider.setModelCalls)
	}
}

func TestBootstrapAuthorizationSeedIfEmptyMergesConfiguredRelationships(t *testing.T) {
	t.Parallel()

	seedPath := writeAuthorizationSeedFile(t, `{
  "model": {
    "resourceTypes": [
      {
        "name": "github",
        "sourceLayer": "SOURCE_LAYER_STATIC_CONFIG",
        "relations": [
          {
            "name": "viewer",
            "allowedTargets": [{ "subjectType": "subject" }]
          }
        ]
      }
    ]
  }
}`)

	provider := &seedTrackingAuthorizationProvider{emptyStore: true}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))
	cfg.Authorization.SeedFile = seedPath

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		nil,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.addCalls != 1 {
		t.Fatalf("AddRelationship calls = %d, want 1 for configured relationship", provider.addCalls)
	}
}

func writeAuthorizationSeedFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "seed.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
