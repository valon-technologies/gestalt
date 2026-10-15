package emit

// EnsureImportGroup initializes a module's import map when it is nil.
func EnsureImportGroup(groups map[string]map[string]bool, module string) {
	if groups[module] == nil {
		groups[module] = map[string]bool{}
	}
}
