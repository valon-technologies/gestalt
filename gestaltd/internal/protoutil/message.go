package protoutil

// ConvertMessage preserves absence before converting a message.
func ConvertMessage[Input, Output any](value *Input, convert func(*Input) *Output) *Output {
	if value == nil {
		return nil
	}
	return convert(value)
}
