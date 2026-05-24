package s3

import "errors"

var (
	ErrNotFound           = errors.New("s3: not found")
	ErrPreconditionFailed = errors.New("s3: precondition failed")
	ErrInvalidRange       = errors.New("s3: invalid range")
	ErrUnsupported        = errors.New("s3: unsupported")
)
