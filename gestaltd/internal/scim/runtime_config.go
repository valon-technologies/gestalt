package scim

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

// ClientConfigFromData converts runtime storage's non-secret representation
// into the canonical server SCIM config shape.
func ClientConfigFromData(d coredata.SCIMClientData) config.SCIMClientConfig {
	out := config.SCIMClientConfig{
		AuthoritativeUserDomains: d.AuthoritativeUserDomains,
		ActiveUserRelationships:  relationshipsConfig(d.ActiveUserRelationships),
	}
	out.Credentials = make([]config.SCIMCredentialConfig, len(d.CredentialIDs))
	for i, id := range d.CredentialIDs {
		out.Credentials[i] = config.SCIMCredentialConfig{ID: id}
	}
	return out
}

// DataFromClientConfig converts config.yaml clients into runtime storage's
// non-secret representation.
func DataFromClientConfig(from config.SCIMClientConfig) coredata.SCIMClientData {
	return coredata.SCIMClientData{
		AuthoritativeUserDomains: from.AuthoritativeUserDomains,
		ActiveUserRelationships:  relationshipsData(from.ActiveUserRelationships),
	}
}

func relationshipsConfig(rels []coredata.SCIMRelationshipData) []config.SCIMRelationshipConfig {
	out := make([]config.SCIMRelationshipConfig, len(rels))
	for i, rel := range rels {
		out[i] = config.SCIMRelationshipConfig{
			Relation: rel.Relation,
			Resource: config.AuthorizationResourceDef{Type: rel.ResourceType, ID: rel.ResourceID},
		}
	}
	return out
}

func relationshipsData(rels []config.SCIMRelationshipConfig) []coredata.SCIMRelationshipData {
	out := make([]coredata.SCIMRelationshipData, len(rels))
	for i, rel := range rels {
		out[i] = coredata.SCIMRelationshipData{Relation: rel.Relation, ResourceType: rel.Resource.Type, ResourceID: rel.Resource.ID}
	}
	return out
}

// PostWriteConfig returns the validation snapshot after clientID is replaced
// by next. It leaves credential decryption to ResolveRuntimeConfig.
func PostWriteConfig(ctx context.Context, service *coredata.SCIMConfigService, clientID string, next *coredata.SCIMClientRecord) (config.ServerSCIMConfig, bool, error) {
	clients, err := service.List(ctx)
	if err != nil {
		return config.ServerSCIMConfig{}, false, err
	}
	out := config.ServerSCIMConfig{Clients: make(map[string]config.SCIMClientConfig, len(clients))}
	for _, client := range clients {
		if client.Enabled {
			out.Clients[client.ID] = ClientConfigFromData(client.SCIMClientData)
		}
	}
	delete(out.Clients, clientID)
	if next != nil && next.Enabled {
		out.Clients[next.ID] = ClientConfigFromData(next.SCIMClientData)
	}
	return out, len(out.Clients) > 0, nil
}

// ResolveRuntimeConfig decrypts credentials through the supplied callback and
// returns the live runtime SCIM config. Plaintext never leaves this
// function's caller through storage APIs.
func ResolveRuntimeConfig(ctx context.Context, service *coredata.SCIMConfigService, decrypt func(coredata.SCIMClientSecret) (string, error)) (config.ServerSCIMConfig, bool, error) {
	clients, err := service.List(ctx)
	if err != nil {
		return config.ServerSCIMConfig{}, false, err
	}
	out := config.ServerSCIMConfig{Clients: make(map[string]config.SCIMClientConfig, len(clients))}
	for _, client := range clients {
		if !client.Enabled {
			continue
		}
		secrets, err := service.Secrets(ctx, client.ID)
		if err != nil {
			return config.ServerSCIMConfig{}, false, err
		}
		populated := ClientConfigFromData(client.SCIMClientData)
		populated.Credentials = make([]config.SCIMCredentialConfig, 0, len(secrets))
		for i := range secrets {
			secret := &secrets[i]
			plaintext, err := decrypt(*secret)
			if err != nil {
				return config.ServerSCIMConfig{}, false, fmt.Errorf("decrypt SCIM credential %s/%s: %w", client.ID, secret.CredentialID, err)
			}
			if strings.TrimSpace(plaintext) == "" {
				return config.ServerSCIMConfig{}, false, fmt.Errorf("SCIM credential %s/%s decrypted to an empty token", client.ID, secret.CredentialID)
			}
			populated.Credentials = append(populated.Credentials, config.SCIMCredentialConfig{
				ID: secret.CredentialID, BearerToken: plaintext,
			})
		}
		if len(populated.Credentials) == 0 {
			return config.ServerSCIMConfig{}, false, fmt.Errorf("enabled SCIM client %s has no credentials", client.ID)
		}
		out.Clients[client.ID] = populated
	}
	return out, len(clients) > 0, nil
}
