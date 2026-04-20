package telemetryutil

import (
	"fmt"
	"strings"
)

type Protocol string

const (
	ProtocolGRPC Protocol = "grpc"
	ProtocolHTTP Protocol = "http"
)

func ParseProtocol(value string) (Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(ProtocolGRPC):
		return ProtocolGRPC, nil
	case string(ProtocolHTTP):
		return ProtocolHTTP, nil
	default:
		return "", fmt.Errorf("unknown protocol %q (expected %q or %q)", value, ProtocolGRPC, ProtocolHTTP)
	}
}

func (p *Protocol) UnmarshalText(text []byte) error {
	parsed, err := ParseProtocol(string(text))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

type LogExporter string

const (
	LogExporterOTLP   LogExporter = "otlp"
	LogExporterStdout LogExporter = "stdout"
)

func ParseLogExporter(value string) (LogExporter, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(LogExporterOTLP):
		return LogExporterOTLP, nil
	case string(LogExporterStdout):
		return LogExporterStdout, nil
	default:
		return "", fmt.Errorf("unknown logs exporter %q (expected %q or %q)", value, LogExporterOTLP, LogExporterStdout)
	}
}

func (e *LogExporter) UnmarshalText(text []byte) error {
	parsed, err := ParseLogExporter(string(text))
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

type LogFormat string

const (
	LogFormatText LogFormat = "text"
	LogFormatJSON LogFormat = "json"
)

func NormalizeLogFormat(value string) LogFormat {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(LogFormatJSON):
		return LogFormatJSON
	default:
		return LogFormatText
	}
}

func (f *LogFormat) UnmarshalText(text []byte) error {
	*f = NormalizeLogFormat(string(text))
	return nil
}
