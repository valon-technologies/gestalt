package providerpkg

import (
	"io"
	"os"
)

type CommandOutput struct {
	Stdout io.Writer
	Stderr io.Writer
	// CaptureErrors captures bounded child stdout/stderr and writes the capture
	// to this writer only when the command fails. Successful commands discard it.
	CaptureErrors io.Writer
	// CaptureLimit is the per-stream byte cap when CaptureErrors is set. Zero uses
	// defaultCommandCaptureLimit.
	CaptureLimit int
}

func CommandOutputCaptureOnFailure(sink io.Writer) CommandOutput {
	return CommandOutput{CaptureErrors: sink}
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
