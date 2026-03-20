package broker

import "fmt"

type ProviderNotFoundError struct {
	Name string
}

func (e *ProviderNotFoundError) Error() string {
	return fmt.Sprintf("provider %q not found", e.Name)
}

type OperationNotFoundError struct {
	Provider  string
	Operation string
}

func (e *OperationNotFoundError) Error() string {
	return fmt.Sprintf("operation %q not found on provider %q", e.Operation, e.Provider)
}

type NoCredentialError struct {
	Provider string
}

func (e *NoCredentialError) Error() string {
	return fmt.Sprintf("no credential stored for provider %q; connect via OAuth first", e.Provider)
}

type MaxDepthError struct {
	Depth int
	Max   int
}

func (e *MaxDepthError) Error() string {
	return fmt.Sprintf("invocation depth %d exceeds maximum %d", e.Depth, e.Max)
}

type RecursionError struct {
	Provider  string
	Operation string
}

func (e *RecursionError) Error() string {
	return fmt.Sprintf("recursive call detected: %s/%s already in call chain", e.Provider, e.Operation)
}

type RateLimitError struct {
	Provider string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded for provider %q", e.Provider)
}
