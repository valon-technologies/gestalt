package s3

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestS3ServerPassesKeysThroughUnchanged(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewServer(store, "roadmap")
	ctx := context.Background()

	stream := newStubS3WriteObjectServer(ctx, []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
				},
			},
		},
		{
			Msg: &proto.WriteObjectRequest_Data{Data: []byte("ready")},
		},
	})

	if err := srv.WriteObject(stream); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if stream.resp == nil {
		t.Fatal("expected WriteObject response")
	}

	_, err := store.HeadObject(ctx, s3sdk.ObjectRef{Key: "plans/q2.txt"})
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
}

func TestRoutingS3ServerRoutesByHostBindingMetadata(t *testing.T) {
	t.Parallel()

	main := &coretesting.StubS3{}
	archive := &coretesting.StubS3{}
	srv, _ := NewRoutingServers(map[string]s3sdk.S3{
		"main":    main,
		"archive": archive,
	}, "", "roadmap", nil)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(runtimehost.HostServiceBindingHeader, "archive"))
	stream := newStubS3WriteObjectServer(ctx, []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
				},
			},
		},
		{
			Msg: &proto.WriteObjectRequest_Data{Data: []byte("ready")},
		},
	})

	if err := srv.WriteObject(stream); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if _, err := archive.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q2.txt",
	}); err != nil {
		t.Fatalf("archive HeadObject: %v", err)
	}
	if _, err := main.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q2.txt",
	}); !errors.Is(err, s3sdk.ErrNotFound) {
		t.Fatalf("main HeadObject error = %v, want ErrNotFound", err)
	}
}

func TestS3ServerPassesListKeysThroughUnchanged(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewServer(store, "roadmap").(*s3Server)

	got := srv.listRequest(&proto.ListObjectsRequest{
		Prefix: "plans/",
	})
	if got.StartAfter != "" {
		t.Fatalf("StartAfter = %q, want empty", got.StartAfter)
	}
	if got.Prefix != "plans/" {
		t.Fatalf("Prefix = %q, want plans/", got.Prefix)
	}
}

func TestS3ServerListPassesKeysThroughUnchanged(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewServer(store, "roadmap").(*s3Server)
	ctx := context.Background()
	for _, key := range []string{
		"plans/a.txt",
		"plans/nested/b.txt",
		"plans/z.txt",
	} {
		if _, err := store.WriteObject(ctx, s3sdk.WriteRequest{
			Ref:  s3sdk.ObjectRef{Key: key},
			Body: strings.NewReader(key),
		}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	first, err := srv.ListObjects(ctx, &proto.ListObjectsRequest{
		Prefix:    "plans/",
		Delimiter: "/",
		MaxKeys:   1,
	})
	if err != nil {
		t.Fatalf("ListObjects(first): %v", err)
	}
	if len(first.GetObjects()) != 1 || first.GetObjects()[0].GetRef().GetKey() != "plans/a.txt" {
		t.Fatalf("first objects = %v, want [plans/a.txt]", first.GetObjects())
	}

	second, err := srv.ListObjects(ctx, &proto.ListObjectsRequest{
		Prefix:            "plans/",
		Delimiter:         "/",
		MaxKeys:           1,
		ContinuationToken: first.GetNextContinuationToken(),
	})
	if err != nil {
		t.Fatalf("ListObjects(second): %v", err)
	}
	if len(second.GetCommonPrefixes()) != 1 || second.GetCommonPrefixes()[0] != "plans/nested/" {
		t.Fatalf("second prefixes = %v, want [plans/nested/]", second.GetCommonPrefixes())
	}

	third, err := srv.ListObjects(ctx, &proto.ListObjectsRequest{
		Prefix:            "plans/",
		Delimiter:         "/",
		MaxKeys:           1,
		ContinuationToken: second.GetNextContinuationToken(),
	})
	if err != nil {
		t.Fatalf("ListObjects(third): %v", err)
	}
	if len(third.GetObjects()) != 1 || third.GetObjects()[0].GetRef().GetKey() != "plans/z.txt" {
		t.Fatalf("third objects = %v, want [plans/z.txt]", third.GetObjects())
	}
}

func TestS3ServerListRoundTripsOpaqueBackendContinuationTokens(t *testing.T) {
	t.Parallel()

	client := &recordingListS3Client{
		pages: []s3sdk.ListPage{
			{
				HasMore:               true,
				NextContinuationToken: "plugin_roadmap/internal-offset-2",
			},
			{},
		},
	}
	srv := NewServer(client, "roadmap")

	first, err := srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Prefix:  "plans/",
		MaxKeys: 1,
	})
	if err != nil {
		t.Fatalf("ListObjects(first): %v", err)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("first request count = %d, want 1", len(client.reqs))
	}
	if client.reqs[0].ContinuationToken != "" {
		t.Fatalf("first backend continuation token = %q, want empty", client.reqs[0].ContinuationToken)
	}
	if got := first.GetNextContinuationToken(); got != "plugin_roadmap/internal-offset-2" {
		t.Fatalf("continuation token = %q, want backend token", got)
	}

	_, err = srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Prefix:            "plans/",
		MaxKeys:           1,
		ContinuationToken: first.GetNextContinuationToken(),
	})
	if err != nil {
		t.Fatalf("ListObjects(second): %v", err)
	}
	if len(client.reqs) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.reqs))
	}
	if got := client.reqs[1].ContinuationToken; got != "plugin_roadmap/internal-offset-2" {
		t.Fatalf("second backend continuation token = %q, want %q", got, "plugin_roadmap/internal-offset-2")
	}
}

func TestS3ServerWriteObjectReturnsWhenProviderStopsReadingEarly(t *testing.T) {
	t.Parallel()

	srv := NewServer(shortReadS3Client{}, "roadmap")
	stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref: &proto.S3ObjectRef{Key: "plans/q3.txt"},
				},
			},
		},
		{Msg: &proto.WriteObjectRequest_Data{Data: []byte("first chunk")}},
		{Msg: &proto.WriteObjectRequest_Data{Data: []byte("second chunk")}},
	})

	done := make(chan error, 1)
	go func() {
		done <- srv.WriteObject(stream)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteObject: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteObject hung when provider returned before draining the request body")
	}
}

func TestS3ServerWriteObjectPropagatesProviderErrorAfterStoppingReadEarly(t *testing.T) {
	t.Parallel()

	srv := NewServer(funcS3Client{
		writeObject: func(_ context.Context, req s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
			return s3sdk.ObjectMeta{}, s3sdk.ErrPreconditionFailed
		},
	}, "roadmap")
	stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref:         &proto.S3ObjectRef{Key: "plans/q4.txt"},
					IfNoneMatch: "*",
				},
			},
		},
		{Msg: &proto.WriteObjectRequest_Data{Data: []byte("payload")}},
	})

	done := make(chan error, 1)
	go func() {
		done <- srv.WriteObject(stream)
	}()

	select {
	case err := <-done:
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("WriteObject error = %v, want codes.FailedPrecondition", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteObject hung when provider returned an early precondition error")
	}
}

func TestS3ServerWriteObjectPropagatesRecvErrorObservedDuringSendAndClose(t *testing.T) {
	t.Parallel()

	srv := NewServer(shortReadS3Client{}, "roadmap")
	sendStarted := make(chan struct{})
	recvDone := make(chan struct{})
	recvStep := 0
	recvErr := status.Error(codes.Unavailable, "recv failed")
	stream := &stubS3WriteObjectServer{
		ctx: context.Background(),
		recv: func() (*proto.WriteObjectRequest, error) {
			switch recvStep {
			case 0:
				recvStep++
				return &proto.WriteObjectRequest{
					Msg: &proto.WriteObjectRequest_Open{
						Open: &proto.WriteObjectOpen{
							Ref: &proto.S3ObjectRef{Key: "plans/q4.txt"},
						},
					},
				}, nil
			case 1:
				recvStep++
				return &proto.WriteObjectRequest{
					Msg: &proto.WriteObjectRequest_Data{Data: []byte("x")},
				}, nil
			}
			<-sendStarted
			close(recvDone)
			return nil, recvErr
		},
		sendAndClose: func(*proto.WriteObjectResponse) error {
			close(sendStarted)
			<-recvDone
			return nil
		},
	}

	err := srv.WriteObject(stream)
	if !errors.Is(err, recvErr) && status.Code(err) != codes.Unavailable {
		t.Fatalf("WriteObject error = %v, want recv error %v", err, recvErr)
	}
}

func TestS3ServerListReturnsBackendKeys(t *testing.T) {
	t.Parallel()

	srv := NewServer(listResultS3Client{
		page: s3sdk.ListPage{
			Objects: []s3sdk.ObjectMeta{
				{Ref: s3sdk.ObjectRef{Key: "plans/a.txt"}},
				{Ref: s3sdk.ObjectRef{Key: "plans/escape.txt"}},
			},
			CommonPrefixes: []string{
				"plans/nested/",
				"plans/escape/",
			},
		},
	}, "roadmap")

	resp, err := srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Prefix:    "plans/",
		Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if got := resp.GetCommonPrefixes(); len(got) != 2 || got[0] != "plans/nested/" || got[1] != "plans/escape/" {
		t.Fatalf("CommonPrefixes = %v, want backend prefixes", got)
	}
	if got := resp.GetObjects(); len(got) != 2 || got[0].GetRef().GetKey() != "plans/a.txt" || got[1].GetRef().GetKey() != "plans/escape.txt" {
		t.Fatalf("Objects = %v, want backend objects", got)
	}
}

func TestS3ServerReturnsBackendMetadata(t *testing.T) {
	t.Parallel()

	t.Run("head", func(t *testing.T) {
		t.Parallel()

		srv := NewServer(funcS3Client{
			headObject: func(context.Context, s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
				return s3sdk.ObjectMeta{Ref: s3sdk.ObjectRef{Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")

		resp, err := srv.HeadObject(context.Background(), &proto.HeadObjectRequest{
			Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
		})
		if err != nil || resp.GetMeta().GetRef().GetKey() != "plans/escape.txt" {
			t.Fatalf("HeadObject = %#v, %v; want backend metadata", resp, err)
		}
	})

	t.Run("read", func(t *testing.T) {
		t.Parallel()

		srv := NewServer(funcS3Client{
			readObject: func(context.Context, s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
				return s3sdk.ReadResult{
					Meta: s3sdk.ObjectMeta{Ref: s3sdk.ObjectRef{Key: "plans/escape.txt"}},
					Body: io.NopCloser(strings.NewReader("leak")),
				}, nil
			},
		}, "roadmap")
		stream := &stubS3ReadObjectServer{ctx: context.Background()}

		err := srv.ReadObject(&proto.ReadObjectRequest{
			Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
		}, stream)
		if err != nil || len(stream.chunks) != 2 {
			t.Fatalf("ReadObject = %v with %d chunks; want backend metadata and body", err, len(stream.chunks))
		}
	})

	t.Run("write", func(t *testing.T) {
		t.Parallel()

		srv := NewServer(funcS3Client{
			writeObject: func(_ context.Context, req s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
				return s3sdk.ObjectMeta{Ref: s3sdk.ObjectRef{Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")
		stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
			{
				Msg: &proto.WriteObjectRequest_Open{
					Open: &proto.WriteObjectOpen{
						Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
					},
				},
			},
			{Msg: &proto.WriteObjectRequest_Data{Data: []byte("payload")}},
		})

		err := srv.WriteObject(stream)
		if err != nil || stream.resp.GetMeta().GetRef().GetKey() != "plans/escape.txt" {
			t.Fatalf("WriteObject = %v, %#v; want backend metadata", err, stream.resp)
		}
	})

	t.Run("copy", func(t *testing.T) {
		t.Parallel()

		srv := NewServer(funcS3Client{
			copyObject: func(context.Context, s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
				return s3sdk.ObjectMeta{Ref: s3sdk.ObjectRef{Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")

		resp, err := srv.CopyObject(context.Background(), &proto.CopyObjectRequest{
			Source:      &proto.S3ObjectRef{Key: "plans/source.txt"},
			Destination: &proto.S3ObjectRef{Key: "plans/dest.txt"},
		})
		if err != nil || resp.GetMeta().GetRef().GetKey() != "plans/escape.txt" {
			t.Fatalf("CopyObject = %#v, %v; want backend metadata", resp, err)
		}
	})
}

func TestS3ServerRejectsAppScopedPresign(t *testing.T) {
	t.Parallel()

	called := false
	srv := NewServer(funcS3Client{
		presignObject: func(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
			called = true
			return s3sdk.PresignResult{}, nil
		},
	}, "roadmap")

	_, err := srv.PresignObject(context.Background(), &proto.PresignObjectRequest{
		Ref: &proto.S3ObjectRef{Key: "plans/q2.txt"},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("PresignObject error = %v, want codes.FailedPrecondition", err)
	}
	if called {
		t.Fatal("PresignObject called backend for app binding")
	}
}

func TestS3ServerAppScopedPresignReturnsHostedObjectAccessURL(t *testing.T) {
	t.Parallel()

	manager, err := NewObjectAccessURLManager([]byte("0123456789abcdef0123456789abcdef"), "https://gestalt.example.test")
	if err != nil {
		t.Fatalf("NewObjectAccessURLManager: %v", err)
	}
	called := false
	srv := NewServerWithOptions(funcS3Client{
		presignObject: func(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
			called = true
			return s3sdk.PresignResult{}, nil
		},
	}, "roadmap", ServerOptions{BindingName: "docs", AccessURLs: manager})

	resp, err := srv.PresignObject(context.Background(), &proto.PresignObjectRequest{
		Ref:            &proto.S3ObjectRef{Key: "plans/q2.txt"},
		Method:         proto.PresignMethod_PRESIGN_METHOD_PUT,
		ExpiresSeconds: 600,
		ContentType:    "text/plain",
		Headers:        map[string]string{"Content-Length": "5"},
	})
	if err != nil {
		t.Fatalf("PresignObject: %v", err)
	}
	if called {
		t.Fatal("PresignObject called backend for app binding")
	}
	if !strings.HasPrefix(resp.GetUrl(), "https://gestalt.example.test"+ObjectAccessPathPrefix) {
		t.Fatalf("url = %q, want hosted object access URL", resp.GetUrl())
	}
	if strings.Contains(resp.GetUrl(), "plans/q2.txt") {
		t.Fatalf("url leaks object path: %q", resp.GetUrl())
	}
	if resp.GetMethod() != proto.PresignMethod_PRESIGN_METHOD_PUT {
		t.Fatalf("method = %v, want PUT", resp.GetMethod())
	}
	if resp.GetHeaders()["Content-Length"] != "5" {
		t.Fatalf("Content-Length header = %q, want 5", resp.GetHeaders()["Content-Length"])
	}

	parsed, err := url.Parse(resp.GetUrl())
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	token := strings.TrimPrefix(parsed.Path, ObjectAccessPathPrefix)
	target, err := manager.ResolveToken(token)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if target.AppName != "roadmap" || target.BindingName != "docs" {
		t.Fatalf("target scope = %s/%s, want roadmap/docs", target.AppName, target.BindingName)
	}
	if target.Ref != (s3sdk.ObjectRef{Key: "plans/q2.txt"}) {
		t.Fatalf("target ref = %#v, want plans/q2.txt", target.Ref)
	}
	if target.Method != s3sdk.PresignMethodPut {
		t.Fatalf("target method = %q, want PUT", target.Method)
	}
}

type shortReadS3Client struct{}

func (shortReadS3Client) HeadObject(context.Context, s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (shortReadS3Client) ReadObject(context.Context, s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
	return s3sdk.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (shortReadS3Client) WriteObject(_ context.Context, req s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
	if req.Body != nil {
		buf := make([]byte, 1)
		_, _ = req.Body.Read(buf)
	}
	return s3sdk.ObjectMeta{Ref: req.Ref}, nil
}

func (shortReadS3Client) DeleteObject(context.Context, s3sdk.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (shortReadS3Client) ListObjects(context.Context, s3sdk.ListRequest) (s3sdk.ListPage, error) {
	return s3sdk.ListPage{}, errors.New("unexpected ListObjects call")
}

func (shortReadS3Client) CopyObject(context.Context, s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (shortReadS3Client) PresignObject(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
	return s3sdk.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (shortReadS3Client) Ping(context.Context) error { return nil }
func (shortReadS3Client) Close() error               { return nil }

type listResultS3Client struct {
	page s3sdk.ListPage
}

func (c listResultS3Client) HeadObject(context.Context, s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (c listResultS3Client) ReadObject(context.Context, s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
	return s3sdk.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (c listResultS3Client) WriteObject(context.Context, s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected WriteObject call")
}

func (c listResultS3Client) DeleteObject(context.Context, s3sdk.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (c listResultS3Client) ListObjects(context.Context, s3sdk.ListRequest) (s3sdk.ListPage, error) {
	return c.page, nil
}

func (c listResultS3Client) CopyObject(context.Context, s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (c listResultS3Client) PresignObject(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
	return s3sdk.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (c listResultS3Client) Ping(context.Context) error { return nil }
func (c listResultS3Client) Close() error               { return nil }

type recordingListS3Client struct {
	reqs  []s3sdk.ListRequest
	pages []s3sdk.ListPage
}

func (*recordingListS3Client) HeadObject(context.Context, s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (*recordingListS3Client) ReadObject(context.Context, s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
	return s3sdk.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (*recordingListS3Client) WriteObject(context.Context, s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected WriteObject call")
}

func (*recordingListS3Client) DeleteObject(context.Context, s3sdk.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (c *recordingListS3Client) ListObjects(_ context.Context, req s3sdk.ListRequest) (s3sdk.ListPage, error) {
	c.reqs = append(c.reqs, req)
	if len(c.pages) == 0 {
		return s3sdk.ListPage{}, nil
	}
	page := c.pages[0]
	c.pages = c.pages[1:]
	return page, nil
}

func (*recordingListS3Client) CopyObject(context.Context, s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
	return s3sdk.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (*recordingListS3Client) PresignObject(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
	return s3sdk.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (*recordingListS3Client) Ping(context.Context) error { return nil }
func (*recordingListS3Client) Close() error               { return nil }

type funcS3Client struct {
	headObject    func(context.Context, s3sdk.ObjectRef) (s3sdk.ObjectMeta, error)
	readObject    func(context.Context, s3sdk.ReadRequest) (s3sdk.ReadResult, error)
	writeObject   func(context.Context, s3sdk.WriteRequest) (s3sdk.ObjectMeta, error)
	deleteObject  func(context.Context, s3sdk.ObjectRef) error
	listObjects   func(context.Context, s3sdk.ListRequest) (s3sdk.ListPage, error)
	copyObject    func(context.Context, s3sdk.CopyRequest) (s3sdk.ObjectMeta, error)
	presignObject func(context.Context, s3sdk.PresignRequest) (s3sdk.PresignResult, error)
}

func (c funcS3Client) HeadObject(ctx context.Context, ref s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
	if c.headObject == nil {
		return s3sdk.ObjectMeta{}, errors.New("unexpected HeadObject call")
	}
	return c.headObject(ctx, ref)
}

func (c funcS3Client) ReadObject(ctx context.Context, req s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
	if c.readObject == nil {
		return s3sdk.ReadResult{}, errors.New("unexpected ReadObject call")
	}
	return c.readObject(ctx, req)
}

func (c funcS3Client) WriteObject(ctx context.Context, req s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
	if c.writeObject == nil {
		return s3sdk.ObjectMeta{}, errors.New("unexpected WriteObject call")
	}
	return c.writeObject(ctx, req)
}

func (c funcS3Client) DeleteObject(ctx context.Context, ref s3sdk.ObjectRef) error {
	if c.deleteObject == nil {
		return errors.New("unexpected DeleteObject call")
	}
	return c.deleteObject(ctx, ref)
}

func (c funcS3Client) ListObjects(ctx context.Context, req s3sdk.ListRequest) (s3sdk.ListPage, error) {
	if c.listObjects == nil {
		return s3sdk.ListPage{}, errors.New("unexpected ListObjects call")
	}
	return c.listObjects(ctx, req)
}

func (c funcS3Client) CopyObject(ctx context.Context, req s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
	if c.copyObject == nil {
		return s3sdk.ObjectMeta{}, errors.New("unexpected CopyObject call")
	}
	return c.copyObject(ctx, req)
}

func (c funcS3Client) PresignObject(ctx context.Context, req s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
	if c.presignObject == nil {
		return s3sdk.PresignResult{}, errors.New("unexpected PresignObject call")
	}
	return c.presignObject(ctx, req)
}

func (funcS3Client) Ping(context.Context) error { return nil }
func (funcS3Client) Close() error               { return nil }

type stubS3WriteObjectServer struct {
	proto.S3_WriteObjectServer
	ctx          context.Context
	reqs         []*proto.WriteObjectRequest
	index        int
	resp         *proto.WriteObjectResponse
	recv         func() (*proto.WriteObjectRequest, error)
	sendAndClose func(*proto.WriteObjectResponse) error
}

func newStubS3WriteObjectServer(ctx context.Context, reqs []*proto.WriteObjectRequest) *stubS3WriteObjectServer {
	return &stubS3WriteObjectServer{ctx: ctx, reqs: reqs}
}

func (s *stubS3WriteObjectServer) Context() context.Context {
	return s.ctx
}

func (s *stubS3WriteObjectServer) Recv() (*proto.WriteObjectRequest, error) {
	if s.recv != nil {
		return s.recv()
	}
	if s.index >= len(s.reqs) {
		return nil, io.EOF
	}
	req := s.reqs[s.index]
	s.index++
	return req, nil
}

func (s *stubS3WriteObjectServer) SendAndClose(resp *proto.WriteObjectResponse) error {
	if s.sendAndClose != nil {
		s.resp = resp
		return s.sendAndClose(resp)
	}
	s.resp = resp
	return nil
}

type stubS3ReadObjectServer struct {
	proto.S3_ReadObjectServer
	ctx    context.Context
	chunks []*proto.ReadObjectChunk
}

func (s *stubS3ReadObjectServer) Context() context.Context {
	return s.ctx
}

func (s *stubS3ReadObjectServer) Send(chunk *proto.ReadObjectChunk) error {
	s.chunks = append(s.chunks, chunk)
	return nil
}
