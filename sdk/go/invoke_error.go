package gestalt

import "fmt"

// InvokeError describes a decoded app invocation failure.
type InvokeError struct {
	App       string
	Operation string
	Status    int
	Code      string
	Message   string
	Body      any
	RawBody   string
}

func (e *InvokeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	if e.Status > 0 {
		return fmt.Sprintf("app invoke failed with status %d", e.Status)
	}
	return "app invoke failed"
}
