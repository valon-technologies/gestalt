package gestalt

import (
	"context"
	"fmt"

	gproto "google.golang.org/protobuf/proto"
)

func ensureManagerClient(service string, ready bool) error {
	if ready {
		return nil
	}
	return fmt.Errorf("%s: client is not initialized", service)
}

func managerRequestWithToken[Req gproto.Message](req gproto.Message, empty Req, token string, set func(Req, string)) Req {
	value := empty
	if req != nil {
		value = gproto.Clone(req).(Req)
	}
	set(value, token)
	return value
}

func managerUnary[Req gproto.Message, Resp any](
	ctx context.Context,
	service string,
	ready bool,
	req gproto.Message,
	empty Req,
	token string,
	set func(Req, string),
	invoke func(context.Context, Req) (Resp, error),
) (Resp, error) {
	var zero Resp
	if err := ensureManagerClient(service, ready); err != nil {
		return zero, err
	}
	return invoke(ctx, managerRequestWithToken(req, empty, token, set))
}

func managerUnaryNoResponse[Req gproto.Message](
	ctx context.Context,
	service string,
	ready bool,
	req gproto.Message,
	empty Req,
	token string,
	set func(Req, string),
	invoke func(context.Context, Req) error,
) error {
	if err := ensureManagerClient(service, ready); err != nil {
		return err
	}
	return invoke(ctx, managerRequestWithToken(req, empty, token, set))
}
