package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func applyPatch(current persistedUser, request patchRequest) (persistedUser, error) {
	if err := validatePatchSchemas(request.Schemas); err != nil {
		return persistedUser{}, err
	}
	if len(request.Operations) == 0 {
		return persistedUser{}, invalid("PATCH Operations must not be empty")
	}
	result := current
	for i, operation := range request.Operations {
		op := strings.ToLower(strings.TrimSpace(operation.Op))
		if op != "add" && op != "replace" && op != "remove" {
			return persistedUser{}, invalid(fmt.Sprintf("Operations[%d].op is unsupported", i))
		}
		path := normalizePatchPath(operation.Path, UserSchemaURN)
		if path == "" {
			if op == "remove" {
				return persistedUser{}, invalid("remove requires a path")
			}
			var input userInput
			if err := json.Unmarshal(operation.Value, &input); err != nil {
				return persistedUser{}, invalid("pathless PATCH value must be an object")
			}
			if err := mergePatchInput(&result, input, op); err != nil {
				return persistedUser{}, err
			}
			continue
		}
		if err := applyPathPatch(&result, op, path, operation.Value); err != nil {
			var scimErr *Error
			if errors.As(err, &scimErr) {
				return persistedUser{}, &Error{Status: scimErr.Status, SCIMType: scimErr.SCIMType, Detail: fmt.Sprintf("Operations[%d]: %s", i, scimErr.Detail)}
			}
			return persistedUser{}, invalid(fmt.Sprintf("Operations[%d]: %v", i, err))
		}
	}
	if strings.TrimSpace(result.UserName) == "" {
		return persistedUser{}, invalid("userName is required")
	}
	if err := validateEmails(result.Emails); err != nil {
		return persistedUser{}, err
	}
	return result, nil
}

func mergePatchInput(result *persistedUser, input userInput, op string) error {
	if input.ExternalID != nil {
		result.ExternalID = *input.ExternalID
	}
	if input.UserName != nil {
		result.UserName = *input.UserName
	}
	if input.Active != nil {
		result.Active = *input.Active
	}
	if input.DisplayName != nil {
		result.DisplayName = *input.DisplayName
	}
	if input.Name != nil {
		result.Name = *input.Name
	}
	if input.Emails != nil {
		if err := validateEmails(*input.Emails); err != nil {
			return err
		}
		if op == "add" {
			result.Emails = applyEmailAdditions(result.Emails, *input.Emails)
		} else {
			result.Emails = append([]Email(nil), (*input.Emails)...)
		}
	}
	return nil
}

func normalizePatchPath(path, base string) string {
	path = strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
	if quote := strings.IndexByte(path, '"'); quote >= 0 {
		path = strings.ToLower(path[:quote]) + path[quote:]
	} else {
		path = strings.ToLower(path)
	}
	for schema, prefix := range map[string]string{UserSchemaURN: strings.ToLower(UserSchemaURN) + ":", GroupSchemaURN: strings.ToLower(GroupSchemaURN) + ":"} {
		if strings.HasPrefix(path, prefix) {
			if schema != base {
				return path
			}
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func applyPathPatch(result *persistedUser, op, path string, raw json.RawMessage) error {
	remove := op == "remove"
	switch path {
	case "active":
		if remove {
			result.Active = false
			return nil
		}
		return decodePatchValue(raw, &result.Active)
	case "externalid":
		if remove {
			if result.ExternalID == "" {
				return noTarget("externalId was not found")
			}
			result.ExternalID = ""
			return nil
		}
		return decodePatchValue(raw, &result.ExternalID)
	case "username":
		if remove {
			return fmt.Errorf("userName cannot be removed")
		}
		return decodePatchValue(raw, &result.UserName)
	case "displayname":
		if remove {
			if result.DisplayName == "" {
				return noTarget("displayName was not found")
			}
			result.DisplayName = ""
			return nil
		}
		return decodePatchValue(raw, &result.DisplayName)
	case "name":
		if remove {
			if result.Name == (Name{}) {
				return noTarget("name was not found")
			}
			result.Name = Name{}
			return nil
		}
		return decodePatchValue(raw, &result.Name)
	case "name.formatted":
		if remove {
			if result.Name.Formatted == "" {
				return noTarget("name.formatted was not found")
			}
			result.Name.Formatted = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.Formatted)
	case "name.givenname":
		if remove {
			if result.Name.GivenName == "" {
				return noTarget("name.givenName was not found")
			}
			result.Name.GivenName = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.GivenName)
	case "name.familyname":
		if remove {
			if result.Name.FamilyName == "" {
				return noTarget("name.familyName was not found")
			}
			result.Name.FamilyName = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.FamilyName)
	case "emails":
		if remove {
			if len(result.Emails) == 0 {
				return noTarget("emails was not found")
			}
			result.Emails = nil
			return nil
		}
		var many []Email
		if err := json.Unmarshal(raw, &many); err == nil {
			if err := validateEmails(many); err != nil {
				return err
			}
			if op == "add" {
				result.Emails = applyEmailAdditions(result.Emails, many)
			} else {
				result.Emails = many
			}
			return nil
		}
		var one Email
		if err := json.Unmarshal(raw, &one); err != nil {
			return fmt.Errorf("emails must be an object or array")
		}
		if err := validateEmails([]Email{one}); err != nil {
			return err
		}
		if op == "add" {
			result.Emails = applyEmailAdditions(result.Emails, []Email{one})
		} else {
			result.Emails = []Email{one}
		}
		return nil
	case `emails[type eq "work"].value`:
		if remove {
			matched := false
			filtered := result.Emails[:0]
			for _, email := range result.Emails {
				if strings.EqualFold(strings.TrimSpace(email.Type), "work") {
					matched = true
					continue
				}
				filtered = append(filtered, email)
			}
			if !matched {
				return noTarget("work email was not found")
			}
			result.Emails = filtered
			return nil
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("work email value must be a string")
		}
		matched := false
		for i := range result.Emails {
			if strings.EqualFold(strings.TrimSpace(result.Emails[i].Type), "work") {
				result.Emails[i].Value = value
				matched = true
			}
		}
		if !matched {
			return noTarget("work email was not found")
		}
		return nil
	default:
		return invalidPath(fmt.Sprintf("unsupported path %q", path))
	}
}

func clearPrimary(emails []Email) {
	for i := range emails {
		emails[i].Primary = false
	}
}

func applyEmailAdditions(existing, additions []Email) []Email {
	result := append([]Email(nil), existing...)
	for _, addition := range additions {
		matching := -1
		for i, current := range result {
			if strings.EqualFold(current.Value, addition.Value) && strings.EqualFold(current.Type, addition.Type) {
				matching = i
				break
			}
		}
		if matching >= 0 {
			if result[matching].Primary != addition.Primary {
				if addition.Primary {
					clearPrimary(result)
				}
				result[matching].Primary = addition.Primary
			}
			continue
		}
		if addition.Primary {
			clearPrimary(result)
		}
		result = append(result, addition)
	}
	return result
}

func decodePatchValue(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("value is required")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("value has the wrong type")
	}
	return nil
}
