package s3

import "errors"

// Sentinel errors backends return so the daemon can map storage failures
// to canonical statuses.
var (
	ErrNotFound           = errors.New("s3: not found")
	ErrPreconditionFailed = errors.New("s3: precondition failed")
	ErrInvalidRange       = errors.New("s3: invalid range")
	ErrUnsupported        = errors.New("s3: unsupported")
)
