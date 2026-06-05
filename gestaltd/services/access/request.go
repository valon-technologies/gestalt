package access

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type CredentialScope uint8

const (
	ProviderCredentialScope CredentialScope = iota + 1
	OperationCredentialScope
)

type Request struct {
	ResourceType    string
	ResourceID      string
	Action          string
	CredentialScope CredentialScope
	ScopeOnly       bool
}

func (r Request) resource() *proto.Resource {
	resourceType := strings.TrimSpace(r.ResourceType)
	resourceID := strings.TrimSpace(r.ResourceID)
	if resourceID == "" {
		resourceID = resourceType
	}
	return &proto.Resource{Type: resourceType, Id: resourceID}
}

func (r Request) action() *proto.Action {
	return &proto.Action{Name: strings.TrimSpace(r.Action)}
}
