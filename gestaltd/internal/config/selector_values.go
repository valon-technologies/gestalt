package config

import "github.com/valon-technologies/gestalt/server/core"

func SafeConnectionValue(value string) bool {
	return core.SafeConnectionValue(value)
}

func SafeInstanceValue(value string) bool {
	return core.SafeInstanceValue(value)
}
