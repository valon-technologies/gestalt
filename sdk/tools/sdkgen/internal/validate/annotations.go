package validate

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// SDK annotation extensions, defined in sdk/proto/v1/annotations.proto. The
// extension definitions live in the schema itself, so option values are
// decoded dynamically against the descriptor image rather than against
// compiled-in stubs.
const (
	extSignature      = "gestalt.provider.v1.signature"
	extInitial        = "gestalt.provider.v1.initial"
	extOptionalResult = "gestalt.provider.v1.optional_result"
	extKeyed          = "gestalt.provider.v1.keyed"
	extUnwrap         = "gestalt.provider.v1.unwrap"
	extJsonResult     = "gestalt.provider.v1.json_result"
	extOptionalSig    = "gestalt.provider.v1.optional_signature"
	extHostBinding    = "gestalt.provider.v1.host_binding"
	extProvider       = "gestalt.provider.v1.provider"
)

type annotations struct {
	resolver *dynamicpb.Types
}

func newAnnotations(files *protoregistry.Files) *annotations {
	return &annotations{resolver: dynamicpb.NewTypes(files)}
}

// extensions re-decodes an options message with the image's own types so SDK
// annotation extensions are visible, returning extension values by full name.
// Extensions the image does not define stay in unknown fields harmlessly.
func (a *annotations) extensions(opts proto.Message) map[string]protoreflect.Value {
	if opts == nil || !opts.ProtoReflect().IsValid() {
		return nil
	}
	raw, err := proto.Marshal(opts)
	if err != nil || len(raw) == 0 {
		return nil
	}
	dyn := dynamicpb.NewMessage(opts.ProtoReflect().Descriptor())
	if err := (proto.UnmarshalOptions{Resolver: a.resolver}).Unmarshal(raw, dyn); err != nil {
		return nil
	}
	out := map[string]protoreflect.Value{}
	dyn.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsExtension() {
			out[string(fd.FullName())] = v
		}
		return true
	})
	return out
}

func subField(v protoreflect.Value, name string) string {
	msg := v.Message()
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return ""
	}
	return msg.Get(fd).String()
}

// serviceAnnotations applies service-level annotations.
func (b *builder) serviceAnnotations(sd protoreflect.ServiceDescriptor, svc *model.Service) {
	exts := b.ann.extensions(sd.Options())
	if v, ok := exts[extHostBinding]; ok {
		svc.HostBinding = v.String()
		if svc.HostBinding == "" {
			b.add(sd, "service", "host_binding annotation must not be empty")
		}
	}
	if v, ok := exts[extProvider]; ok && v.Bool() {
		svc.Provider = true
		for _, m := range svc.Methods {
			if m.Stream != model.Unary {
				b.add(sd, "service", fmt.Sprintf("provider annotation does not yet support streaming method %s", m.Name))
			}
		}
	}
}

// methodAnnotations validates and applies method-level annotations.
func (b *builder) methodAnnotations(md protoreflect.MethodDescriptor, m *model.Method) {
	exts := b.ann.extensions(md.Options())

	if v, ok := exts[extInitial]; ok {
		header := subField(v, "header")
		chunk := subField(v, "chunk")
		var frames *model.Message
		switch m.Stream {
		case model.ServerStream:
			frames = m.Output
		case model.ClientStream:
			frames = m.Input
		default:
			b.add(md, "method", "initial annotation requires a server- or client-streaming method")
			return
		}
		oneof := oneofContaining(frames, header, chunk)
		if oneof == "" {
			b.add(md, "method", fmt.Sprintf("initial requires oneof variants %q and %q on %s", header, chunk, frames.FullName))
			return
		}
		m.Initial = &model.Initial{Oneof: oneof, HeaderField: header, ChunkField: chunk}
	}

	if v, ok := exts[extJsonResult]; ok {
		status := subField(v, "status")
		body := subField(v, "body")
		switch {
		case m.Stream != model.Unary:
			b.add(md, "method", "json_result annotation requires a unary method")
		case m.Initial != nil:
			b.add(md, "method", "json_result and initial annotations cannot be combined")
		case m.Output == nil:
			b.add(md, "method", "json_result annotation requires a response message")
		case m.Output.OptionalResult != nil || m.Output.Keyed != nil || m.Output.Unwrap != "":
			b.add(md, "method", fmt.Sprintf("json_result cannot collapse %s, which already carries a response collapse", m.Output.FullName))
		default:
			statusField := fieldByName(m.Output, status)
			bodyField := fieldByName(m.Output, body)
			switch {
			case statusField == nil || statusField.Kind != model.KindScalar || statusField.Scalar != model.ScalarInt32 || statusField.Presence != model.NoPresence:
				b.add(md, "method", fmt.Sprintf("json_result status %q must be a plain int32 field of %s", status, m.Output.FullName))
			case bodyField == nil || bodyField.Kind != model.KindBytes:
				b.add(md, "method", fmt.Sprintf("json_result body %q must be a bytes field of %s", body, m.Output.FullName))
			default:
				m.JsonResult = &model.JsonResult{Status: status, Body: body}
			}
		}
	}

	if v, ok := exts[extSignature]; ok {
		if m.Initial != nil {
			b.add(md, "method", "signature and initial annotations cannot be combined")
		}
		if m.Stream != model.Unary {
			b.add(md, "method", "signature annotation requires a unary method")
			return
		}
		if m.InputIsEmpty {
			b.add(md, "method", "signature annotation requires a request message")
			return
		}
		list := v.List()
		seenPresence := false
		for i := 0; i < list.Len(); i++ {
			name := list.Get(i).String()
			field := fieldByName(m.Input, name)
			if field == nil {
				b.add(md, "method", fmt.Sprintf("signature names unknown request field %q", name))
				continue
			}
			if field.Presence == model.ExplicitPresence {
				seenPresence = true
			} else if seenPresence {
				b.add(md, "method", fmt.Sprintf("signature field %q without presence must come before optional fields", name))
			}
			m.Signature = append(m.Signature, name)
		}
	}

	if v, ok := exts[extOptionalSig]; ok {
		switch {
		case m.Initial != nil:
			b.add(md, "method", "optional_signature and initial annotations cannot be combined")
		case m.Stream != model.Unary:
			b.add(md, "method", "optional_signature annotation requires a unary method")
		case m.InputIsEmpty:
			b.add(md, "method", "optional_signature annotation requires a request message")
		default:
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				name := list.Get(i).String()
				if fieldByName(m.Input, name) == nil {
					b.add(md, "method", fmt.Sprintf("optional_signature names unknown request field %q", name))
					continue
				}
				if slices.Contains(m.Signature, name) {
					b.add(md, "method", fmt.Sprintf("optional_signature field %q repeats the signature list", name))
					continue
				}
				m.OptionalSignature = append(m.OptionalSignature, name)
			}
		}
	}
}

// messageAnnotations validates and applies message-level annotations.
func (b *builder) messageAnnotations(md protoreflect.MessageDescriptor, m *model.Message) {
	exts := b.ann.extensions(md.Options())

	collapses := 0
	if v, ok := exts[extOptionalResult]; ok {
		collapses++
		guard := subField(v, "guard")
		value := subField(v, "value")
		if f := fieldByName(m, guard); f == nil || f.Kind != model.KindScalar || f.Scalar != model.ScalarBool || f.Presence != model.NoPresence {
			b.add(md, "message", fmt.Sprintf("optional_result guard %q must be a plain bool field", guard))
		} else if fieldByName(m, value) == nil || guard == value {
			b.add(md, "message", fmt.Sprintf("optional_result value %q must name a sibling field", value))
		} else {
			m.OptionalResult = &model.OptionalResult{Guard: guard, Value: value}
		}
	}
	if v, ok := exts[extKeyed]; ok {
		collapses++
		keyed := &model.Keyed{
			Entries: subField(v, "entries"),
			Key:     subField(v, "key"),
			Present: subField(v, "present"),
			Value:   subField(v, "value"),
		}
		entries := fieldByName(m, keyed.Entries)
		if entries == nil || entries.Kind != model.KindRepeated || entries.Elem.Kind != model.KindMessage {
			b.add(md, "message", fmt.Sprintf("keyed entries %q must be a repeated message field", keyed.Entries))
		} else {
			entry := b.messages[protoreflect.FullName(entries.Elem.Message)]
			key := fieldByName(entry, keyed.Key)
			present := fieldByName(entry, keyed.Present)
			value := fieldByName(entry, keyed.Value)
			switch {
			case key == nil || key.Kind != model.KindScalar:
				b.add(md, "message", fmt.Sprintf("keyed key %q must be a scalar field of %s", keyed.Key, entry.FullName))
			case present == nil || present.Kind != model.KindScalar || present.Scalar != model.ScalarBool:
				b.add(md, "message", fmt.Sprintf("keyed present %q must be a bool field of %s", keyed.Present, entry.FullName))
			case value == nil:
				b.add(md, "message", fmt.Sprintf("keyed value %q must name a field of %s", keyed.Value, entry.FullName))
			default:
				m.Keyed = keyed
			}
		}
	}
	if v, ok := exts[extUnwrap]; ok {
		collapses++
		name := v.String()
		if fieldByName(m, name) == nil {
			b.add(md, "message", fmt.Sprintf("unwrap names unknown field %q", name))
		} else {
			m.Unwrap = name
		}
	}
	if collapses > 1 {
		b.add(md, "message", "optional_result, keyed, and unwrap are mutually exclusive")
	}
}

func fieldByName(m *model.Message, name string) *model.Field {
	if m == nil || name == "" {
		return nil
	}
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// oneofContaining returns the name of the oneof holding both fields, or "".
func oneofContaining(m *model.Message, header, chunk string) string {
	h := fieldByName(m, header)
	c := fieldByName(m, chunk)
	if h == nil || c == nil || h.OneofIndex < 0 || h.OneofIndex != c.OneofIndex {
		return ""
	}
	return m.Oneofs[h.OneofIndex].Name
}
