package gestalt

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type sharedManagerTransport[C any] = host.SharedTransport[C]
type protoMessage = gproto.Message

func managerTransportClient[C any](ctx context.Context, serviceName, target, token string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return host.ManagerClient(ctx, serviceName, target, token, transport, newClient)
}

func hostServiceTransportClient[C any](ctx context.Context, serviceName, target, token, binding string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return host.ServiceClient(ctx, serviceName, target, token, binding, transport, newClient)
}

func dialHostService(ctx context.Context, serviceName, target, token, binding string) (*grpc.ClientConn, error) {
	return host.DialService(ctx, serviceName, target, token, binding)
}

func hostServiceTarget(serviceName string) (string, string, error) {
	return host.Target(serviceName)
}

func hostServiceDialOptions(token string, binding string) []grpc.DialOption {
	return host.DialOptions(token, binding)
}

func cloneHostServiceRequest[M gproto.Message](serviceName string, req M, invocationToken, defaultIdempotencyKey string) (M, error) {
	var zero M
	if protoMessageIsNil(req) {
		return zero, fmt.Errorf("%s: request is nil", serviceName)
	}
	clone, ok := gproto.Clone(req).(M)
	if !ok {
		return zero, fmt.Errorf("%s: clone request %T", serviceName, req)
	}
	msg := clone.ProtoReflect()
	setProtoStringField(msg, "invocation_token", strings.TrimSpace(invocationToken), false)
	setProtoStringField(msg, "idempotency_key", strings.TrimSpace(defaultIdempotencyKey), true)
	return clone, nil
}

func protoMessageIsNil[M gproto.Message](msg M) bool {
	if any(msg) == nil {
		return true
	}
	value := reflect.ValueOf(msg)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func setProtoStringField(msg protoreflect.Message, name protoreflect.Name, value string, onlyIfEmpty bool) {
	if value == "" || !msg.IsValid() {
		return
	}
	field := msg.Descriptor().Fields().ByName(name)
	if field == nil || field.Kind() != protoreflect.StringKind {
		return
	}
	if onlyIfEmpty && msg.Get(field).String() != "" {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(value))
}
