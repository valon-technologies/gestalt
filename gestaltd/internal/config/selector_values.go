package config

import "regexp"

var (
	safeConnectionValue = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeInstanceValue   = regexp.MustCompile(`^[a-zA-Z0-9._ -]+$`)
)

func SafeConnectionValue(value string) bool {
	return safeConnectionValue.MatchString(value)
}

func SafeInstanceValue(value string) bool {
	return safeInstanceValue.MatchString(value)
}
