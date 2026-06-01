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
	CreateObjectAccessURL(ctx context.Context, req PresignRequest) (PresignResult, error)
}

// Object returns a convenience handle for one object key on client.
func Object(client S3, key string) *ObjectHandleRef {
	return NewObject(client, ObjectRef{Key: key})
}

// ObjectVersion returns a convenience handle for one object version on client.
func ObjectVersion(client S3, key, versionID string) *ObjectHandleRef {
	return NewObject(client, ObjectRef{Key: key, VersionID: versionID})
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
func (o *ObjectHandleRef) Stream(ctx context.Context, req *ReadRequest) (ObjectMeta, io.ReadCloser, error) {
	if o == nil || o.client == nil {
		return ObjectMeta{}, nil, fmt.Errorf("s3: object client is required")
	}
	readReq := ReadRequest{}
	if req != nil {
		readReq = *req
	}
	if err := requireMatchingRef(readReq.Ref, o.ref); err != nil {
		return ObjectMeta{}, nil, err
	}
	readReq.Ref = o.ref
	result, err := o.client.ReadObject(ctx, readReq)
	if err != nil {
		return ObjectMeta{}, nil, err
	}
	return result.Meta, result.Body, nil
}

// Bytes reads the entire current object into memory.
func (o *ObjectHandleRef) Bytes(ctx context.Context, req *ReadRequest) ([]byte, error) {
	_, body, err := o.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// Text reads the entire current object as UTF-8 text.
func (o *ObjectHandleRef) Text(ctx context.Context, req *ReadRequest) (string, error) {
	data, err := o.Bytes(ctx, req)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// JSON reads and decodes the entire current object as JSON.
func (o *ObjectHandleRef) JSON(ctx context.Context, req *ReadRequest) (any, error) {
	data, err := o.Bytes(ctx, req)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// Write uploads the body in req to the current object.
func (o *ObjectHandleRef) Write(ctx context.Context, req WriteRequest) (ObjectMeta, error) {
	if o == nil || o.client == nil {
		return ObjectMeta{}, fmt.Errorf("s3: object client is required")
	}
	if err := requireMatchingRef(req.Ref, o.ref); err != nil {
		return ObjectMeta{}, err
	}
	req.Ref = o.ref
	return o.client.WriteObject(ctx, req)
}

// WriteBytes uploads body as raw bytes.
func (o *ObjectHandleRef) WriteBytes(ctx context.Context, body []byte, req *WriteRequest) (ObjectMeta, error) {
	writeReq, err := objectWriteRequest(o.ref, req)
	if err != nil {
		return ObjectMeta{}, err
	}
	writeReq.Body = bytes.NewReader(body)
	return o.Write(ctx, writeReq)
}

// WriteString uploads body as text.
func (o *ObjectHandleRef) WriteString(ctx context.Context, body string, req *WriteRequest) (ObjectMeta, error) {
	return o.WriteBytes(ctx, []byte(body), req)
}

// WriteJSON uploads value as JSON, defaulting the content type when omitted.
func (o *ObjectHandleRef) WriteJSON(ctx context.Context, value any, req *WriteRequest) (ObjectMeta, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return ObjectMeta{}, err
	}
	writeReq, err := objectWriteRequest(o.ref, req)
	if err != nil {
		return ObjectMeta{}, err
	}
	if writeReq.ContentType == "" {
		writeReq.ContentType = "application/json"
	}
	writeReq.Body = bytes.NewReader(body)
	return o.Write(ctx, writeReq)
}

// Delete removes the current object.
func (o *ObjectHandleRef) Delete(ctx context.Context) error {
	if o == nil || o.client == nil {
		return fmt.Errorf("s3: object client is required")
	}
	return o.client.DeleteObject(ctx, o.ref)
}

// Presign creates a presigned URL for the current object.
func (o *ObjectHandleRef) Presign(ctx context.Context, req *PresignRequest) (PresignResult, error) {
	if o == nil || o.client == nil {
		return PresignResult{}, fmt.Errorf("s3: object client is required")
	}
	presignReq := PresignRequest{}
	if req != nil {
		presignReq = *req
	}
	if err := requireMatchingRef(presignReq.Ref, o.ref); err != nil {
		return PresignResult{}, err
	}
	presignReq.Ref = o.ref
	return o.client.PresignObject(ctx, presignReq)
}

// CreateAccessURL creates a host-mediated object-access URL for the current object.
func (o *ObjectHandleRef) CreateAccessURL(ctx context.Context, req *PresignRequest) (PresignResult, error) {
	if o == nil || o.client == nil {
		return PresignResult{}, fmt.Errorf("s3: object client is required")
	}
	creator, ok := o.client.(objectAccessURLCreator)
	if !ok {
		return PresignResult{}, ErrUnsupported
	}
	presignReq := PresignRequest{}
	if req != nil {
		presignReq = *req
	}
	if err := requireMatchingRef(presignReq.Ref, o.ref); err != nil {
		return PresignResult{}, err
	}
	presignReq.Ref = o.ref
	return creator.CreateObjectAccessURL(ctx, presignReq)
}

func requireMatchingRef(requestRef, handleRef ObjectRef) error {
	if requestRef == (ObjectRef{}) || requestRef == handleRef {
		return nil
	}
	return fmt.Errorf("s3: request ref %q does not match object handle ref %q", requestRef.Key, handleRef.Key)
}

func objectWriteRequest(handleRef ObjectRef, req *WriteRequest) (WriteRequest, error) {
	writeReq := WriteRequest{}
	if req != nil {
		writeReq = *req
	}
	if writeReq.Body != nil {
		return WriteRequest{}, fmt.Errorf("s3: request body must be empty when body is supplied separately")
	}
	if err := requireMatchingRef(writeReq.Ref, handleRef); err != nil {
		return WriteRequest{}, err
	}
	writeReq.Ref = handleRef
	return writeReq, nil
}
