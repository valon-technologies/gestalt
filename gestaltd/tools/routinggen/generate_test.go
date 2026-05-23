package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateGolden(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..")
	cases := []struct {
		name     string
		grpcFile string
		service  string
		receiver string
		pkg      string
	}{
		{
			name:     "cache",
			grpcFile: filepath.Join(repoRoot, "internal/gen/v1/cache_grpc.pb.go"),
			service:  "CacheServer",
			receiver: "routingCacheServer",
			pkg:      "cache",
		},
		{
			name:     "s3",
			grpcFile: filepath.Join(repoRoot, "internal/gen/v1/s3_grpc.pb.go"),
			service:  "S3Server",
			receiver: "routingS3Server",
			pkg:      "s3",
		},
		{
			name:     "indexeddb",
			grpcFile: filepath.Join(repoRoot, "internal/gen/v1/datastore_grpc.pb.go"),
			service:  "IndexedDBServer",
			receiver: "routingIndexedDBServer",
			pkg:      "indexeddb",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			methods, err := parseInterfaceMethods(tc.grpcFile, tc.service)
			if err != nil {
				t.Fatalf("parseInterfaceMethods: %v", err)
			}

			got, err := generate(config{
				Package:  tc.pkg,
				Receiver: tc.receiver,
			}, methods)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			goldenPath := filepath.Join("testdata", tc.name+".golden.go")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}
			if string(got) != string(want) {
				t.Fatalf("generated output differs from %s", goldenPath)
			}
		})
	}
}
