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

// PrettierVersion pins the formatter for generated TypeScript. Prettier runs
// through `bun x prettier@<pin>`, which resolves the exact version without a
// global install; bun is already required by the TypeScript SDK.
const PrettierVersion = "3.6.2"

// Prettier returns the pinned prettier tool, used to format generated
// TypeScript.
func Prettier() *Tool {
	return &Tool{
		Name:        "bun",
		FormatArgs:  []string{"x", "prettier@" + PrettierVersion, "--write"},
		InstallHint: "install bun (https://bun.sh)",
	}
}

// RuffVersion pins the formatter for generated Python, matching the ruff
// pin in sdk/python/pyproject.toml so the formatter and the lint gate agree.
const RuffVersion = "0.15.14"

// Ruff returns the pinned ruff tool, used to format generated Python.
func Ruff() *Tool {
	return &Tool{
		Name:        "ruff",
		Version:     "ruff " + RuffVersion,
		VersionArgs: []string{"--version"},
		FormatArgs:  []string{"format"},
		InstallHint: "uv tool install ruff@" + RuffVersion,
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
