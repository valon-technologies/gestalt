package model

// OneofFields returns matching message fields in the oneof's field-number order.
func OneofFields(m *Message, o *Oneof) []*Field {
	var out []*Field
	for _, number := range o.FieldNumbers {
		for _, f := range m.Fields {
			if f.Number == number {
				out = append(out, f)
			}
		}
	}
	return out
}

// FindField returns the first message field with the given proto name.
func FindField(m *Message, protoName string) *Field {
	for _, f := range m.Fields {
		if f.Name == protoName {
			return f
		}
	}
	return nil
}

// ContextField returns the first context field in the service's method inputs.
func ContextField(svc *Service) *Field {
	for _, m := range svc.Methods {
		if m.Input == nil {
			continue
		}
		if f := FindField(m.Input, "context"); f != nil {
			return f
		}
	}
	return nil
}
