// Package model is the normalized provider SDK model: pure data shared by
// every language emitter. No protoreflect types cross this boundary, which is
// what keeps descriptor classification in one place.
package model

// StreamKind classifies an RPC's streaming shape.
type StreamKind int

const (
	Unary StreamKind = iota
	ServerStream
	ClientStream
	Bidi
)

func (k StreamKind) String() string {
	switch k {
	case Unary:
		return "unary"
	case ServerStream:
		return "server-stream"
	case ClientStream:
		return "client-stream"
	case Bidi:
		return "bidi"
	default:
		return "unknown"
	}
}

// Presence reports whether an unset field is distinguishable from one set to
// its default value.
type Presence int

const (
	NoPresence Presence = iota
	ExplicitPresence
)

// SemanticKind is the shared-semantics classification of a field or type
// reference. Every emitter maps these kinds to native types; nothing outside
// this set survives validation.
type SemanticKind int

const (
	KindInvalid SemanticKind = iota
	KindScalar
	KindBytes
	KindEnum
	KindMessage
	KindRepeated
	KindMap
	KindJSONStruct // google.protobuf.Struct
	KindJSONValue  // google.protobuf.Value
	KindJSONNull   // google.protobuf.NullValue
	KindTimestamp  // google.protobuf.Timestamp
	KindDuration   // google.protobuf.Duration
	KindUnit       // google.protobuf.Empty as a field: a unit variant
	KindRPCStatus  // google.rpc.Status: the canonical SDK error
)

// ScalarType identifies a proto scalar for KindScalar fields and refs.
type ScalarType int

const (
	ScalarInvalid ScalarType = iota
	ScalarBool
	ScalarInt32
	ScalarSint32
	ScalarUint32
	ScalarInt64
	ScalarSint64
	ScalarUint64
	ScalarSfixed32
	ScalarFixed32
	ScalarSfixed64
	ScalarFixed64
	ScalarFloat
	ScalarDouble
	ScalarString
)

// Schema is the root of the normalized model.
type Schema struct {
	Services []*Service
	Messages []*Message // transitive closure of method I/O, sorted by full name
	Enums    []*Enum    // sorted by full name
}

type Service struct {
	// Doc is the leading proto comment, normalized; empty when absent.
	Doc       string
	FullName  string
	Name      string
	ProtoFile string
	Methods   []*Method

	// HostBinding is the host-service binding name from the host_binding
	// annotation; empty when unannotated.
	HostBinding string
}

type Method struct {
	Doc           string
	Name          string
	Stream        StreamKind
	Input         *Message // nil when InputIsEmpty
	InputIsEmpty  bool     // google.protobuf.Empty request: no public request argument
	Output        *Message // nil when OutputIsEmpty
	OutputIsEmpty bool

	// Signature lists request fields promoted to parameters, from the
	// signature annotation. Presence fields come last.
	Signature []string
	// Framing declares the header-then-payload protocol for this method's
	// streaming side, from the framing annotation.
	Framing *Framing
	// JsonResult declares that this method's result carries the standard
	// JSON operation envelope, from the json_result annotation.
	JsonResult *JsonResult
}

// JsonResult names the output message's status and body fields holding an
// HTTP-shaped JSON operation result.
type JsonResult struct {
	Status string
	Body   string
}

// Framing identifies the frame oneof and its header and chunk variants by
// proto field name.
type Framing struct {
	Oneof       string
	HeaderField string
	ChunkField  string
}

type Message struct {
	Doc       string
	FullName  string
	Name      string
	ProtoFile string
	Fields    []*Field // declaration order
	Oneofs    []*Oneof // real oneofs only; synthetic optional-scalar oneofs are presence

	// OptionalResult, Keyed, and Unwrap collapse this message at API
	// boundaries, from the matching annotations. At most one is set.
	OptionalResult *OptionalResult
	Keyed          *Keyed
	Unwrap         string
}

// OptionalResult declares that Value is meaningful only when the bool Guard
// field is true; the message collapses to an optional value.
type OptionalResult struct {
	Guard string
	Value string
}

// Keyed declares that the repeated Entries field is a collection keyed by
// Key; entries whose Present field is false are omitted, and the message
// collapses to a native map from Key to Value.
type Keyed struct {
	Entries string
	Key     string
	Present string
	Value   string
}

type Field struct {
	Doc      string
	Name     string
	JSONName string
	Number   int32
	Kind     SemanticKind
	Presence Presence

	Scalar     ScalarType // KindScalar
	Elem       *TypeRef   // KindRepeated
	MapKey     ScalarType // KindMap
	MapValue   *TypeRef   // KindMap
	Message    string     // KindMessage: full name
	Enum       string     // KindEnum: full name
	OneofIndex int        // index into Message.Oneofs, -1 when not a oneof member
}

// TypeRef classifies a repeated element or map value.
type TypeRef struct {
	Kind    SemanticKind
	Scalar  ScalarType
	Message string
	Enum    string
}

type Oneof struct {
	Name         string
	FieldNumbers []int32
}

// Enum is an open proto3 enum; unknown numeric values are preserved by SDKs.
type Enum struct {
	Doc       string
	FullName  string
	Name      string
	ProtoFile string
	Values    []EnumValue
}

type EnumValue struct {
	Doc    string
	Name   string
	Number int32
}
