package s3

import (
	"context"
	"testing"
	"time"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

const testUnaryTimeout = 30 * time.Second

type deadlineS3Client struct {
	proto.S3Client
	headObject func(context.Context, *proto.HeadObjectRequest, ...grpc.CallOption) (*proto.HeadObjectResponse, error)
}

func (c *deadlineS3Client) HeadObject(ctx context.Context, req *proto.HeadObjectRequest, opts ...grpc.CallOption) (*proto.HeadObjectResponse, error) {
	return c.headObject(ctx, req, opts...)
}

func TestHeadObjectUsesUnaryTimeout(t *testing.T) {
	t.Parallel()

	client := &deadlineS3Client{
		headObject: func(ctx context.Context, _ *proto.HeadObjectRequest, _ ...grpc.CallOption) (*proto.HeadObjectResponse, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("head object context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= testUnaryTimeout-2*time.Second || remaining > testUnaryTimeout {
				t.Fatalf("head object deadline remaining = %s, want within 2s of %s", remaining, testUnaryTimeout)
			}
			return &proto.HeadObjectResponse{
				Meta: &proto.S3ObjectMeta{
					Ref: &proto.S3ObjectRef{Bucket: "b", Key: "k"},
				},
			}, nil
		},
	}

	rpc := NewClient(client, nil, Options{UnaryTimeout: testUnaryTimeout})
	if _, err := rpc.HeadObject(context.Background(), s3sdk.ObjectRef{Bucket: "b", Key: "k"}); err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
}
