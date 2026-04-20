package telemetryutil

import "testing"

func TestParseProtocol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    Protocol
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "grpc", input: "grpc", want: ProtocolGRPC},
		{name: "http uppercase", input: "HTTP", want: ProtocolHTTP},
		{name: "invalid", input: "ftp", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseProtocol(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ParseProtocol() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProtocol() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseProtocol() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseLogExporter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    LogExporter
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "otlp", input: "otlp", want: LogExporterOTLP},
		{name: "stdout uppercase", input: "STDOUT", want: LogExporterStdout},
		{name: "invalid", input: "file", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseLogExporter(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ParseLogExporter() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLogExporter() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseLogExporter() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeLogFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  LogFormat
	}{
		{name: "empty defaults to text", input: "", want: LogFormatText},
		{name: "json", input: "json", want: LogFormatJSON},
		{name: "json uppercase", input: "JSON", want: LogFormatJSON},
		{name: "unknown falls back to text", input: "pretty", want: LogFormatText},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeLogFormat(tc.input); got != tc.want {
				t.Fatalf("NormalizeLogFormat() = %q, want %q", got, tc.want)
			}
		})
	}
}
