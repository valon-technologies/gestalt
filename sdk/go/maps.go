package gestalt

import "maps"

func cloneMap[K comparable, V any](values map[K]V) map[K]V {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
