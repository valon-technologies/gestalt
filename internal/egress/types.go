package egress

// SubjectKind identifies the caller shape for an outbound egress request.
type SubjectKind string

const (
	SubjectUser     SubjectKind = "user"
	SubjectIdentity SubjectKind = "identity"
	SubjectSystem   SubjectKind = "system"
)

// Subject identifies who initiated an outbound request.
type Subject struct {
	Kind SubjectKind
	ID   string
}

// Target describes the destination and logical source for an outbound request.
type Target struct {
	Provider  string
	Instance  string
	Operation string
	Method    string
	Host      string
	Path      string
}
