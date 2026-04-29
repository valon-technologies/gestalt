package providerhost

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestS3ServerPrefixesKeysPerPlugin(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewS3Server(store, "roadmap")
	ctx := context.Background()

	stream := newStubS3WriteObjectServer(ctx, []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
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

	_, err := store.HeadObject(ctx, s3store.ObjectRef{
		Bucket: "docs",
		Key:    s3NamespacePrefix("roadmap") + "plans/q2.txt",
	})
	if err != nil {
		t.Fatalf("HeadObject(prefixed): %v", err)
	}
	_, err = store.HeadObject(ctx, s3store.ObjectRef{
		Bucket: "docs",
		Key:    "plans/q2.txt",
	})
	if !errors.Is(err, s3store.ErrNotFound) {
		t.Fatalf("HeadObject(unprefixed) error = %v, want ErrNotFound", err)
	}
}

func TestS3ServerLeavesEmptyStartAfterUnset(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewS3Server(store, "roadmap").(*s3Server)

	got, gotErr := srv.namespacedListRequest(&proto.ListObjectsRequest{
		Bucket: "docs",
		Prefix: "plans/",
	})
	if gotErr != nil {
		t.Fatalf("namespacedListRequest: %v", gotErr)
	}
	if got.StartAfter != "" {
		t.Fatalf("StartAfter = %q, want empty", got.StartAfter)
	}
	wantPrefix := s3NamespacePrefix("roadmap") + "plans/"
	if got.Prefix != wantPrefix {
		t.Fatalf("Prefix = %q, want %q", got.Prefix, wantPrefix)
	}
}

func TestS3ServerListWrapsContinuationTokensForNamespacedPlugins(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	srv := NewS3Server(store, "roadmap").(*s3Server)
	ctx := context.Background()
	for _, key := range []string{
		s3NamespacePrefix("roadmap") + "plans/a.txt",
		s3NamespacePrefix("roadmap") + "plans/nested/b.txt",
		s3NamespacePrefix("roadmap") + "plans/z.txt",
	} {
		if _, err := store.WriteObject(ctx, s3store.WriteRequest{
			Ref:  s3store.ObjectRef{Bucket: "docs", Key: key},
			Body: strings.NewReader(key),
		}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	first, err := srv.ListObjects(ctx, &proto.ListObjectsRequest{
		Bucket:    "docs",
		Prefix:    "plans/",
		Delimiter: "/",
		MaxKeys:   1,
	})
	if err != nil {
		t.Fatalf("ListObjects(first): %v", err)
	}
	if got := first.GetNextContinuationToken(); strings.Contains(got, s3NamespacePrefix("roadmap")) {
		t.Fatalf("NextContinuationToken leaked namespace prefix: %q", got)
	}
	if len(first.GetObjects()) != 1 || first.GetObjects()[0].GetRef().GetKey() != "plans/a.txt" {
		t.Fatalf("first objects = %v, want [plans/a.txt]", first.GetObjects())
	}

	second, err := srv.ListObjects(ctx, &proto.ListObjectsRequest{
		Bucket:            "docs",
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
		Bucket:            "docs",
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
		pages: []s3store.ListPage{
			{
				HasMore:               true,
				NextContinuationToken: "plugin_roadmap/internal-offset-2",
			},
			{},
		},
	}
	srv := NewS3Server(client, "roadmap")

	first, err := srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Bucket:  "docs",
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
	if got := first.GetNextContinuationToken(); got == "" || got == "plugin_roadmap/internal-offset-2" || strings.Contains(got, s3NamespacePrefix("roadmap")) {
		t.Fatalf("wrapped continuation token = %q, want opaque wrapped token", got)
	}

	_, err = srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Bucket:            "docs",
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

	srv := NewS3Server(shortReadS3Client{}, "roadmap")
	stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q3.txt"},
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

	srv := NewS3Server(funcS3Client{
		writeObject: func(_ context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
			return s3store.ObjectMeta{}, s3store.ErrPreconditionFailed
		},
	}, "roadmap")
	stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
		{
			Msg: &proto.WriteObjectRequest_Open{
				Open: &proto.WriteObjectOpen{
					Ref:         &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q4.txt"},
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

	srv := NewS3Server(shortReadS3Client{}, "roadmap")
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
							Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q4.txt"},
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

func TestS3ServerListDropsKeysOutsidePluginNamespace(t *testing.T) {
	t.Parallel()

	srv := NewS3Server(listResultS3Client{
		page: s3store.ListPage{
			Objects: []s3store.ObjectMeta{
				{Ref: s3store.ObjectRef{Bucket: "docs", Key: s3NamespacePrefix("roadmap") + "plans/a.txt"}},
				{Ref: s3store.ObjectRef{Bucket: "docs", Key: "plans/escape.txt"}},
			},
			CommonPrefixes: []string{
				s3NamespacePrefix("roadmap") + "plans/nested/",
				"plans/escape/",
			},
		},
	}, "roadmap")

	resp, err := srv.ListObjects(context.Background(), &proto.ListObjectsRequest{
		Bucket:    "docs",
		Prefix:    "plans/",
		Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if got := resp.GetCommonPrefixes(); len(got) != 1 || got[0] != "plans/nested/" {
		t.Fatalf("CommonPrefixes = %v, want [plans/nested/]", got)
	}
	if got := resp.GetObjects(); len(got) != 1 || got[0].GetRef().GetKey() != "plans/a.txt" {
		t.Fatalf("Objects = %v, want [plans/a.txt]", got)
	}
}

func TestS3ServerRejectsForeignMetadataOutsidePluginNamespace(t *testing.T) {
	t.Parallel()

	t.Run("head", func(t *testing.T) {
		t.Parallel()

		srv := NewS3Server(funcS3Client{
			headObject: func(context.Context, s3store.ObjectRef) (s3store.ObjectMeta, error) {
				return s3store.ObjectMeta{Ref: s3store.ObjectRef{Bucket: "docs", Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")

		_, err := srv.HeadObject(context.Background(), &proto.HeadObjectRequest{
			Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("HeadObject error = %v, want codes.Internal", err)
		}
	})

	t.Run("read", func(t *testing.T) {
		t.Parallel()

		srv := NewS3Server(funcS3Client{
			readObject: func(context.Context, s3store.ReadRequest) (s3store.ReadResult, error) {
				return s3store.ReadResult{
					Meta: s3store.ObjectMeta{Ref: s3store.ObjectRef{Bucket: "docs", Key: "plans/escape.txt"}},
					Body: io.NopCloser(strings.NewReader("leak")),
				}, nil
			},
		}, "roadmap")
		stream := &stubS3ReadObjectServer{ctx: context.Background()}

		err := srv.ReadObject(&proto.ReadObjectRequest{
			Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
		}, stream)
		if status.Code(err) != codes.Internal {
			t.Fatalf("ReadObject error = %v, want codes.Internal", err)
		}
		if len(stream.chunks) != 0 {
			t.Fatalf("ReadObject leaked chunks = %d, want 0", len(stream.chunks))
		}
	})

	t.Run("write", func(t *testing.T) {
		t.Parallel()

		srv := NewS3Server(funcS3Client{
			writeObject: func(_ context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
				return s3store.ObjectMeta{Ref: s3store.ObjectRef{Bucket: req.Ref.Bucket, Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")
		stream := newStubS3WriteObjectServer(context.Background(), []*proto.WriteObjectRequest{
			{
				Msg: &proto.WriteObjectRequest_Open{
					Open: &proto.WriteObjectOpen{
						Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
					},
				},
			},
			{Msg: &proto.WriteObjectRequest_Data{Data: []byte("payload")}},
		})

		err := srv.WriteObject(stream)
		if status.Code(err) != codes.Internal {
			t.Fatalf("WriteObject error = %v, want codes.Internal", err)
		}
	})

	t.Run("copy", func(t *testing.T) {
		t.Parallel()

		srv := NewS3Server(funcS3Client{
			copyObject: func(context.Context, s3store.CopyRequest) (s3store.ObjectMeta, error) {
				return s3store.ObjectMeta{Ref: s3store.ObjectRef{Bucket: "docs", Key: "plans/escape.txt"}}, nil
			},
		}, "roadmap")

		_, err := srv.CopyObject(context.Background(), &proto.CopyObjectRequest{
			Source:      &proto.S3ObjectRef{Bucket: "docs", Key: "plans/source.txt"},
			Destination: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/dest.txt"},
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("CopyObject error = %v, want codes.Internal", err)
		}
	})
}

func TestS3ServerRejectsPluginScopedPresign(t *testing.T) {
	t.Parallel()

	called := false
	srv := NewS3Server(funcS3Client{
		presignObject: func(context.Context, s3store.PresignRequest) (s3store.PresignResult, error) {
			called = true
			return s3store.PresignResult{}, nil
		},
	}, "roadmap")

	_, err := srv.PresignObject(context.Background(), &proto.PresignObjectRequest{
		Ref: &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("PresignObject error = %v, want codes.FailedPrecondition", err)
	}
	if called {
		t.Fatal("PresignObject called backend for plugin-scoped binding")
	}
}

func TestS3ServerPluginScopedPresignReturnsHostedObjectAccessURL(t *testing.T) {
	t.Parallel()

	manager, err := NewS3ObjectAccessURLManager([]byte("0123456789abcdef0123456789abcdef"), "https://gestalt.example.test")
	if err != nil {
		t.Fatalf("NewS3ObjectAccessURLManager: %v", err)
	}
	called := false
	srv := NewS3ServerWithOptions(funcS3Client{
		presignObject: func(context.Context, s3store.PresignRequest) (s3store.PresignResult, error) {
			called = true
			return s3store.PresignResult{}, nil
		},
	}, "roadmap", S3ServerOptions{BindingName: "docs", AccessURLs: manager})

	resp, err := srv.PresignObject(context.Background(), &proto.PresignObjectRequest{
		Ref:            &proto.S3ObjectRef{Bucket: "docs", Key: "plans/q2.txt"},
		Method:         proto.PresignMethod_PRESIGN_METHOD_PUT,
		ExpiresSeconds: 600,
		ContentType:    "text/plain",
		Headers:        map[string]string{"Content-Length": "5"},
	})
	if err != nil {
		t.Fatalf("PresignObject: %v", err)
	}
	if called {
		t.Fatal("PresignObject called backend for plugin-scoped binding")
	}
	if !strings.HasPrefix(resp.GetUrl(), "https://gestalt.example.test"+S3ObjectAccessPathPrefix) {
		t.Fatalf("url = %q, want hosted object access URL", resp.GetUrl())
	}
	if strings.Contains(resp.GetUrl(), s3NamespacePrefix("roadmap")) || strings.Contains(resp.GetUrl(), "plans/q2.txt") {
		t.Fatalf("url leaks plugin-scoped object path: %q", resp.GetUrl())
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
	token := strings.TrimPrefix(parsed.Path, S3ObjectAccessPathPrefix)
	target, err := manager.ResolveToken(token)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if target.PluginName != "roadmap" || target.BindingName != "docs" {
		t.Fatalf("target scope = %s/%s, want roadmap/docs", target.PluginName, target.BindingName)
	}
	if target.Ref != (s3store.ObjectRef{Bucket: "docs", Key: "plans/q2.txt"}) {
		t.Fatalf("target ref = %#v, want docs/plans/q2.txt", target.Ref)
	}
	if target.Method != s3store.PresignMethodPut {
		t.Fatalf("target method = %q, want PUT", target.Method)
	}
}

type shortReadS3Client struct{}

func (shortReadS3Client) HeadObject(context.Context, s3store.ObjectRef) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (shortReadS3Client) ReadObject(context.Context, s3store.ReadRequest) (s3store.ReadResult, error) {
	return s3store.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (shortReadS3Client) WriteObject(_ context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
	if req.Body != nil {
		buf := make([]byte, 1)
		_, _ = req.Body.Read(buf)
	}
	return s3store.ObjectMeta{Ref: req.Ref}, nil
}

func (shortReadS3Client) DeleteObject(context.Context, s3store.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (shortReadS3Client) ListObjects(context.Context, s3store.ListRequest) (s3store.ListPage, error) {
	return s3store.ListPage{}, errors.New("unexpected ListObjects call")
}

func (shortReadS3Client) CopyObject(context.Context, s3store.CopyRequest) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (shortReadS3Client) PresignObject(context.Context, s3store.PresignRequest) (s3store.PresignResult, error) {
	return s3store.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (shortReadS3Client) Ping(context.Context) error { return nil }
func (shortReadS3Client) Close() error               { return nil }

type listResultS3Client struct {
	page s3store.ListPage
}

func (c listResultS3Client) HeadObject(context.Context, s3store.ObjectRef) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (c listResultS3Client) ReadObject(context.Context, s3store.ReadRequest) (s3store.ReadResult, error) {
	return s3store.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (c listResultS3Client) WriteObject(context.Context, s3store.WriteRequest) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected WriteObject call")
}

func (c listResultS3Client) DeleteObject(context.Context, s3store.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (c listResultS3Client) ListObjects(context.Context, s3store.ListRequest) (s3store.ListPage, error) {
	return c.page, nil
}

func (c listResultS3Client) CopyObject(context.Context, s3store.CopyRequest) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (c listResultS3Client) PresignObject(context.Context, s3store.PresignRequest) (s3store.PresignResult, error) {
	return s3store.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (c listResultS3Client) Ping(context.Context) error { return nil }
func (c listResultS3Client) Close() error               { return nil }

type recordingListS3Client struct {
	reqs  []s3store.ListRequest
	pages []s3store.ListPage
}

func (*recordingListS3Client) HeadObject(context.Context, s3store.ObjectRef) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected HeadObject call")
}

func (*recordingListS3Client) ReadObject(context.Context, s3store.ReadRequest) (s3store.ReadResult, error) {
	return s3store.ReadResult{}, errors.New("unexpected ReadObject call")
}

func (*recordingListS3Client) WriteObject(context.Context, s3store.WriteRequest) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected WriteObject call")
}

func (*recordingListS3Client) DeleteObject(context.Context, s3store.ObjectRef) error {
	return errors.New("unexpected DeleteObject call")
}

func (c *recordingListS3Client) ListObjects(_ context.Context, req s3store.ListRequest) (s3store.ListPage, error) {
	c.reqs = append(c.reqs, req)
	if len(c.pages) == 0 {
		return s3store.ListPage{}, nil
	}
	page := c.pages[0]
	c.pages = c.pages[1:]
	return page, nil
}

func (*recordingListS3Client) CopyObject(context.Context, s3store.CopyRequest) (s3store.ObjectMeta, error) {
	return s3store.ObjectMeta{}, errors.New("unexpected CopyObject call")
}

func (*recordingListS3Client) PresignObject(context.Context, s3store.PresignRequest) (s3store.PresignResult, error) {
	return s3store.PresignResult{}, errors.New("unexpected PresignObject call")
}

func (*recordingListS3Client) Ping(context.Context) error { return nil }
func (*recordingListS3Client) Close() error               { return nil }

type funcS3Client struct {
	headObject    func(context.Context, s3store.ObjectRef) (s3store.ObjectMeta, error)
	readObject    func(context.Context, s3store.ReadRequest) (s3store.ReadResult, error)
	writeObject   func(context.Context, s3store.WriteRequest) (s3store.ObjectMeta, error)
	deleteObject  func(context.Context, s3store.ObjectRef) error
	listObjects   func(context.Context, s3store.ListRequest) (s3store.ListPage, error)
	copyObject    func(context.Context, s3store.CopyRequest) (s3store.ObjectMeta, error)
	presignObject func(context.Context, s3store.PresignRequest) (s3store.PresignResult, error)
}

func (c funcS3Client) HeadObject(ctx context.Context, ref s3store.ObjectRef) (s3store.ObjectMeta, error) {
	if c.headObject == nil {
		return s3store.ObjectMeta{}, errors.New("unexpected HeadObject call")
	}
	return c.headObject(ctx, ref)
}

func (c funcS3Client) ReadObject(ctx context.Context, req s3store.ReadRequest) (s3store.ReadResult, error) {
	if c.readObject == nil {
		return s3store.ReadResult{}, errors.New("unexpected ReadObject call")
	}
	return c.readObject(ctx, req)
}

func (c funcS3Client) WriteObject(ctx context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
	if c.writeObject == nil {
		return s3store.ObjectMeta{}, errors.New("unexpected WriteObject call")
	}
	return c.writeObject(ctx, req)
}

func (c funcS3Client) DeleteObject(ctx context.Context, ref s3store.ObjectRef) error {
	if c.deleteObject == nil {
		return errors.New("unexpected DeleteObject call")
	}
	return c.deleteObject(ctx, ref)
}

func (c funcS3Client) ListObjects(ctx context.Context, req s3store.ListRequest) (s3store.ListPage, error) {
	if c.listObjects == nil {
		return s3store.ListPage{}, errors.New("unexpected ListObjects call")
	}
	return c.listObjects(ctx, req)
}

func (c funcS3Client) CopyObject(ctx context.Context, req s3store.CopyRequest) (s3store.ObjectMeta, error) {
	if c.copyObject == nil {
		return s3store.ObjectMeta{}, errors.New("unexpected CopyObject call")
	}
	return c.copyObject(ctx, req)
}

func (c funcS3Client) PresignObject(ctx context.Context, req s3store.PresignRequest) (s3store.PresignResult, error) {
	if c.presignObject == nil {
		return s3store.PresignResult{}, errors.New("unexpected PresignObject call")
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
