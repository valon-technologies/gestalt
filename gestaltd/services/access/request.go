package access

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const ProviderAccessAction = "provider.access"

type scopeKind uint8

const (
	scopeProvider scopeKind = iota + 1
	scopeOperation
)

type Request struct {
	Subject   *proto.Subject
	Action    *proto.Action
	Resource  *proto.Resource
	scope     scopeKind
	scopeOnly bool
}

func AppOperation(provider, operation string) Request {
	provider = strings.TrimSpace(provider)
	operation = strings.TrimSpace(operation)
	return Request{
		Action:   &proto.Action{Name: operation},
		Resource: &proto.Resource{Type: provider, Id: provider},
		scope:    scopeOperation,
	}
}

func AppOperationPolicyOnly(provider, operation string) Request {
	provider = strings.TrimSpace(provider)
	operation = strings.TrimSpace(operation)
	return Request{
		Action:   &proto.Action{Name: operation},
		Resource: &proto.Resource{Type: provider, Id: provider},
	}
}

func Provider(provider string) Request {
	provider = strings.TrimSpace(provider)
	return Request{
		Action:   &proto.Action{Name: ProviderAccessAction},
		Resource: &proto.Resource{Type: provider, Id: provider},
		scope:    scopeProvider,
	}
}

func ProviderScope(provider string) Request {
	provider = strings.TrimSpace(provider)
	return Request{
		Resource:  &proto.Resource{Type: provider, Id: provider},
		scope:     scopeProvider,
		scopeOnly: true,
	}
}

func UIRole(policy, role string) Request {
	policy = strings.TrimSpace(policy)
	role = strings.TrimSpace(role)
	return Request{
		Action:   &proto.Action{Name: role},
		Resource: &proto.Resource{Type: policy, Id: policy},
	}
}

func AuthorizationMutation(method string) Request {
	method = strings.TrimSpace(method)
	return Request{
		Action:   &proto.Action{Name: method},
		Resource: &proto.Resource{Type: "AuthorizationProvider", Id: "authorization"},
	}
}

func WorkflowPlatform(action string) Request {
	action = strings.TrimSpace(action)
	return Request{
		Action:   &proto.Action{Name: action},
		Resource: &proto.Resource{Type: "gestalt", Id: "gestalt"},
	}
}
