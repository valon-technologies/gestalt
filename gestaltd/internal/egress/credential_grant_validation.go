package egress

import "fmt"

type CredentialGrantValidationInput struct {
	SubjectKind string
	SubjectID   string
	Operation   string
	Method      string
	Host        string
	PathPrefix  string
	AuthStyle   string
}

// ValidateCredentialGrant checks that a credential grant has at least one match
// criterion and a valid auth_style.
func ValidateCredentialGrant(g CredentialGrantValidationInput) error {
	if g.SubjectKind == "" && g.SubjectID == "" &&
		g.Operation == "" && g.Method == "" &&
		g.Host == "" && g.PathPrefix == "" {
		return fmt.Errorf("at least one match criterion is required")
	}
	if g.AuthStyle != "" {
		switch AuthStyle(g.AuthStyle) {
		case AuthStyleBearer, AuthStyleRaw, AuthStyleBasic, AuthStyleNone:
		default:
			return fmt.Errorf(
				"auth_style must be one of %q, %q, %q, %q, got %q",
				AuthStyleBearer, AuthStyleRaw, AuthStyleBasic, AuthStyleNone, g.AuthStyle,
			)
		}
	}
	return nil
}
