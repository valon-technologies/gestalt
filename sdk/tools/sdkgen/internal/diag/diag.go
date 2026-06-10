// Package diag carries sdkgen diagnostics tied to proto source locations.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

// Diagnostic is a single generation error tied to a proto source location.
type Diagnostic struct {
	ProtoFile string
	Line      int
	Col       int
	Kind      string
	FullName  string
	Detail    string
}

func (d Diagnostic) String() string {
	var b strings.Builder
	if d.ProtoFile != "" {
		fmt.Fprintf(&b, "%s:", d.ProtoFile)
		if d.Line > 0 {
			fmt.Fprintf(&b, "%d:%d:", d.Line, d.Col)
		}
		b.WriteByte(' ')
	}
	if d.FullName != "" {
		fmt.Fprintf(&b, "%s %s: ", d.Kind, d.FullName)
	}
	b.WriteString(d.Detail)
	return b.String()
}

// List collects diagnostics across a whole run so every problem is reported at
// once instead of stopping at the first unsupported construct.
type List struct {
	diags []Diagnostic
}

func (l *List) Add(d Diagnostic) {
	l.diags = append(l.diags, d)
}

func (l *List) Empty() bool {
	return len(l.diags) == 0
}

func (l *List) All() []Diagnostic {
	return l.diags
}

func (l *List) Err() error {
	if l.Empty() {
		return nil
	}
	lines := make([]string, len(l.diags))
	for i, d := range l.diags {
		lines[i] = d.String()
	}
	return errors.New(strings.Join(lines, "\n"))
}
