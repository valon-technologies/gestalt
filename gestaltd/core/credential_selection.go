package core

import (
	"strings"
	"time"
)

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
	candidateInvalid := CredentialNeedsReconnect(candidate, now)
	currentInvalid := CredentialNeedsReconnect(current, now)
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
		key := AccountKeyFromMetadataJSON(credential.MetadataJSON)
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
