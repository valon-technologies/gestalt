package toolchain

// BufVersion is the single source of truth for the buf pin. CI asserts that
// the go install pins in workflow files match this constant.
const BufVersion = "1.66.1"

const bufInstallHint = "go install github.com/bufbuild/buf/cmd/buf@v" + BufVersion

// Buf returns the pinned buf tool.
func Buf() *Tool {
	return &Tool{
		Name:        "buf",
		Version:     BufVersion,
		VersionArgs: []string{"--version"},
		InstallHint: bufInstallHint,
	}
}

// Rustfmt returns the rustfmt tool. Like sdk/rust/scripts/generate_stubs.sh,
// only presence is required: rustfmt ships with the Rust toolchain and is not
// version-pinned by this repo.
func Rustfmt() *Tool {
	return &Tool{
		Name:        "rustfmt",
		FormatArgs:  []string{"--edition", "2024"},
		InstallHint: "rustup component add rustfmt",
	}
}

// Gofmt returns the gofmt tool, which ships with the Go toolchain.
func Gofmt() *Tool {
	return &Tool{
		Name:        "gofmt",
		FormatArgs:  []string{"-w"},
		InstallHint: "install the Go toolchain",
	}
}
