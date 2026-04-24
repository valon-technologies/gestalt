package gestalt

import "google.golang.org/grpc"

func internalHostServiceBaseDialOptions(base ...grpc.DialOption) []grpc.DialOption {
	opts := make([]grpc.DialOption, 0, len(base)+1)
	opts = append(opts, grpc.WithNoProxy())
	opts = append(opts, base...)
	return opts
}
