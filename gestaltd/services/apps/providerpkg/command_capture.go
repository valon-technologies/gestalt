package providerpkg

import (
	"bytes"
	"io"
)

const defaultCommandCaptureLimit = 64 << 10
const captureTruncatedNotice = "\n... (output truncated)\n"

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

func (c *boundedCapture) writeTo(sink io.Writer) {
	if c == nil || sink == nil || c.buf.Len() == 0 {
		return
	}
	_, _ = sink.Write(c.buf.Bytes())
	if c.truncated {
		_, _ = sink.Write([]byte(captureTruncatedNotice))
	}
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
	s.stdout.writeTo(s.sink)
	s.stderr.writeTo(s.sink)
}

func phaseCommandWriters(output CommandOutput) (stdout, stderr io.Writer, finalize func(error)) {
	if output.CaptureErrors != nil {
		session := newCommandCaptureSession(output.CaptureErrors, output.CaptureLimit)
		return session.stdout, session.stderr, func(err error) {
			if err != nil {
				session.flush()
			}
			flushCommandOutput(output.CaptureErrors)
		}
	}
	stdout, stderr = commandStdout(output), commandStderr(output)
	return stdout, stderr, func(error) {
		flushCommandOutput(stdout, stderr)
	}
}

func flushCommandOutput(writers ...io.Writer) {
	for _, writer := range writers {
		if flusher, ok := writer.(interface{ Flush() }); ok {
			flusher.Flush()
		}
	}
}
