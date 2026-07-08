package remote

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNewClientSetRequiresURL(t *testing.T) {
	t.Parallel()

	_, err := NewClientSet(context.Background(), Config{Token: "token"})
	if err == nil {
		t.Fatal("NewClientSet: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("NewClientSet error = %q", err.Error())
	}
}

func TestNewClientSetRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := NewClientSet(context.Background(), Config{URL: "https://valon.tools"})
	if err == nil {
		t.Fatal("NewClientSet: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("NewClientSet error = %q", err.Error())
	}
}

func TestNewClientSetRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	_, err := NewClientSet(context.Background(), Config{
		URL:   "ftp://valon.tools",
		Token: "token",
	})
	if err == nil {
		t.Fatal("NewClientSet: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("NewClientSet error = %q", err.Error())
	}
}

func TestTransportCredentialsHTTPS(t *testing.T) {
	t.Parallel()

	target, opt, err := transportCredentials(mustParseURL(t, "https://valon.tools/"))
	if err != nil {
		t.Fatalf("transportCredentials: %v", err)
	}
	if target != "valon.tools:443" {
		t.Fatalf("target = %q, want valon.tools:443", target)
	}
	if opt == nil {
		t.Fatal("expected transport credentials option")
	}
}

func TestTransportCredentialsHTTP(t *testing.T) {
	t.Parallel()

	target, opt, err := transportCredentials(mustParseURL(t, "http://127.0.0.1:8080"))
	if err != nil {
		t.Fatalf("transportCredentials: %v", err)
	}
	if target != "127.0.0.1:8080" {
		t.Fatalf("target = %q", target)
	}
	if opt == nil {
		t.Fatal("expected transport credentials option")
	}
}

func TestWithBearerAppendsAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	ctx := WithBearer(context.Background(), "secret")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata")
	}
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Fatalf("authorization metadata = %#v", got)
	}
}

func TestBearerUnaryInterceptorAttachesToken(t *testing.T) {
	t.Parallel()

	var got string
	interceptor := bearerUnaryInterceptor("gst_api_test")
	err := interceptor(context.Background(), "/test", nil, nil, nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok || len(md.Get("authorization")) == 0 {
			t.Fatal("expected authorization metadata on outgoing context")
		}
		got = md.Get("authorization")[0]
		return nil
	})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got != "Bearer gst_api_test" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestBearerUnaryInterceptorPreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	want := grpcstatus.Error(codes.PermissionDenied, "remote denied")
	interceptor := bearerUnaryInterceptor("gst_api_test")
	err := interceptor(context.Background(), "/test", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return want
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want PermissionDenied", grpcstatus.Code(err))
	}
	if !strings.Contains(err.Error(), "remote denied") {
		t.Fatalf("error = %v", err)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	return parsed
}
