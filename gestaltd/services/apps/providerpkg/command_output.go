package providerpkg

import (
	"io"
	"os"
)

type CommandOutput struct {
	Stdout io.Writer
	Stderr io.Writer
}

func commandStdout(output CommandOutput) io.Writer {
	if output.Stdout != nil {
		return output.Stdout
	}
	return os.Stdout
}

func commandStderr(output CommandOutput) io.Writer {
	if output.Stderr != nil {
		return output.Stderr
	}
	return os.Stderr
}
