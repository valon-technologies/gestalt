package invocation

import "fmt"

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
