package appregistryremote

import (
	"os/exec"
	"strings"
)

func runShellCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if detail != "" {
				return "", &commandError{name: name, detail: detail, err: err}
			}
		}
		return "", err
	}
	return string(out), nil
}

type commandError struct {
	name   string
	detail string
	err    error
}

func (e *commandError) Error() string {
	if e == nil {
		return "command failed"
	}
	if e.detail != "" {
		return e.name + ": " + e.detail
	}
	return e.name + ": " + e.err.Error()
}

func (e *commandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}
