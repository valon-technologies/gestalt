package egress

import (
	"context"
	"errors"
	"fmt"
)

var ErrEgressDenied = errors.New("egress denied by policy")

type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

type StaticPolicyRule struct {
	Action      PolicyAction
	SubjectKind SubjectKind
	SubjectID   string
	Provider    string
	Operation   string
	Method      string
	Host        string
	PathPrefix  string
}

type StaticPolicyEnforcer struct {
	DefaultAction PolicyAction
	Rules         []StaticPolicyRule
}

func (e StaticPolicyEnforcer) Evaluate(_ context.Context, input PolicyInput) error {
	for i := range e.Rules {
		if matchesRule(&e.Rules[i], &input) {
			if e.Rules[i].Action == PolicyDeny {
				return fmt.Errorf("%w: %s %s", ErrEgressDenied, input.Target.Method, input.Target.Path)
			}
			return nil
		}
	}
	if e.DefaultAction == PolicyDeny {
		return fmt.Errorf("%w: %s %s", ErrEgressDenied, input.Target.Method, input.Target.Path)
	}
	return nil
}

func matchesRule(rule *StaticPolicyRule, input *PolicyInput) bool {
	if rule.SubjectKind != "" && rule.SubjectKind != input.Subject.Kind {
		return false
	}
	if rule.SubjectID != "" && rule.SubjectID != input.Subject.ID {
		return false
	}
	if rule.Provider != "" && rule.Provider != input.Target.Provider {
		return false
	}
	if rule.Operation != "" && rule.Operation != input.Target.Operation {
		return false
	}
	if rule.Method != "" && rule.Method != input.Target.Method {
		return false
	}
	if rule.Host != "" && rule.Host != input.Target.Host {
		return false
	}
	if rule.PathPrefix != "" && !MatchPathPrefix(rule.PathPrefix, input.Target.Path) {
		return false
	}
	return true
}
