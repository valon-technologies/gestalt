package egress

import (
	"context"
	"fmt"
)

type DenyRuleRecord struct {
	SubjectKind string
	SubjectID   string
	Provider    string
	Operation   string
	Method      string
	Host        string
	PathPrefix  string
	ID          string
}

type DenyRuleLoader interface {
	LoadDenyRules(ctx context.Context) ([]DenyRuleRecord, error)
}

type CompositePolicyEnforcer struct {
	Static StaticPolicyEnforcer
	Store  DenyRuleLoader
}

func (c *CompositePolicyEnforcer) Evaluate(ctx context.Context, input PolicyInput) error {
	if err := c.Static.Evaluate(ctx, input); err != nil {
		return err
	}

	if c.Store == nil {
		return nil
	}

	rules, err := c.Store.LoadDenyRules(ctx)
	if err != nil {
		return fmt.Errorf("%w: deny rule lookup failed: %v", ErrEgressDenied, err)
	}

	for i := range rules {
		mc := MatchCriteria{
			SubjectKind: SubjectKind(rules[i].SubjectKind),
			SubjectID:   rules[i].SubjectID,
			Provider:    rules[i].Provider,
			Operation:   rules[i].Operation,
			Method:      rules[i].Method,
			Host:        rules[i].Host,
			PathPrefix:  rules[i].PathPrefix,
		}
		if mc.Matches(input.Subject, input.Target) {
			return fmt.Errorf("%w: matched deny rule %s", ErrEgressDenied, rules[i].ID)
		}
	}

	return nil
}
