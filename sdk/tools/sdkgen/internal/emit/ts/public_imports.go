package ts

type PublicImports struct {
	SupportPrefix     string
	FixedNativeModule string
}

func ServerPublicImports() PublicImports {
	return PublicImports{SupportPrefix: "../.."}
}

func WebPublicImports() PublicImports {
	return PublicImports{
		SupportPrefix:     "../runtime",
		FixedNativeModule: "native-types.ts",
	}
}

func WebRuntimeImports() PublicImports {
	return PublicImports{
		SupportPrefix:     "../..",
		FixedNativeModule: "native-types.ts",
	}
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
