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
func PostWriteConfig(ctx context.Context, service *coredata.SCIMConfigService, clientID string, next *coredata.SCIMClientRecord) (config.ServerSCIMConfig, error) {
	clients, err := service.List(ctx)
	if err != nil {
		return config.ServerSCIMConfig{}, err
	}
	replaced := false
	for i, client := range clients {
		if client.ID == clientID {
			if next != nil {
				clients[i] = next
				replaced = true
			} else {
				clients = append(clients[:i], clients[i+1:]...)
			}
			break
		}
	}
	if next != nil && !replaced {
		clients = append(clients, next)
	}
	out, err := runtimeConfig(clients, credentialIDsFromRecord)
	if err != nil {
		return config.ServerSCIMConfig{}, err
	}
	return out, nil
}

func credentialIDsFromRecord(client *coredata.SCIMClientRecord) ([]config.SCIMCredentialConfig, error) {
	credentials := make([]config.SCIMCredentialConfig, len(client.CredentialIDs))
	for i, id := range client.CredentialIDs {
		credentials[i] = config.SCIMCredentialConfig{ID: id}
	}
	return credentials, nil
}

// runtimeConfig returns the enabled clients represented by service records.
// Credential values come from the supplied callback so pre-write validation
// can use candidate IDs while publication uses decrypted secrets.
func runtimeConfig(clients []*coredata.SCIMClientRecord, credentials func(*coredata.SCIMClientRecord) ([]config.SCIMCredentialConfig, error)) (config.ServerSCIMConfig, error) {
	out := config.ServerSCIMConfig{Clients: make(map[string]config.SCIMClientConfig, len(clients))}
	for _, client := range clients {
		if !client.Enabled {
			continue
		}
		populated := ClientConfigFromData(client.SCIMClientData)
		creds, err := credentials(client)
		if err != nil {
			return config.ServerSCIMConfig{}, err
		}
		populated.Credentials = creds
		out.Clients[client.ID] = populated
	}
	return out, nil
}

// ResolveRuntimeConfig decrypts credentials through the supplied callback and
// returns the live runtime SCIM config. Plaintext never leaves this
// function's caller through storage APIs.
func ResolveRuntimeConfig(ctx context.Context, service *coredata.SCIMConfigService, decrypt func(coredata.SCIMClientSecret) (string, error)) (config.ServerSCIMConfig, error) {
	clients, err := service.List(ctx)
	if err != nil {
		return config.ServerSCIMConfig{}, err
	}
	out, err := runtimeConfig(clients, func(client *coredata.SCIMClientRecord) ([]config.SCIMCredentialConfig, error) {
		secrets, err := service.Secrets(ctx, client.ID)
		if err != nil {
			return nil, err
		}
		credentials := make([]config.SCIMCredentialConfig, 0, len(secrets))
		for i := range secrets {
			secret := &secrets[i]
			plaintext, err := decrypt(*secret)
			if err != nil {
				return nil, fmt.Errorf("decrypt SCIM credential %s/%s: %w", client.ID, secret.CredentialID, err)
			}
			if strings.TrimSpace(plaintext) == "" {
				return nil, fmt.Errorf("SCIM credential %s/%s decrypted to an empty token", client.ID, secret.CredentialID)
			}
			credentials = append(credentials, config.SCIMCredentialConfig{ID: secret.CredentialID, BearerToken: plaintext})
		}
		if len(credentials) == 0 {
			return nil, fmt.Errorf("enabled SCIM client %s has no credentials", client.ID)
		}
		return credentials, nil
	})
	if err != nil {
		return config.ServerSCIMConfig{}, err
	}
	return out, nil
}
