package core

import "context"

type Runtime interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
