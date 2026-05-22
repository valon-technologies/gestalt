package main

import "testing"

func TestQualifyTypesPrefixesUnqualifiedProtoTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{
			in:   "ctx context.Context, req *CacheGetRequest",
			want: "ctx context.Context, req *proto.CacheGetRequest",
		},
		{
			in:   "*CacheGetResponse, error",
			want: "*proto.CacheGetResponse, error",
		},
		{
			in:   "req *proto.CacheGetRequest",
			want: "req *proto.CacheGetRequest",
		},
		{
			in:   "req *emptypb.Empty",
			want: "req *emptypb.Empty",
		},
		{
			in:   "stream grpc.ServerStreamingServer[*S3]",
			want: "stream grpc.ServerStreamingServer[*proto.S3]",
		},
	}

	for _, tc := range tests {
		if got := qualifyTypes(tc.in); got != tc.want {
			t.Fatalf("qualifyTypes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
