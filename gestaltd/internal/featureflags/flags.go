package featureflags

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Flag declares a feature flag and the value to use when its backing object is
// absent. Flags are intentionally immutable so a Snapshot can be shared across
// the process after startup.
type Flag struct {
	name         string
	defaultValue bool
}

// Declare creates a feature flag whose missing-object behavior is explicit at
// the declaration site.
func Declare(name string, defaultValue bool) Flag {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("feature flag name is required")
	}
	if strings.Contains(name, "/") {
		panic("feature flag name must not include path segments")
	}
	return Flag{name: name, defaultValue: defaultValue}
}

func (f Flag) Name() string {
	return f.name
}

func (f Flag) Default() bool {
	return f.defaultValue
}

var (
	Agent    = Declare("agent", false)
	Workflow = Declare("workflow", false)

	declared = []Flag{Agent, Workflow}
)

// Snapshot is the immutable set of feature-flag values resolved at startup.
// Its zero value uses every flag's declared default.
type Snapshot struct {
	values map[Flag]bool
}

func Defaults() Snapshot {
	return Snapshot{}
}

func NewSnapshot(values map[Flag]bool) Snapshot {
	if len(values) == 0 {
		return Defaults()
	}
	cloned := make(map[Flag]bool, len(values))
	for flag, value := range values {
		cloned[flag] = value
	}
	return Snapshot{values: cloned}
}

// AllEnabled returns a snapshot used by compatibility helpers and validation
// paths that need to exercise every configured provider without reading GCS.
func AllEnabled() Snapshot {
	values := make(map[Flag]bool, len(declared))
	for _, flag := range declared {
		values[flag] = true
	}
	return NewSnapshot(values)
}

func (s Snapshot) Enabled(flag Flag) bool {
	if value, ok := s.values[flag]; ok {
		return value
	}
	return flag.Default()
}

func (s Snapshot) Values() map[string]bool {
	values := make(map[string]bool, len(declared))
	for _, flag := range declared {
		values[flag.Name()] = s.Enabled(flag)
	}
	return values
}

var ErrDisabled = errors.New("feature is not enabled")

type DisabledError struct {
	Flag Flag
}

func (e DisabledError) Error() string {
	return fmt.Sprintf("%s feature is not enabled", e.Flag.Name())
}

func (e DisabledError) Unwrap() error {
	return ErrDisabled
}

func (e DisabledError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

func NewDisabledError(flag Flag) error {
	return DisabledError{Flag: flag}
}

func IsDisabled(err error, flag Flag) bool {
	var disabled DisabledError
	return errors.As(err, &disabled) && disabled.Flag == flag
}
