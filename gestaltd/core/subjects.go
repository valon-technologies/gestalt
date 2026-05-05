package core

import "strings"

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
