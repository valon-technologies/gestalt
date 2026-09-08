package core

import (
	"strings"
	"time"
)

// CredentialAccountCandidate is the presentation-independent portion of a
// stored credential needed to choose one credential for one logical account.
// Status, disconnect, and invocation code all use this same policy.
type CredentialAccountCandidate struct {
	AccountKey     string
	ID             string
	Qualifier      string
	CreatedAt      time.Time
	NeedsReconnect bool
}

// PreferCredentialAccountCandidate applies the canonical ordering for one
// logical account. A usable credential wins over one needing reconnect, then
// the preferred instance, then the oldest stored credential, and finally
// stable IDs and qualifiers.
func PreferCredentialAccountCandidate(candidate, current CredentialAccountCandidate, preferred string) bool {
	candidateInvalid := candidate.NeedsReconnect
	currentInvalid := current.NeedsReconnect
	if candidateInvalid != currentInvalid {
		return !candidateInvalid
	}
	preferred = strings.TrimSpace(preferred)
	candidatePreferred := preferred != "" && strings.TrimSpace(candidate.Qualifier) == preferred
	currentPreferred := preferred != "" && strings.TrimSpace(current.Qualifier) == preferred
	if candidatePreferred != currentPreferred {
		return candidatePreferred
	}
	if candidate.CreatedAt.IsZero() != current.CreatedAt.IsZero() {
		return !candidate.CreatedAt.IsZero()
	}
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.Before(current.CreatedAt)
	}
	if candidate.ID != current.ID {
		return candidate.ID < current.ID
	}
	return candidate.Qualifier < current.Qualifier
}

// GroupCredentialAccountCandidates returns one canonical candidate per
// explicit account key. Keyless candidates remain distinct because their
// account identity cannot be proven safely.
func GroupCredentialAccountCandidates(candidates []CredentialAccountCandidate, preferred string) []CredentialAccountCandidate {
	normalized := append([]CredentialAccountCandidate(nil), candidates...)
	for index := range normalized {
		normalized[index].AccountKey = strings.TrimSpace(normalized[index].AccountKey)
	}
	indices := GroupCredentialAccountCandidateIndices(normalized, preferred)
	accounts := make([]CredentialAccountCandidate, 0, len(indices))
	for _, index := range indices {
		accounts = append(accounts, normalized[index])
	}
	return accounts
}

// GroupCredentialAccountCandidateGroups returns all record indexes belonging
// to each logical account. Keyless records remain singleton groups because
// their account identity cannot be proven safely.
func GroupCredentialAccountCandidateGroups(candidates []CredentialAccountCandidate) [][]int {
	groups := make([][]int, 0, len(candidates))
	seen := make(map[string]int, len(candidates))
	for index, candidate := range candidates {
		key := strings.TrimSpace(candidate.AccountKey)
		if key == "" {
			groups = append(groups, []int{index})
			continue
		}
		groupIndex, ok := seen[key]
		if !ok {
			seen[key] = len(groups)
			groups = append(groups, []int{index})
			continue
		}
		groups[groupIndex] = append(groups[groupIndex], index)
	}
	return groups
}

// GroupCredentialAccountCandidateIndices returns the indexes of the canonical
// candidate for each account. Returning indexes lets presentation layers keep
// their own data while still using the one account-selection policy.
func GroupCredentialAccountCandidateIndices(candidates []CredentialAccountCandidate, preferred string) []int {
	indices := make([]int, 0, len(candidates))
	for _, group := range GroupCredentialAccountCandidateGroups(candidates) {
		best := group[0]
		for _, index := range group[1:] {
			if PreferCredentialAccountCandidate(candidates[index], candidates[best], preferred) {
				best = index
			}
		}
		indices = append(indices, best)
	}
	return indices
}

// CredentialNeedsReconnect reports whether a credential's expired grant has
// already failed refresh and therefore cannot be selected as a usable account.
func CredentialNeedsReconnect(credential *ExternalCredential, now time.Time) bool {
	if credential == nil || credential.Grant == nil || credential.Grant.ExpiresAt == nil || credential.Grant.RefreshErrorCount <= 0 {
		return false
	}
	return !credential.Grant.ExpiresAt.After(now)
}

// PreferCredential applies the canonical ordering for credentials that
// represent one logical account. A usable credential wins over one needing
// reconnect, then the preferred instance, then the oldest stored credential,
// and finally stable IDs and qualifiers.
func PreferCredential(candidate, current *ExternalCredential, preferred string, now time.Time) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	return PreferCredentialAccountCandidate(credentialAccountCandidate(candidate, now), credentialAccountCandidate(current, now), preferred)
}

// ChooseCredential selects the canonical credential from a set already known
// to represent one logical account.
func ChooseCredential(credentials []*ExternalCredential, preferred string, now time.Time) *ExternalCredential {
	var chosen *ExternalCredential
	for _, credential := range credentials {
		if PreferCredential(credential, chosen, preferred, now) {
			chosen = credential
		}
	}
	return chosen
}

// GroupCredentialAccounts returns one canonical credential per explicit
// account key. Keyless credentials remain distinct because their account
// identity cannot be proven safely.
func GroupCredentialAccounts(credentials []*ExternalCredential, preferred string, now time.Time) []*ExternalCredential {
	accounts := make([]*ExternalCredential, 0, len(credentials))
	seen := make(map[string]int, len(credentials))
	for _, credential := range credentials {
		if credential == nil {
			continue
		}
		key := AccountKeyForCredential(credential)
		if key == "" {
			accounts = append(accounts, credential)
			continue
		}
		idx, ok := seen[key]
		if !ok {
			seen[key] = len(accounts)
			accounts = append(accounts, credential)
			continue
		}
		if PreferCredential(credential, accounts[idx], preferred, now) {
			accounts[idx] = credential
		}
	}
	return accounts
}

func credentialAccountCandidate(credential *ExternalCredential, now time.Time) CredentialAccountCandidate {
	if credential == nil {
		return CredentialAccountCandidate{}
	}
	return CredentialAccountCandidate{
		AccountKey:     AccountKeyForCredential(credential),
		ID:             credential.ID,
		Qualifier:      credential.Qualifier,
		CreatedAt:      credential.CreatedAt,
		NeedsReconnect: CredentialNeedsReconnect(credential, now),
	}
}

// ChooseCredentialInstance resolves the stored instance for a connection.
// A valid preferred instance wins; otherwise a sole usable logical account is
// selected, even when invalid sibling accounts remain. If no usable account
// exists, a sole logical account is returned so callers can surface its
// reconnect error. Empty or duplicate keyless credentials remain ambiguous.
func ChooseCredentialInstance(credentials []*ExternalCredential, preferred string, now time.Time) (string, bool) {
	accounts := GroupCredentialAccounts(credentials, preferred, now)
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, credential := range accounts {
			if strings.TrimSpace(credential.Qualifier) == preferred && !CredentialNeedsReconnect(credential, now) {
				return preferred, true
			}
		}
	}
	usable := make([]*ExternalCredential, 0, len(accounts))
	for _, credential := range accounts {
		if !CredentialNeedsReconnect(credential, now) {
			usable = append(usable, credential)
		}
	}
	if len(usable) == 1 {
		return strings.TrimSpace(usable[0].Qualifier), true
	}
	if len(usable) > 1 || len(accounts) != 1 {
		return "", false
	}
	return strings.TrimSpace(accounts[0].Qualifier), true
}
