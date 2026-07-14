package core

import "strings"

// GestaltdSubjectID is gestaltd's own workload-identity subject, the owner of
// ClientInfo credentials minted via RFC 7591 dynamic client registration.
const GestaltdSubjectID = "system:gestaltd"

// RunAsSubject is retained for agent and app-invoke delegation. Workflow
// definitions store their run-as identity as a plain subject ID string.
type RunAsSubject struct {
	SubjectID string
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
	return &RunAsSubject{
		SubjectID: strings.TrimSpace(subject.SubjectID),
	}
}

func RunAsSubjectsEqual(left, right *RunAsSubject) bool {
	left = NormalizeRunAsSubject(left)
	right = NormalizeRunAsSubject(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SubjectID == right.SubjectID
}

func RunAsSubjectsMatchIdentity(left, right *RunAsSubject) bool {
	return RunAsSubjectsEqual(left, right)
}
