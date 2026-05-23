package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMethodStreamingPatterns(t *testing.T) {
	t.Parallel()

	grpcRoot := filepath.Join("..", "..", "internal", "gen", "v1")

	tests := []struct {
		name     string
		grpcFile string
		service  string
		receiver string
		want     []string
	}{
		{
			name:     "s3 server streaming read",
			grpcFile: filepath.Join(grpcRoot, "s3_grpc.pb.go"),
			service:  "S3Server",
			receiver: "routingS3Server",
			want: []string{
				"func (s *routingS3Server) ReadObject(req *proto.ReadObjectRequest, stream grpc.ServerStreamingServer[proto.ReadObjectChunk]) error {",
				"server, err := s.server(stream.Context())",
				"return server.ReadObject(req, stream)",
			},
		},
		{
			name:     "s3 client streaming write",
			grpcFile: filepath.Join(grpcRoot, "s3_grpc.pb.go"),
			service:  "S3Server",
			receiver: "routingS3Server",
			want: []string{
				"func (s *routingS3Server) WriteObject(stream grpc.ClientStreamingServer[proto.WriteObjectRequest, proto.WriteObjectResponse]) error {",
				"return server.WriteObject(stream)",
			},
		},
		{
			name:     "indexeddb bidi cursor",
			grpcFile: filepath.Join(grpcRoot, "datastore_grpc.pb.go"),
			service:  "IndexedDBServer",
			receiver: "routingIndexedDBServer",
			want: []string{
				"func (s *routingIndexedDBServer) OpenCursor(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse]) error {",
				"return server.OpenCursor(stream)",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			methods, err := parseInterfaceMethods(tc.grpcFile, tc.service)
			if err != nil {
				t.Fatalf("parseInterfaceMethods: %v", err)
			}

			var buf bytes.Buffer
			for _, method := range methods {
				if !method.IsStreaming {
					continue
				}
				writeMethod(&buf, tc.receiver, qualifyMethod(method))
			}

			got := buf.String()
			for _, snippet := range tc.want {
				if !strings.Contains(got, snippet) {
					t.Fatalf("generated output missing %q\n\ngot:\n%s", snippet, got)
				}
			}
		})
	}
}
