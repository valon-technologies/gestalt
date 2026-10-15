package protoutil

// ConvertMessage preserves absence before converting a message.
func ConvertMessage[Input, Output any](value *Input, convert func(*Input) Output) Output {
	if value == nil {
		var zero Output
		return zero
	}
	return convert(value)
}

// ConvertMessageErr preserves absence before converting a fallible message.
func ConvertMessageErr[Input, Output any](value *Input, convert func(*Input) (*Output, error)) (*Output, error) {
	if value == nil {
		return nil, nil
	}
	return convert(value)
}
