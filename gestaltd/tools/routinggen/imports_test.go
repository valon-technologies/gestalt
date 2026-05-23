package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		methods []methodSpec
		want    bool
	}{
		{
			name: "unary only",
			methods: []methodSpec{
				{Name: "Get", Params: "context.Context, *GetRequest", IsStreaming: false},
			},
			want: true,
		},
		{
			name: "streaming only",
			methods: []methodSpec{
				{Name: "Write", Params: "grpc.ClientStreamingServer[WriteRequest, WriteResponse]", IsStreaming: true},
			},
			want: false,
		},
		{
			name: "mixed",
			methods: []methodSpec{
				{Name: "Get", Params: "context.Context, *GetRequest", IsStreaming: false},
				{Name: "Write", Params: "grpc.ClientStreamingServer[WriteRequest, WriteResponse]", IsStreaming: true},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := needsContext(tc.methods); got != tc.want {
				t.Fatalf("needsContext() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteImportsStreamingOnlyService(t *testing.T) {
	t.Parallel()

	grpcFile := filepath.Join("..", "..", "internal", "gen", "v1", "s3_grpc.pb.go")
	methods, err := parseInterfaceMethods(grpcFile, "S3Server")
	if err != nil {
		t.Fatalf("parseInterfaceMethods: %v", err)
	}

	streamingOnly := make([]methodSpec, 0, len(methods))
	for _, method := range methods {
		if method.IsStreaming {
			streamingOnly = append(streamingOnly, method)
		}
	}
	if len(streamingOnly) == 0 {
		t.Fatal("expected S3Server to expose streaming methods")
	}

	var buf bytes.Buffer
	writeImports(&buf, streamingOnly)
	got := buf.String()

	if strings.Contains(got, `"context"`) {
		t.Fatalf("streaming-only imports must not include context\n\ngot:\n%s", got)
	}
	if !strings.Contains(got, `"google.golang.org/grpc"`) {
		t.Fatalf("streaming-only imports must include grpc\n\ngot:\n%s", got)
	}
}

func TestWriteImportsMixedService(t *testing.T) {
	t.Parallel()

	grpcFile := filepath.Join("..", "..", "internal", "gen", "v1", "s3_grpc.pb.go")
	methods, err := parseInterfaceMethods(grpcFile, "S3Server")
	if err != nil {
		t.Fatalf("parseInterfaceMethods: %v", err)
	}

	var buf bytes.Buffer
	writeImports(&buf, methods)
	got := buf.String()

	if !strings.Contains(got, `"context"`) {
		t.Fatalf("mixed imports must include context\n\ngot:\n%s", got)
	}
}
