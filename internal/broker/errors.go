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
