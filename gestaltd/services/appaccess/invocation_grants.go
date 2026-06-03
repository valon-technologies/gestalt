package appaccess

import (
	"fmt"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const InvocationGrantAllApps = "*"

type InvocationGrant struct {
	AllOperations        bool
	Operations           map[string]core.ConnectionMode
	OperationDelegations map[string]InvocationDelegation
	Surfaces             map[string]struct{}
}

type InvocationGrants map[string]InvocationGrant

type InvocationDelegation struct {
	RunAs *core.RunAsSubject
}

type invocationGrantClaims struct {
	AllOperations        bool                                      `json:"all_operations,omitempty"`
	Operations           []string                                  `json:"operations,omitempty"`
	OperationModes       map[string]string                         `json:"operation_modes,omitempty"`
	OperationDelegations map[string]operationDelegationGrantClaims `json:"operation_delegation,omitempty"`
	Surfaces             []string                                  `json:"surfaces,omitempty"`
}

type operationDelegationGrantClaims struct {
	RunAs *runAsSubjectGrantClaims `json:"run_as,omitempty"`
}

type runAsSubjectGrantClaims struct {
	SubjectID           string `json:"subject_id,omitempty"`
	CredentialSubjectID string `json:"credential_subject_id,omitempty"`
}

func AllInvocationGrants() InvocationGrants {
	return InvocationGrants{
		InvocationGrantAllApps: {AllOperations: true},
	}
}

func decodeAppInvocationGrantProto(grants []*proto.AppInvocationGrant) (InvocationGrants, error) {
	if len(grants) == 0 {
		return nil, nil
	}
	decoded := make(InvocationGrants, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		app := strings.TrimSpace(grant.GetApp())
		if app == "" {
			return nil, fmt.Errorf("invocation grant app is required")
		}
		decodedGrant := decoded[app]
		entryHasAccess := grant.GetAllOperations()
		if grant.GetAllOperations() {
			decodedGrant.AllOperations = true
		}
		for _, operation := range grant.GetOperations() {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if decodedGrant.Operations == nil {
				decodedGrant.Operations = make(map[string]core.ConnectionMode)
			}
			decodedGrant.Operations[operation] = ""
			entryHasAccess = true
		}
		for _, surface := range grant.GetSurfaces() {
			surface = strings.ToLower(strings.TrimSpace(surface))
			if surface == "" {
				continue
			}
			if decodedGrant.Surfaces == nil {
				decodedGrant.Surfaces = make(map[string]struct{})
			}
			decodedGrant.Surfaces[surface] = struct{}{}
			entryHasAccess = true
		}
		if !entryHasAccess {
			return nil, fmt.Errorf("invocation grant for app %q must set all_operations, operations, or surfaces", app)
		}
		decoded[app] = decodedGrant
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	return decoded, nil
}

func cloneInvocationGrants(src InvocationGrants) InvocationGrants {
	if len(src) == 0 {
		return nil
	}
	out := make(InvocationGrants, len(src))
	for plugin, grant := range src {
		cloned := InvocationGrant{
			AllOperations: grant.AllOperations,
		}
		if len(grant.Operations) > 0 {
			cloned.Operations = make(map[string]core.ConnectionMode, len(grant.Operations))
			for operation, mode := range grant.Operations {
				cloned.Operations[operation] = mode
			}
		}
		if len(grant.OperationDelegations) > 0 {
			cloned.OperationDelegations = make(map[string]InvocationDelegation, len(grant.OperationDelegations))
			for operation, delegation := range grant.OperationDelegations {
				cloned.OperationDelegations[operation] = normalizeInvocationDelegation(delegation)
			}
		}
		if len(grant.Surfaces) > 0 {
			cloned.Surfaces = make(map[string]struct{}, len(grant.Surfaces))
			for surface := range grant.Surfaces {
				cloned.Surfaces[surface] = struct{}{}
			}
		}
		out[plugin] = cloned
	}
	return out
}

func CloneInvocationGrants(src InvocationGrants) InvocationGrants {
	return cloneInvocationGrants(src)
}

func encodeInvocationGrantClaims(src InvocationGrants) map[string]invocationGrantClaims {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]invocationGrantClaims, len(src))
	for plugin, grant := range src {
		out[plugin] = invocationGrantClaims{
			AllOperations:        grant.AllOperations,
			Operations:           sortedGrantKeys(grant.Operations),
			OperationModes:       grantOperationModes(grant.Operations),
			OperationDelegations: grantOperationDelegationsClaims(grant.OperationDelegations),
			Surfaces:             sortedGrantKeys(grant.Surfaces),
		}
	}
	return out
}

func decodeInvocationGrantClaims(src map[string]invocationGrantClaims) InvocationGrants {
	if len(src) == 0 {
		return nil
	}
	out := make(InvocationGrants, len(src))
	for app, grant := range src {
		app = strings.TrimSpace(app)
		if app == "" {
			continue
		}
		decoded := InvocationGrant{
			AllOperations: grant.AllOperations,
		}
		for _, operation := range grant.Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if decoded.Operations == nil {
				decoded.Operations = make(map[string]core.ConnectionMode)
			}
			decoded.Operations[operation] = ""
		}
		for operation, mode := range grant.OperationModes {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if _, ok := decoded.Operations[operation]; !ok {
				continue
			}
			decoded.Operations[operation] = core.NormalizeOptionalConnectionMode(core.ConnectionMode(mode))
		}
		for operation, delegationClaims := range grant.OperationDelegations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			delegation := delegationFromGrantClaims(delegationClaims)
			if invocationDelegationEmpty(delegation) {
				continue
			}
			if decoded.OperationDelegations == nil {
				decoded.OperationDelegations = make(map[string]InvocationDelegation)
			}
			decoded.OperationDelegations[operation] = delegation
		}
		for _, surface := range grant.Surfaces {
			surface = strings.ToLower(strings.TrimSpace(surface))
			if surface == "" {
				continue
			}
			if decoded.Surfaces == nil {
				decoded.Surfaces = make(map[string]struct{})
			}
			decoded.Surfaces[surface] = struct{}{}
		}
		if !decoded.AllOperations && len(decoded.Operations) == 0 && len(decoded.OperationDelegations) == 0 && len(decoded.Surfaces) == 0 {
			continue
		}
		out[app] = decoded
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func allowsOperation(grants InvocationGrants, plugin, operation string) bool {
	_, ok := EffectiveOperationProfile(grants, plugin, operation)
	return ok
}

func allowsSurface(grants InvocationGrants, plugin, surface string) bool {
	grant, ok := grantForApp(grants, plugin)
	if !ok {
		return false
	}
	if grant.AllOperations {
		return true
	}
	_, ok = grant.Surfaces[strings.ToLower(strings.TrimSpace(surface))]
	return ok
}

func sortedGrantKeys[V any](values map[string]V) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func grantOperationModes(operations map[string]core.ConnectionMode) map[string]string {
	if len(operations) == 0 {
		return nil
	}
	out := make(map[string]string, len(operations))
	for operation, mode := range operations {
		if mode != "" {
			out[operation] = string(mode)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func grantOperationDelegationsClaims(delegations map[string]InvocationDelegation) map[string]operationDelegationGrantClaims {
	if len(delegations) == 0 {
		return nil
	}
	out := make(map[string]operationDelegationGrantClaims, len(delegations))
	for operation, delegation := range delegations {
		delegation = normalizeInvocationDelegation(delegation)
		if invocationDelegationEmpty(delegation) {
			continue
		}
		claims := operationDelegationGrantClaims{}
		if delegation.RunAs != nil {
			claims.RunAs = &runAsSubjectGrantClaims{
				SubjectID:           delegation.RunAs.SubjectID,
				CredentialSubjectID: delegation.RunAs.CredentialSubjectID,
			}
		}
		out[operation] = claims
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func delegationFromGrantClaims(claims operationDelegationGrantClaims) InvocationDelegation {
	var runAs *core.RunAsSubject
	if claims.RunAs != nil {
		runAs = core.NormalizeRunAsSubject(&core.RunAsSubject{
			SubjectID:           claims.RunAs.SubjectID,
			CredentialSubjectID: claims.RunAs.CredentialSubjectID,
		})
	}
	return normalizeInvocationDelegation(InvocationDelegation{
		RunAs: runAs,
	})
}
