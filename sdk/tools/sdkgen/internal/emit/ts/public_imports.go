package ts

// PublicImports configures support-module import paths for generated public
// client files under a package-owned output directory.
type PublicImports struct {
	// SupportPrefix is the path from generated/ to support modules, without a
	// trailing slash. Server clients use "../.."; web clients use "../runtime".
	SupportPrefix string
	// FixedNativeModule, when set, is used for every native type import instead
	// of per-proto generatedFileBase names. Web clients use native-types.ts.
	FixedNativeModule string
}

// ServerPublicImports returns import paths for @valon-technologies/gestalt.
func ServerPublicImports() PublicImports {
	return PublicImports{SupportPrefix: "../.."}
}

// WebPublicImports returns import paths for @valon-technologies/gestalt-web.
func WebPublicImports() PublicImports {
	return PublicImports{SupportPrefix: "../runtime"}
}

func (i PublicImports) nativeModulePath(protoFile string) string {
	if i.FixedNativeModule != "" {
		return i.SupportPrefix + "/" + i.FixedNativeModule
	}
	return i.SupportPrefix + "/" + generatedFileBase(protoFile) + ".ts"
}

func (i PublicImports) nativeTypeImportQuoted(base string) string {
	if i.FixedNativeModule != "" {
		return `"` + i.SupportPrefix + "/" + i.FixedNativeModule + `"`
	}
	return `"` + i.SupportPrefix + "/" + base + ".ts" + `"`
}

func (i PublicImports) codecModulePath(protoFile string) string {
	return i.SupportPrefix + "/internal/codec/" + generatedFileBase(protoFile) + ".ts"
}

func (i PublicImports) genModulePath(protoFile string) string {
	return i.SupportPrefix + "/internal/gen/" + protoGenImportBase(protoFile) + "_pb.ts"
}

func (i PublicImports) genModuleQuoted(protoFile string) string {
	return `"` + i.genModulePath(protoFile) + `"`
}

func (i PublicImports) supportModuleQuoted(suffix string) string {
	return `"` + i.SupportPrefix + "/" + suffix + `"`
}
