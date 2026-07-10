package invocation

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func AuthorizationResource(name string, kinds map[string]ProviderKind) *proto.Resource {
	name = strings.TrimSpace(name)
	if kind, ok := kinds[name]; ok {
		return &proto.Resource{Type: string(kind), Id: name}
	}
	return &proto.Resource{Type: name, Id: name}
}
