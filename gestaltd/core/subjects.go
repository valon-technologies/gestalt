package core

import (
	"fmt"
	"strings"
)

const SubjectKindServiceAccount = "service_account"

// Actor identifies who performed an action without credential-scoping fields.
type Actor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// TODO(#1823): Add first-class run-as subject and external-identity grant
// provisioning instead of relying on opaque subject IDs plus separate tuple
// seeding.
type RunAsSubject struct {
	SubjectID           string
	SubjectKind         string
	CredentialSubjectID string
	DisplayName         string
	AuthSource          string
}

func ParseSubjectID(subjectID string) (kind, id string, ok bool) {
	kind, id, ok = strings.Cut(strings.TrimSpace(subjectID), ":")
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	return kind, id, ok && kind != "" && id != ""
}

func NormalizeRunAsSubject(subject *RunAsSubject) *RunAsSubject {
	if subject == nil {
		return nil
	}
	out := &RunAsSubject{
		SubjectID:           strings.TrimSpace(subject.SubjectID),
		SubjectKind:         strings.TrimSpace(subject.SubjectKind),
		CredentialSubjectID: strings.TrimSpace(subject.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(subject.DisplayName),
		AuthSource:          strings.TrimSpace(subject.AuthSource),
	}
	if out.SubjectKind == "" {
		if kind, _, ok := ParseSubjectID(out.SubjectID); ok {
			out.SubjectKind = kind
		}
	}
	if out.CredentialSubjectID == "" {
		out.CredentialSubjectID = out.SubjectID
	}
	return out
}

func NormalizeServiceAccountSubject(subject *RunAsSubject, path string) (*RunAsSubject, error) {
	out := NormalizeRunAsSubject(subject)
	if out == nil {
		return nil, fmt.Errorf("%s is required", path)
	}
	if out.AuthSource == "" {
		out.AuthSource = "config"
	}
	if out.SubjectID == "" {
		return nil, fmt.Errorf("%s.id is required", path)
	}
	kind, _, ok := ParseSubjectID(out.SubjectID)
	if !ok {
		return nil, fmt.Errorf("%s.id %q must be a fully-qualified service_account subject", path, out.SubjectID)
	}
	if kind != SubjectKindServiceAccount {
		return nil, fmt.Errorf("%s.id %q must identify a service_account subject", path, out.SubjectID)
	}
	if out.SubjectKind == "" {
		out.SubjectKind = kind
	} else if out.SubjectKind != kind {
		return nil, fmt.Errorf("%s.kind %q must match subject.id kind %q", path, out.SubjectKind, kind)
	}
	return out, nil
}

func RunAsSubjectsEqual(left, right *RunAsSubject) bool {
	left = NormalizeRunAsSubject(left)
	right = NormalizeRunAsSubject(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SubjectID == right.SubjectID &&
		left.SubjectKind == right.SubjectKind &&
		left.CredentialSubjectID == right.CredentialSubjectID &&
		left.DisplayName == right.DisplayName &&
		left.AuthSource == right.AuthSource
}

func RunAsSubjectsMatchIdentity(left, right *RunAsSubject) bool {
	left = NormalizeRunAsSubject(left)
	right = NormalizeRunAsSubject(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SubjectID == right.SubjectID &&
		left.SubjectKind == right.SubjectKind
}
