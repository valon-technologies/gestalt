package runtimehost

import "google.golang.org/grpc"

func hostServiceServerOptions(cfgOpts []grpc.ServerOption, opts ...grpc.ServerOption) []grpc.ServerOption {
	combined := append([]grpc.ServerOption{}, opts...)
	if len(cfgOpts) > 0 {
		combined = append(combined, cfgOpts...)
	}
	return combined
}
