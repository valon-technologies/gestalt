package protoutil

import (
	"strings"

	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// SetProviderNameIfEmpty sets provider_name on a protobuf request when absent.
func SetProviderNameIfEmpty(req gproto.Message, name string) {
	name = strings.TrimSpace(name)
	if req == nil || name == "" {
		return
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return
	}
	if strings.TrimSpace(msg.Get(field).String()) != "" {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(name))
}
