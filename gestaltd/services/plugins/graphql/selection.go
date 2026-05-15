package graphql

import (
	"fmt"
	"strings"
	"unicode"
)

type selection interface {
	selectionNode()
}

type fieldSelection struct {
	Alias      string
	Name       string
	Selections []selection
}

func (fieldSelection) selectionNode() {}

type inlineFragmentSelection struct {
	TypeCondition string
	Selections    []selection
}

func (inlineFragmentSelection) selectionNode() {}

func validateExplicitSelectionSet(schema *Schema, operation string, ref TypeRef, raw string) error {
	trimmed := strings.TrimSpace(raw)
	inner := ref.innerType()
	typeName := inner.namedType()
	ft := schema.lookupType(typeName)
	if !isCompositeType(ft) {
		if trimmed != "" {
			kind := "unknown"
			if ft != nil {
				kind = ft.Kind
			}
			return fmt.Errorf("operation %q selectionSet is not valid for %s return type %q", operation, kind, typeName)
		}
		return nil
	}
	if trimmed == "" {
		return fmt.Errorf("operation %q requires graphql.selectionSet for %s return type %q", operation, ft.Kind, typeName)
	}

	p := selectionParser{input: trimmed}
	selections, err := p.parseSelectionList(false)
	if err != nil {
		return fmt.Errorf("operation %q graphql.selectionSet: %w", operation, err)
	}
	if err := p.expectEnd(); err != nil {
		return fmt.Errorf("operation %q graphql.selectionSet: %w", operation, err)
	}
	if len(selections) == 0 {
		return fmt.Errorf("operation %q requires at least one selection", operation)
	}
	if err := validateSelections(schema, operation, ft, selections); err != nil {
		return err
	}
	return nil
}

func explicitSelectionRequired(schema *Schema, ref TypeRef) bool {
	return isCompositeType(schema.lookupType(ref.innerType().namedType()))
}

func isCompositeType(ft *FullType) bool {
	if ft == nil {
		return false
	}
	return ft.Kind == KindObject || ft.Kind == KindInterface || ft.Kind == KindUnion
}

func validateSelections(schema *Schema, operation string, parent *FullType, selections []selection) error {
	for _, sel := range selections {
		switch s := sel.(type) {
		case fieldSelection:
			if err := validateFieldSelection(schema, operation, parent, s); err != nil {
				return err
			}
		case inlineFragmentSelection:
			target := schema.lookupType(s.TypeCondition)
			if !isCompositeType(target) {
				return fmt.Errorf("operation %q inline fragment references unknown composite type %q", operation, s.TypeCondition)
			}
			if !fragmentTypeAllowed(parent, target) {
				return fmt.Errorf("operation %q inline fragment type %q is not valid for parent type %q", operation, target.Name, parent.Name)
			}
			if len(s.Selections) == 0 {
				return fmt.Errorf("operation %q inline fragment on %q requires at least one selection", operation, target.Name)
			}
			if err := validateSelections(schema, operation, target, s.Selections); err != nil {
				return err
			}
		default:
			return fmt.Errorf("operation %q contains unsupported selection", operation)
		}
	}
	return nil
}

func validateFieldSelection(schema *Schema, operation string, parent *FullType, sel fieldSelection) error {
	if sel.Name == "__typename" {
		if len(sel.Selections) > 0 {
			return fmt.Errorf("operation %q field %q on %q cannot have a nested selection", operation, sel.Name, parent.Name)
		}
		return nil
	}
	if parent.Kind == KindUnion {
		return fmt.Errorf("operation %q field %q cannot be selected directly on union %q", operation, sel.Name, parent.Name)
	}

	field := lookupField(parent, sel.Name)
	if field == nil {
		return fmt.Errorf("operation %q field %q does not exist on type %q", operation, sel.Name, parent.Name)
	}
	if required := requiredArgumentNames(field.Args); len(required) > 0 {
		return fmt.Errorf("operation %q field %q on %q requires argument %q, but selectionSet arguments are not supported", operation, sel.Name, parent.Name, required[0])
	}
	fieldType := schema.lookupType(field.Type.innerType().namedType())
	fieldComposite := isCompositeType(fieldType)
	if fieldComposite {
		if len(sel.Selections) == 0 {
			return fmt.Errorf("operation %q field %q on %q requires a nested selection", operation, sel.Name, parent.Name)
		}
		return validateSelections(schema, operation, fieldType, sel.Selections)
	}
	if len(sel.Selections) > 0 {
		return fmt.Errorf("operation %q field %q on %q cannot have a nested selection", operation, sel.Name, parent.Name)
	}
	return nil
}

func requiredArgumentNames(args []InputValue) []string {
	var required []string
	for _, arg := range args {
		if arg.Type.isNonNull() && arg.DefaultValue == nil {
			required = append(required, arg.Name)
		}
	}
	return required
}

func lookupField(ft *FullType, name string) *Field {
	if ft == nil {
		return nil
	}
	for i := range ft.Fields {
		if ft.Fields[i].Name == name {
			return &ft.Fields[i]
		}
	}
	return nil
}

func fragmentTypeAllowed(parent, target *FullType) bool {
	if parent == nil || target == nil {
		return false
	}
	if parent.Name == target.Name {
		return true
	}

	parentPossible, parentKnown := possibleObjectNames(parent)
	if !parentKnown {
		return false
	}
	targetPossible, targetKnown := possibleObjectNames(target)
	if !targetKnown {
		return false
	}
	for name := range parentPossible {
		if _, ok := targetPossible[name]; ok {
			return true
		}
	}
	return false
}

func possibleObjectNames(ft *FullType) (map[string]struct{}, bool) {
	if ft == nil {
		return nil, false
	}
	if ft.Kind == KindObject {
		return map[string]struct{}{ft.Name: {}}, true
	}
	if ft.Kind != KindInterface && ft.Kind != KindUnion {
		return nil, false
	}
	if len(ft.PossibleTypes) == 0 {
		return nil, false
	}
	names := make(map[string]struct{}, len(ft.PossibleTypes))
	for _, possible := range ft.PossibleTypes {
		names[possible.Name] = struct{}{}
	}
	return names, true
}

type selectionParser struct {
	input string
	pos   int
}

func (p *selectionParser) parseSelectionList(stopOnBrace bool) ([]selection, error) {
	var selections []selection
	for {
		p.skipIgnored()
		if p.eof() {
			if stopOnBrace {
				return nil, fmt.Errorf("missing closing brace")
			}
			return selections, nil
		}
		if p.peek() == '}' {
			if !stopOnBrace {
				return nil, fmt.Errorf("unexpected closing brace")
			}
			p.pos++
			return selections, nil
		}
		sel, err := p.parseSelection()
		if err != nil {
			return nil, err
		}
		selections = append(selections, sel)
	}
}

func (p *selectionParser) parseSelection() (selection, error) {
	if p.consume("...") {
		p.skipIgnored()
		if !p.consumeName("on") {
			return nil, fmt.Errorf("fragment spreads are not supported")
		}
		p.skipIgnored()
		typeName, err := p.readName()
		if err != nil {
			return nil, fmt.Errorf("inline fragment missing type condition")
		}
		p.skipIgnored()
		selections, err := p.parseBracedSelectionList()
		if err != nil {
			return nil, err
		}
		return inlineFragmentSelection{TypeCondition: typeName, Selections: selections}, nil
	}

	first, err := p.readName()
	if err != nil {
		return nil, err
	}
	p.skipIgnored()
	name := first
	alias := ""
	if p.consume(":") {
		alias = first
		p.skipIgnored()
		name, err = p.readName()
		if err != nil {
			return nil, fmt.Errorf("alias %q missing field name", alias)
		}
		p.skipIgnored()
	}
	if p.peekOrZero() == '(' {
		return nil, fmt.Errorf("field arguments are not supported in selectionSet")
	}
	if p.peekOrZero() == '@' {
		return nil, fmt.Errorf("directives are not supported in selectionSet")
	}

	var selections []selection
	if p.peekOrZero() == '{' {
		var err error
		selections, err = p.parseBracedSelectionList()
		if err != nil {
			return nil, err
		}
	}
	return fieldSelection{Alias: alias, Name: name, Selections: selections}, nil
}

func (p *selectionParser) parseBracedSelectionList() ([]selection, error) {
	if !p.consume("{") {
		return nil, fmt.Errorf("expected opening brace")
	}
	return p.parseSelectionList(true)
}

func (p *selectionParser) expectEnd() error {
	p.skipIgnored()
	if !p.eof() {
		return fmt.Errorf("unexpected input %q", p.input[p.pos:])
	}
	return nil
}

func (p *selectionParser) readName() (string, error) {
	if p.eof() {
		return "", fmt.Errorf("expected name")
	}
	r := rune(p.input[p.pos])
	if !isNameStart(r) {
		return "", fmt.Errorf("expected name at %q", p.input[p.pos:])
	}
	start := p.pos
	p.pos++
	for !p.eof() {
		r = rune(p.input[p.pos])
		if !isNameContinue(r) {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos], nil
}

func (p *selectionParser) consumeName(name string) bool {
	start := p.pos
	got, err := p.readName()
	if err != nil || got != name {
		p.pos = start
		return false
	}
	return true
}

func (p *selectionParser) skipIgnored() {
	for !p.eof() {
		r := rune(p.input[p.pos])
		if unicode.IsSpace(r) || r == ',' {
			p.pos++
			continue
		}
		if r == '#' {
			for !p.eof() && p.input[p.pos] != '\n' && p.input[p.pos] != '\r' {
				p.pos++
			}
			continue
		}
		return
	}
}

func (p *selectionParser) consume(s string) bool {
	if strings.HasPrefix(p.input[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *selectionParser) peek() byte {
	return p.input[p.pos]
}

func (p *selectionParser) peekOrZero() byte {
	if p.eof() {
		return 0
	}
	return p.input[p.pos]
}

func (p *selectionParser) eof() bool {
	return p.pos >= len(p.input)
}

func isNameStart(r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func isNameContinue(r rune) bool {
	return isNameStart(r) || r >= '0' && r <= '9'
}
