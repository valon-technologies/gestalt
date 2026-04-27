package toolschema

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// MaxPropertyNameLength is the maximum model-facing tool input property length
// accepted by the strictest model provider we currently target.
const MaxPropertyNameLength = 64

var propertyNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

// NameMapping links a model-facing tool input name to its original wire name.
type NameMapping struct {
	Name     string
	WireName string
}

// NameAllocator assigns unique model-facing names within one tool schema.
type NameAllocator struct {
	used map[string]string
}

// NewNameAllocator returns an allocator for one operation or tool schema.
func NewNameAllocator() *NameAllocator {
	return &NameAllocator{used: make(map[string]string)}
}

// Allocate returns a safe, unique model-facing name for raw.
func (a *NameAllocator) Allocate(raw string) NameMapping {
	if a.used == nil {
		a.used = make(map[string]string)
	}
	baseName, wireName := NormalizePropertyName(raw)
	name := a.unique(baseName, raw)
	if name != baseName && wireName == "" {
		wireName = raw
	}
	return NameMapping{Name: name, WireName: wireName}
}

// NormalizePropertyName converts a raw provider field name into a model-safe
// tool input property name. It returns a wireName only when callers must retain
// the original name for execution.
func NormalizePropertyName(raw string) (name, wireName string) {
	normalized := safePropertyName(raw)
	if normalized == raw {
		return raw, ""
	}
	return normalized, raw
}

// ValidPropertyName reports whether name is safe to expose as a model-facing
// tool input property.
func ValidPropertyName(name string) bool {
	return propertyNamePattern.MatchString(name)
}

func safePropertyName(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case isPropertyNameChar(r):
			b.WriteRune(r)
		case r == '[':
			b.WriteByte('_')
		case r == ']':
			continue
		case r == '$':
			writeNamedSeparator(&b, "dollar")
		case r == '@':
			writeNamedSeparator(&b, "at")
		case r == '\'', r == '"', r == '`':
			continue
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "param"
	}
	if len(name) <= MaxPropertyNameLength {
		return name
	}
	hash := hashPropertyName(raw)
	suffix := fmt.Sprintf("_%08x", hash)
	return name[:MaxPropertyNameLength-len(suffix)] + suffix
}

func isPropertyNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' ||
		r == '.' ||
		r == '-'
}

func writeNamedSeparator(b *strings.Builder, name string) {
	if b.Len() > 0 {
		b.WriteByte('_')
	}
	b.WriteString(name)
	b.WriteByte('_')
}

func hashPropertyName(raw string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(raw))
	return h.Sum32()
}

func (a *NameAllocator) unique(baseName, rawName string) string {
	if _, exists := a.used[baseName]; !exists {
		a.used[baseName] = rawName
		return baseName
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("_%d", i)
		prefixLen := MaxPropertyNameLength - len(suffix)
		name := baseName
		if len(name) > prefixLen {
			name = name[:prefixLen]
		}
		name += suffix
		if _, exists := a.used[name]; !exists {
			a.used[name] = rawName
			return name
		}
	}
}
