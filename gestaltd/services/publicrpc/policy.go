package publicrpc

// PublicMethodPolicy describes how a public gRPC method handles trusted fields.
type PublicMethodPolicy struct {
	FullMethod string
	Service    string
	Method     string
	Fill       []string
	Reject     []string
}

// PublicMethodRegistry resolves public method policy by grpc-go full method.
type PublicMethodRegistry interface {
	Lookup(fullMethod string) (PublicMethodPolicy, bool)
}
