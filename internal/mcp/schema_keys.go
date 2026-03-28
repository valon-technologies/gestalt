package mcp

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/core"
)

var openAIToolKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

type argMapper struct {
	originalName string
	props        map[string]*argMapper
	items        *argMapper
}

func sanitizeFlatParams(params []core.Parameter) ([]core.Parameter, *argMapper) {
	if len(params) == 0 {
		return params, nil
	}

	sanitized := make([]core.Parameter, len(params))
	used := make(map[string]struct{}, len(params))
	props := make(map[string]*argMapper, len(params))
	changed := false

	for i, param := range params {
		safeName := uniqueSanitizedKey(param.Name, used)
		sanitized[i] = param
		sanitized[i].Name = safeName

		if safeName != param.Name {
			changed = true
		}
		props[safeName] = &argMapper{originalName: param.Name}
	}

	if !changed {
		return params, nil
	}

	return sanitized, &argMapper{props: props}
}

func sanitizeInputSchema(raw json.RawMessage) (json.RawMessage, *argMapper) {
	if len(raw) == 0 {
		return raw, nil
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, nil
	}

	sanitized, mapper := sanitizeSchemaValue(doc)
	if mapper == nil || mapper.empty() {
		return raw, nil
	}

	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return raw, nil
	}

	return encoded, mapper
}

func sanitizeSchemaValue(v any) (any, *argMapper) {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		mapper := &argMapper{}
		var rawToSafe map[string]string

		if props, ok := val["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(props))
			for key := range props {
				keys = append(keys, key)
			}
			sort.Strings(keys)

			newProps := make(map[string]any, len(props))
			propMap := make(map[string]*argMapper, len(props))
			used := make(map[string]struct{}, len(props))
			rawToSafe = make(map[string]string, len(props))

			for _, rawName := range keys {
				safeName := uniqueSanitizedKey(rawName, used)
				rawToSafe[rawName] = safeName

				propValue, childMapper := sanitizeSchemaValue(props[rawName])
				newProps[safeName] = propValue

				if childMapper == nil {
					childMapper = &argMapper{}
				}
				childMapper.originalName = rawName
				propMap[safeName] = childMapper
			}

			out["properties"] = newProps
			if len(propMap) > 0 {
				mapper.props = propMap
			}
		}

		for key, child := range val {
			if key == "properties" {
				continue
			}
			if key == "required" && rawToSafe != nil {
				out[key] = rewriteRequired(child, rawToSafe)
				continue
			}
			sanitizedChild, childMapper := sanitizeSchemaValue(child)
			out[key] = sanitizedChild
			if key == "items" && childMapper != nil && !childMapper.empty() {
				mapper.items = childMapper
			}
		}

		if mapper.empty() {
			return out, nil
		}
		return out, mapper
	case []any:
		out := make([]any, len(val))
		for i := range val {
			sanitizedChild, _ := sanitizeSchemaValue(val[i])
			out[i] = sanitizedChild
		}
		return out, nil
	default:
		return v, nil
	}
}

func rewriteRequired(v any, rawToSafe map[string]string) any {
	req, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, len(req))
	for i, item := range req {
		if rawName, ok := item.(string); ok {
			if safeName, exists := rawToSafe[rawName]; exists {
				out[i] = safeName
				continue
			}
		}
		out[i] = item
	}
	return out
}

func remapArguments(args map[string]any, mapper *argMapper) map[string]any {
	if mapper == nil || mapper.empty() || len(args) == 0 {
		return args
	}
	remapped, ok := remapArgumentValue(args, mapper).(map[string]any)
	if !ok {
		return args
	}
	return remapped
}

func remapArgumentValue(v any, mapper *argMapper) any {
	if mapper == nil || mapper.empty() {
		return v
	}

	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for key, child := range val {
			nextMapper := mapper.props[key]
			outKey := key
			if nextMapper != nil && nextMapper.originalName != "" {
				outKey = nextMapper.originalName
			}
			out[outKey] = remapArgumentValue(child, nextMapper)
		}
		return out
	case []any:
		if mapper.items == nil || mapper.items.empty() {
			return v
		}
		out := make([]any, len(val))
		for i := range val {
			out[i] = remapArgumentValue(val[i], mapper.items)
		}
		return out
	default:
		return v
	}
}

func (m *argMapper) empty() bool {
	return m == nil || (m.originalName == "" && len(m.props) == 0 && (m.items == nil || m.items.empty()))
}

func uniqueSanitizedKey(raw string, used map[string]struct{}) string {
	candidate := sanitizeSchemaKey(raw)
	if _, exists := used[candidate]; !exists {
		used[candidate] = struct{}{}
		return candidate
	}

	suffix := "." + shortKeyHash(raw)
	unique := truncateWithSuffix(candidate, suffix)
	for {
		if _, exists := used[unique]; !exists {
			used[unique] = struct{}{}
			return unique
		}
		suffix += "x"
		if len(suffix) > 16 {
			suffix = "." + shortKeyHash(unique)
		}
		unique = truncateWithSuffix(candidate, suffix)
	}
}

func sanitizeSchemaKey(raw string) string {
	if openAIToolKeyPattern.MatchString(raw) {
		return raw
	}

	var b strings.Builder
	lastSep := false
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastSep = false
		case r == '[':
			if b.Len() > 0 && !lastSep {
				b.WriteByte('.')
				lastSep = true
			}
		case r == ']':
			continue
		default:
			if b.Len() > 0 && !lastSep {
				b.WriteByte('.')
				lastSep = true
			}
		}
	}

	safe := strings.Trim(b.String(), "._-")
	if safe == "" {
		safe = "arg"
	}

	if len(safe) > 64 {
		safe = truncateWithSuffix(safe, "."+shortKeyHash(raw))
	}

	return safe
}

const maxKeyLen = 64

func truncateWithSuffix(base, suffix string) string {
	maxBase := maxKeyLen - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return base + suffix
}

func shortKeyHash(raw string) string {
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])[:8]
}
