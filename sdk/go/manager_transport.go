package gestalt

import (
	"context"
	"reflect"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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

func attachHostServiceAuth(req gproto.Message, reqCtx *proto.RequestContext) {
	if protoMessageIsNil(req) {
		return
	}
	msg := req.ProtoReflect()
	setProtoMessageField(msg, "context", cloneRequestContext(reqCtx))
}

func attachWorkflowContextField(ctx context.Context, req gproto.Message) error {
	workflow := WorkflowContextFromContext(ctx)
	if workflow == nil || protoMessageIsNil(req) {
		return nil
	}
	msg, err := structFromAny(workflow)
	if err != nil {
		return err
	}
	setProtoMessageField(req.ProtoReflect(), "workflow", msg)
	return nil
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

func setProtoMessageField(msg protoreflect.Message, name protoreflect.Name, value gproto.Message) {
	if protoMessageIsNil(value) || !msg.IsValid() {
		return
	}
	field := msg.Descriptor().Fields().ByName(name)
	if field == nil || field.Kind() != protoreflect.MessageKind {
		return
	}
	msg.Set(field, protoreflect.ValueOfMessage(value.ProtoReflect()))
}
