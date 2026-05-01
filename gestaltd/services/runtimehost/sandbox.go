package runtimehost

import "github.com/valon-technologies/gestalt/server/services/runtimehost/internal/sandbox"

// RunSandboxSubcommand runs the runtimehost sandbox helper command.
func RunSandboxSubcommand(args []string) error {
	return sandbox.RunSubcommand(args)
}
