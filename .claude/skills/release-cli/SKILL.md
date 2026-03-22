---
name: release-cli
description: "Release the gestalt CLI. Handles version bumping, tagging, and triggering the release pipeline. Triggers: release CLI, release gestalt, cut a release, tag a release, new CLI version."
allowed-tools: Bash Read Edit Glob Grep
---

# Release Gestalt CLI

Guide for releasing a new version of the gestalt CLI.

## Prerequisites

- All changes must be merged to `main`
- The `valon-technologies/homebrew-gestalt` repo must exist (for Homebrew tap sync)
- The `PAT_TOKEN` secret must be configured in the repo (for cross-repo formula sync)

## How the Release Pipeline Works

Pushing a `v*` tag to `main` triggers this automated chain:

1. **Build** (`rust-build-matrix.yml`): Compiles for macOS ARM64/x86_64, Linux x86_64/ARM64, Windows x86_64
2. **Release** (`release-gestalt-cli.yml`): Creates a GitHub Release with tar.gz/zip archives + SHA256 checksums
3. **Update Formula** (`release-gestalt-cli.yml`): Checks out `main`, downloads SHA256 files from the release, updates `Formula/gestalt.rb` with real checksums and version, commits and pushes
4. **Sync Homebrew** (`sync-homebrew-formula.yml`): Triggered by the formula commit on `main`, syncs `Formula/` to `valon-technologies/homebrew-gestalt`

Tags containing `-alpha`, `-beta`, or `-rc` are marked as pre-releases on GitHub.

## Release Steps

### 1. Decide on a version

The version lives in `client/Cargo.toml` (workspace version). Current versioning uses semver with optional pre-release suffixes.

If bumping the version, update these files:
- `client/Cargo.toml` — `[workspace.package] version`
- `deploy/helm/gestalt/Chart.yaml` — `version` and `appVersion` (if keeping them in sync)

### 2. Tag and push

```bash
# Make sure you're on main and up to date
git checkout main
git pull

# Tag (use the version from Cargo.toml, prefixed with v)
git tag v<VERSION>
git push origin v<VERSION>
```

Example: `git tag v0.1.0 && git push origin v0.1.0`

### 3. Verify

- Check the Actions tab for the "Release Gestalt CLI" workflow
- All 3 jobs should succeed: Build → Create Release → Update Homebrew Formula
- The GitHub Release page should have 10 assets (5 platforms x 2 files each: archive + checksum)
- A follow-up commit on `main` should update `Formula/gestalt.rb` with real SHA256 values
- The "Sync Homebrew Formula" workflow should run after that commit

### 4. Test installation

```bash
brew update
brew install valon-technologies/gestalt/gestalt
gestalt --version
```

## Troubleshooting

- **Release workflow fails at "Update Homebrew Formula"**: Check that `PAT_TOKEN` secret is set and has repo access
- **Sync workflow fails**: Check that `valon-technologies/homebrew-gestalt` repo exists and `PAT_TOKEN` can push to it
- **`brew install` fails with SHA mismatch**: The formula update job may not have run yet — wait for the workflow to complete
- **`brew install` fails with auth error**: User needs `gh auth login` or `HOMEBREW_GITHUB_API_TOKEN` set (private repo)
