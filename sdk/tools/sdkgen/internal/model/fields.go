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
