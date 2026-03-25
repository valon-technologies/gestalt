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
	Action PolicyAction
	MatchCriteria
}

type StaticPolicyEnforcer struct {
	DefaultAction PolicyAction
	Rules         []StaticPolicyRule
}

func (e StaticPolicyEnforcer) Evaluate(_ context.Context, input PolicyInput) error {
	for i := range e.Rules {
		if e.Rules[i].Matches(input.Subject, input.Target) {
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
