package providerpkg

import (
	"bytes"
	"io"
)

const defaultCommandCaptureLimit = 64 << 10

type boundedCapture struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (c *boundedCapture) Write(p []byte) (int, error) {
	if c.limit <= 0 {
		return len(p), nil
	}
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		c.truncated = true
	}
	_, _ = c.buf.Write(p[:min(len(p), remaining)])
	return len(p), nil
}

type commandCaptureSession struct {
	stdout *boundedCapture
	stderr *boundedCapture
	sink   io.Writer
}

func newCommandCaptureSession(sink io.Writer, limit int) *commandCaptureSession {
	if limit <= 0 {
		limit = defaultCommandCaptureLimit
	}
	return &commandCaptureSession{
		stdout: &boundedCapture{limit: limit},
		stderr: &boundedCapture{limit: limit},
		sink:   sink,
	}
}

func (s *commandCaptureSession) flush() {
	if s == nil || s.sink == nil {
		return
	}
	if s.stdout.buf.Len() > 0 {
		_, _ = s.sink.Write(s.stdout.buf.Bytes())
	}
	if s.stderr.buf.Len() > 0 {
		_, _ = s.sink.Write(s.stderr.buf.Bytes())
	}
}

func phaseCommandWriters(output CommandOutput) (stdout, stderr io.Writer, finalize func(error)) {
	if output.CaptureErrors != nil {
		session := newCommandCaptureSession(output.CaptureErrors, output.CaptureLimit)
		return session.stdout, session.stderr, func(err error) {
			if err != nil {
				session.flush()
			}
		}
	}
	return commandStdout(output), commandStderr(output), func(error) {}
}
