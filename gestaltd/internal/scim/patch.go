package scim

import (
	"encoding/json"
	"fmt"
	"strings"
)

func applyPatch(current persistedUser, request patchRequest) (persistedUser, error) {
	if len(request.Operations) == 0 {
		return persistedUser{}, invalid("PATCH Operations must not be empty")
	}
	result := current
	for i, operation := range request.Operations {
		op := strings.ToLower(strings.TrimSpace(operation.Op))
		if op != "add" && op != "replace" && op != "remove" {
			return persistedUser{}, invalid(fmt.Sprintf("Operations[%d].op is unsupported", i))
		}
		path := normalizePatchPath(operation.Path)
		if path == "" {
			if op == "remove" {
				return persistedUser{}, invalid("remove requires a path")
			}
			var input userInput
			if err := json.Unmarshal(operation.Value, &input); err != nil {
				return persistedUser{}, invalid("pathless PATCH value must be an object")
			}
			mergePatchInput(&result, input)
			continue
		}
		if err := applyPathPatch(&result, op, path, operation.Value); err != nil {
			return persistedUser{}, invalid(fmt.Sprintf("Operations[%d]: %v", i, err))
		}
	}
	if strings.TrimSpace(result.UserName) == "" {
		return persistedUser{}, invalid("userName is required")
	}
	return result, nil
}

func mergePatchInput(result *persistedUser, input userInput) {
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
		result.Emails = append([]Email(nil), (*input.Emails)...)
	}
}

func normalizePatchPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	path = strings.Join(strings.Fields(path), " ")
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
			result.DisplayName = ""
			return nil
		}
		return decodePatchValue(raw, &result.DisplayName)
	case "name":
		if remove {
			result.Name = Name{}
			return nil
		}
		return decodePatchValue(raw, &result.Name)
	case "name.formatted":
		if remove {
			result.Name.Formatted = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.Formatted)
	case "name.givenname":
		if remove {
			result.Name.GivenName = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.GivenName)
	case "name.familyname":
		if remove {
			result.Name.FamilyName = ""
			return nil
		}
		return decodePatchValue(raw, &result.Name.FamilyName)
	case "emails":
		if remove {
			result.Emails = nil
			return nil
		}
		var many []Email
		if err := json.Unmarshal(raw, &many); err == nil {
			if op == "add" {
				result.Emails = append(result.Emails, many...)
			} else {
				result.Emails = many
			}
			return nil
		}
		var one Email
		if err := json.Unmarshal(raw, &one); err != nil {
			return fmt.Errorf("emails must be an object or array")
		}
		if op == "add" {
			result.Emails = append(result.Emails, one)
		} else {
			result.Emails = []Email{one}
		}
		return nil
	case `emails[type eq "work"].value`:
		if remove {
			for i := range result.Emails {
				if strings.EqualFold(strings.TrimSpace(result.Emails[i].Type), "work") {
					result.Emails[i].Value = ""
				}
			}
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
			result.Emails = append(result.Emails, Email{Value: value, Type: "work"})
		}
		return nil
	default:
		return fmt.Errorf("unsupported path %q", path)
	}
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
