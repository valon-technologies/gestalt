package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type objectAccessURLCreator interface {
	CreateObjectAccessURL(ctx context.Context, ref ObjectRef, opts *ObjectAccessURLOptions) (ObjectAccessURL, error)
}

// Object returns a convenience handle for one object key on client.
func Object(client S3, bucket, key string) *ObjectHandleRef {
	return NewObject(client, ObjectRef{Bucket: bucket, Key: key})
}

// ObjectVersion returns a convenience handle for one object version on client.
func ObjectVersion(client S3, bucket, key, versionID string) *ObjectHandleRef {
	return NewObject(client, ObjectRef{Bucket: bucket, Key: key, VersionID: versionID})
}

// NewObject returns a convenience handle for ref on client.
func NewObject(client S3, ref ObjectRef) *ObjectHandleRef {
	return &ObjectHandleRef{client: client, ref: ref}
}

// ObjectHandleRef implements ObjectHandle using the request-oriented S3 interface.
type ObjectHandleRef struct {
	client S3
	ref    ObjectRef
}

var _ ObjectHandle = (*ObjectHandleRef)(nil)

// Stat returns metadata for the current object.
func (o *ObjectHandleRef) Stat(ctx context.Context) (ObjectMeta, error) {
	if o == nil || o.client == nil {
		return ObjectMeta{}, fmt.Errorf("s3: object client is required")
	}
	return o.client.HeadObject(ctx, o.ref)
}

// Exists reports whether the current object exists.
func (o *ObjectHandleRef) Exists(ctx context.Context) (bool, error) {
	_, err := o.Stat(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

// Stream opens a streaming reader for the current object.
func (o *ObjectHandleRef) Stream(ctx context.Context, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error) {
	if o == nil || o.client == nil {
		return ObjectMeta{}, nil, fmt.Errorf("s3: object client is required")
	}
	req := ReadRequest{Ref: o.ref}
	if opts != nil {
		req.Range = opts.Range
		req.IfMatch = opts.IfMatch
		req.IfNoneMatch = opts.IfNoneMatch
		req.IfModifiedSince = opts.IfModifiedSince
		req.IfUnmodifiedSince = opts.IfUnmodifiedSince
	}
	result, err := o.client.ReadObject(ctx, req)
	if err != nil {
		return ObjectMeta{}, nil, err
	}
	return result.Meta, result.Body, nil
}

// Bytes reads the entire current object into memory.
func (o *ObjectHandleRef) Bytes(ctx context.Context, opts *ReadOptions) ([]byte, error) {
	_, body, err := o.Stream(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// Text reads the entire current object as UTF-8 text.
func (o *ObjectHandleRef) Text(ctx context.Context, opts *ReadOptions) (string, error) {
	data, err := o.Bytes(ctx, opts)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// JSON reads and decodes the entire current object as JSON.
func (o *ObjectHandleRef) JSON(ctx context.Context, opts *ReadOptions) (any, error) {
	data, err := o.Bytes(ctx, opts)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// Write uploads a new object body from body.
func (o *ObjectHandleRef) Write(ctx context.Context, body io.Reader, opts *WriteOptions) (ObjectMeta, error) {
	if o == nil || o.client == nil {
		return ObjectMeta{}, fmt.Errorf("s3: object client is required")
	}
	req := WriteRequest{Ref: o.ref, Body: body}
	if opts != nil {
		req.ContentType = opts.ContentType
		req.CacheControl = opts.CacheControl
		req.ContentDisposition = opts.ContentDisposition
		req.ContentEncoding = opts.ContentEncoding
		req.ContentLanguage = opts.ContentLanguage
		req.Metadata = opts.Metadata
		req.IfMatch = opts.IfMatch
		req.IfNoneMatch = opts.IfNoneMatch
	}
	return o.client.WriteObject(ctx, req)
}

// WriteBytes uploads body as raw bytes.
func (o *ObjectHandleRef) WriteBytes(ctx context.Context, body []byte, opts *WriteOptions) (ObjectMeta, error) {
	return o.Write(ctx, bytes.NewReader(body), opts)
}

// WriteString uploads body as text.
func (o *ObjectHandleRef) WriteString(ctx context.Context, body string, opts *WriteOptions) (ObjectMeta, error) {
	return o.WriteBytes(ctx, []byte(body), opts)
}

// WriteJSON uploads value as JSON, defaulting the content type when omitted.
func (o *ObjectHandleRef) WriteJSON(ctx context.Context, value any, opts *WriteOptions) (ObjectMeta, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return ObjectMeta{}, err
	}
	if opts == nil {
		opts = &WriteOptions{ContentType: "application/json"}
	} else if opts.ContentType == "" {
		opts.ContentType = "application/json"
	}
	return o.WriteBytes(ctx, body, opts)
}

// Delete removes the current object.
func (o *ObjectHandleRef) Delete(ctx context.Context) error {
	if o == nil || o.client == nil {
		return fmt.Errorf("s3: object client is required")
	}
	return o.client.DeleteObject(ctx, o.ref)
}

// Presign creates a presigned URL for the current object.
func (o *ObjectHandleRef) Presign(ctx context.Context, opts *PresignOptions) (PresignResult, error) {
	if o == nil || o.client == nil {
		return PresignResult{}, fmt.Errorf("s3: object client is required")
	}
	req := PresignRequest{Ref: o.ref}
	if opts != nil {
		req.Method = opts.Method
		req.Expires = opts.Expires
		req.ContentType = opts.ContentType
		req.ContentDisposition = opts.ContentDisposition
		req.Headers = opts.Headers
	}
	return o.client.PresignObject(ctx, req)
}

// CreateAccessURL creates a host-mediated object-access URL for the current object.
func (o *ObjectHandleRef) CreateAccessURL(ctx context.Context, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	if o == nil || o.client == nil {
		return ObjectAccessURL{}, fmt.Errorf("s3: object client is required")
	}
	creator, ok := o.client.(objectAccessURLCreator)
	if !ok {
		return ObjectAccessURL{}, ErrUnsupported
	}
	return creator.CreateObjectAccessURL(ctx, o.ref, opts)
}

// CreateAccessUrl is an alias for CreateAccessURL.
func (o *ObjectHandleRef) CreateAccessUrl(ctx context.Context, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	return o.CreateAccessURL(ctx, opts)
}
