package gestalt

import "maps"

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
