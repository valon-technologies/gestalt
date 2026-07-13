// Package validate builds the normalized model from service descriptors,
// classifying every construct against the shared-semantics allowlist. The
// classification lives here and nowhere else; emitters must not reinterpret
// it. Constructs outside the allowlist produce diagnostics naming the proto
// source location, and all diagnostics are collected before failure.
package validate

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/diag"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

const (
	structFullName    = "google.protobuf.Struct"
	valueFullName     = "google.protobuf.Value"
	nullValueFullName = "google.protobuf.NullValue"
	timestampFullName = "google.protobuf.Timestamp"
	durationFullName  = "google.protobuf.Duration"
	emptyFullName     = "google.protobuf.Empty"
	rpcStatusFullName = "google.rpc.Status"

	wellKnownPrefix = "google.protobuf."
)

// Build constructs the normalized model for the given services. The file
// registry resolves SDK annotation extensions, which are defined in the
// schema itself. PathPrefix is prepended to descriptor file paths in
// diagnostics (descriptor paths are relative to the proto module root).
func Build(files *protoregistry.Files, services []protoreflect.ServiceDescriptor, pathPrefix string) (*model.Schema, *diag.List) {
	b := &builder{
		pathPrefix:    pathPrefix,
		ann:           newAnnotations(files),
		diags:         &diag.List{},
		messages:      map[protoreflect.FullName]*model.Message{},
		enums:         map[protoreflect.FullName]*model.Enum{},
		files:         map[string]bool{},
		providerKinds: map[model.ProviderKind]protoreflect.FullName{},
	}
	schema := &model.Schema{}
	for _, sd := range services {
		schema.Services = append(schema.Services, b.service(sd))
	}
	// Provider annotations are part of the public SDK contract even though
	// their enums are referenced only from descriptor options, not message
	// fields. Keep both enum definitions in the immutable plan so every
	// language surface exposes the same annotation vocabulary.
	for _, name := range []protoreflect.FullName{
		"gestalt.provider.v1.ProviderKind",
		"gestalt.provider.v1.ProviderInput",
	} {
		if descriptor, err := files.FindDescriptorByName(name); err == nil {
			if enum, ok := descriptor.(protoreflect.EnumDescriptor); ok {
				b.enum(enum)
			}
		}
	}
	for _, m := range b.messages {
		schema.Messages = append(schema.Messages, m)
	}
	sort.Slice(schema.Messages, func(i, j int) bool { return schema.Messages[i].FullName < schema.Messages[j].FullName })
	for _, e := range b.enums {
		schema.Enums = append(schema.Enums, e)
	}
	sort.Slice(schema.Enums, func(i, j int) bool { return schema.Enums[i].FullName < schema.Enums[j].FullName })
	return schema, b.diags
}

type builder struct {
	pathPrefix    string
	ann           *annotations
	diags         *diag.List
	messages      map[protoreflect.FullName]*model.Message
	enums         map[protoreflect.FullName]*model.Enum
	files         map[string]bool
	providerKinds map[model.ProviderKind]protoreflect.FullName
}

func (b *builder) service(sd protoreflect.ServiceDescriptor) *model.Service {
	b.file(sd.ParentFile())
	svc := &model.Service{
		Doc:       docFor(sd),
		FullName:  string(sd.FullName()),
		Name:      string(sd.Name()),
		ProtoFile: b.pathPrefix + sd.ParentFile().Path(),
	}
	methods := sd.Methods()
	for i := 0; i < methods.Len(); i++ {
		svc.Methods = append(svc.Methods, b.method(methods.Get(i)))
	}
	b.serviceAnnotations(sd, svc)
	return svc
}

func (b *builder) method(md protoreflect.MethodDescriptor) *model.Method {
	m := &model.Method{Doc: docFor(md), Name: string(md.Name())}
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		m.Stream = model.Bidi
	case md.IsStreamingClient():
		m.Stream = model.ClientStream
	case md.IsStreamingServer():
		m.Stream = model.ServerStream
	default:
		m.Stream = model.Unary
	}
	if md.Input().FullName() == emptyFullName {
		m.InputIsEmpty = true
	} else {
		m.Input = b.message(md.Input())
	}
	if md.Output().FullName() == emptyFullName {
		m.OutputIsEmpty = true
	} else {
		m.Output = b.message(md.Output())
	}
	b.methodAnnotations(md, m)
	return m
}

// file validates file-level constraints once per file.
func (b *builder) file(fd protoreflect.FileDescriptor) {
	if b.files[fd.Path()] {
		return
	}
	b.files[fd.Path()] = true
	if fd.Syntax() != protoreflect.Proto3 {
		b.add(fd, "file", fmt.Sprintf("unsupported syntax %s: sdkgen supports proto3 only", fd.Syntax()))
	}
}

func (b *builder) message(md protoreflect.MessageDescriptor) *model.Message {
	if m, ok := b.messages[md.FullName()]; ok {
		return m
	}
	b.file(md.ParentFile())
	m := &model.Message{
		Doc:       docFor(md),
		FullName:  string(md.FullName()),
		Name:      string(md.Name()),
		ProtoFile: b.pathPrefix + md.ParentFile().Path(),
	}
	// Register before walking fields so recursive messages terminate.
	b.messages[md.FullName()] = m

	if md.ExtensionRanges().Len() > 0 {
		b.add(md, "message", "unsupported construct: extension range")
	}

	oneofIndex := map[protoreflect.FullName]int{}
	oneofs := md.Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		od := oneofs.Get(i)
		if od.IsSynthetic() {
			continue // proto3 optional mechanics, modeled as explicit presence
		}
		oneofIndex[od.FullName()] = len(m.Oneofs)
		m.Oneofs = append(m.Oneofs, &model.Oneof{Name: string(od.Name())})
	}

	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		f := b.field(fd)
		f.OneofIndex = -1
		if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
			idx := oneofIndex[od.FullName()]
			f.OneofIndex = idx
			m.Oneofs[idx].FieldNumbers = append(m.Oneofs[idx].FieldNumbers, f.Number)
		}
		m.Fields = append(m.Fields, f)
	}
	b.messageAnnotations(md, m)
	return m
}

func (b *builder) field(fd protoreflect.FieldDescriptor) *model.Field {
	f := &model.Field{
		Doc:      docFor(fd),
		Name:     string(fd.Name()),
		JSONName: fd.JSONName(),
		Number:   int32(fd.Number()),
	}
	switch {
	case fd.IsMap():
		f.Kind = model.KindMap
		f.MapKey = scalarType(fd.MapKey().Kind())
		if f.MapKey == model.ScalarInvalid {
			b.add(fd, "field", fmt.Sprintf("unsupported map key kind %s", fd.MapKey().Kind()))
		}
		f.MapValue = b.typeRef(fd.MapValue())
	case fd.IsList():
		f.Kind = model.KindRepeated
		f.Elem = b.typeRef(fd)
	default:
		b.singular(fd, f)
		if fd.HasPresence() {
			f.Presence = model.ExplicitPresence
		}
	}
	return f
}

// singular classifies a non-repeated, non-map field's value type into f.
func (b *builder) singular(fd protoreflect.FieldDescriptor, f *model.Field) {
	ref := b.typeRef(fd)
	f.Kind = ref.Kind
	f.Scalar = ref.Scalar
	f.Message = ref.Message
	f.Enum = ref.Enum
}

// typeRef classifies a value type: a singular field, a repeated element, or a
// map value. This is the single kind-switch that implements the allowlist.
func (b *builder) typeRef(fd protoreflect.FieldDescriptor) *model.TypeRef {
	switch fd.Kind() {
	case protoreflect.MessageKind:
		md := fd.Message()
		switch md.FullName() {
		case structFullName:
			return &model.TypeRef{Kind: model.KindJSONStruct}
		case valueFullName:
			return &model.TypeRef{Kind: model.KindJSONValue}
		case timestampFullName:
			return &model.TypeRef{Kind: model.KindTimestamp}
		case durationFullName:
			return &model.TypeRef{Kind: model.KindDuration}
		case emptyFullName:
			return &model.TypeRef{Kind: model.KindUnit}
		case rpcStatusFullName:
			// Maps to the canonical SDK error; terminal, so its Any-typed
			// details field is never classified.
			return &model.TypeRef{Kind: model.KindRPCStatus}
		}
		if isWellKnown(md.FullName()) {
			b.add(fd, "field", fmt.Sprintf("unsupported well-known type %s", md.FullName()))
			return &model.TypeRef{Kind: model.KindInvalid}
		}
		m := b.message(md)
		return &model.TypeRef{Kind: model.KindMessage, Message: m.FullName}
	case protoreflect.EnumKind:
		ed := fd.Enum()
		if ed.FullName() == nullValueFullName {
			return &model.TypeRef{Kind: model.KindJSONNull}
		}
		if isWellKnown(ed.FullName()) {
			b.add(fd, "field", fmt.Sprintf("unsupported well-known type %s", ed.FullName()))
			return &model.TypeRef{Kind: model.KindInvalid}
		}
		e := b.enum(ed)
		return &model.TypeRef{Kind: model.KindEnum, Enum: e.FullName}
	case protoreflect.BytesKind:
		return &model.TypeRef{Kind: model.KindBytes}
	case protoreflect.GroupKind:
		b.add(fd, "field", "unsupported construct: group")
		return &model.TypeRef{Kind: model.KindInvalid}
	default:
		if s := scalarType(fd.Kind()); s != model.ScalarInvalid {
			return &model.TypeRef{Kind: model.KindScalar, Scalar: s}
		}
		b.add(fd, "field", fmt.Sprintf("unsupported field kind %s", fd.Kind()))
		return &model.TypeRef{Kind: model.KindInvalid}
	}
}

func (b *builder) enum(ed protoreflect.EnumDescriptor) *model.Enum {
	if e, ok := b.enums[ed.FullName()]; ok {
		return e
	}
	b.file(ed.ParentFile())
	e := &model.Enum{
		Doc:       docFor(ed),
		FullName:  string(ed.FullName()),
		Name:      string(ed.Name()),
		ProtoFile: b.pathPrefix + ed.ParentFile().Path(),
	}
	values := ed.Values()
	for i := 0; i < values.Len(); i++ {
		vd := values.Get(i)
		e.Values = append(e.Values, model.EnumValue{Doc: docFor(vd), Name: string(vd.Name()), Number: int32(vd.Number())})
	}
	b.enums[ed.FullName()] = e
	return e
}

// docFor returns the normalized leading proto comment for a descriptor: each
// line's single leading space stripped, trailing whitespace removed, and the
// block trimmed of blank edges.
func docFor(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	if loc.LeadingComments == "" {
		return ""
	}
	lines := strings.Split(loc.LeadingComments, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		lines[i] = strings.TrimPrefix(line, " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (b *builder) add(d protoreflect.Descriptor, kind, detail string) {
	file := d.ParentFile()
	loc := file.SourceLocations().ByDescriptor(d)
	b.diags.Add(diag.Diagnostic{
		ProtoFile: b.pathPrefix + file.Path(),
		Line:      loc.StartLine + 1,
		Col:       loc.StartColumn + 1,
		Kind:      kind,
		FullName:  string(d.FullName()),
		Detail:    detail,
	})
}

func isWellKnown(name protoreflect.FullName) bool {
	const prefix = wellKnownPrefix
	return len(name) > len(prefix) && string(name[:len(prefix)]) == prefix
}

func scalarType(k protoreflect.Kind) model.ScalarType {
	switch k {
	case protoreflect.BoolKind:
		return model.ScalarBool
	case protoreflect.Int32Kind:
		return model.ScalarInt32
	case protoreflect.Sint32Kind:
		return model.ScalarSint32
	case protoreflect.Uint32Kind:
		return model.ScalarUint32
	case protoreflect.Int64Kind:
		return model.ScalarInt64
	case protoreflect.Sint64Kind:
		return model.ScalarSint64
	case protoreflect.Uint64Kind:
		return model.ScalarUint64
	case protoreflect.Sfixed32Kind:
		return model.ScalarSfixed32
	case protoreflect.Fixed32Kind:
		return model.ScalarFixed32
	case protoreflect.Sfixed64Kind:
		return model.ScalarSfixed64
	case protoreflect.Fixed64Kind:
		return model.ScalarFixed64
	case protoreflect.FloatKind:
		return model.ScalarFloat
	case protoreflect.DoubleKind:
		return model.ScalarDouble
	case protoreflect.StringKind:
		return model.ScalarString
	default:
		return model.ScalarInvalid
	}
}
