package core

import "strings"

// TODO(#1823): Add first-class run-as subject and external-identity grant
// provisioning instead of relying on opaque subject IDs plus separate tuple
// seeding.
type RunAsSubject struct {
	SubjectID           string
	CredentialSubjectID string
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
		CredentialSubjectID: strings.TrimSpace(subject.CredentialSubjectID),
	}
	if out.CredentialSubjectID == "" {
		out.CredentialSubjectID = out.SubjectID
	}
	return out
}

func RunAsSubjectsEqual(left, right *RunAsSubject) bool {
	left = NormalizeRunAsSubject(left)
	right = NormalizeRunAsSubject(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SubjectID == right.SubjectID &&
		left.CredentialSubjectID == right.CredentialSubjectID
}

func RunAsSubjectsMatchIdentity(left, right *RunAsSubject) bool {
	return RunAsSubjectsEqual(left, right)
}
