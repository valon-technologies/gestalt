package core

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyRegistered = errors.New("already registered")
	ErrMCPOnly           = errors.New("this integration is accessible only via MCP")
)
