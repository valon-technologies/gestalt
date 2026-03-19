package core

import "context"

type Capability struct {
	Provider    string
	Operation   string
	Description string
	Parameters  []Parameter
}

type InvocationRequest struct {
	Provider  string
	Operation string
	Params    map[string]any
	UserID    string
}

type Broker interface {
	Invoke(ctx context.Context, req InvocationRequest) (*OperationResult, error)
	ListCapabilities() []Capability
}
