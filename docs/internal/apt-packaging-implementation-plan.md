# APT Packaging Implementation Plan

Status: draft implementation plan
Last updated: 2026-04-20
Depends on:

- `README.md`
- `.github/workflows/release-gestalt-cli.yml`
- `.github/workflows/release-gestaltd.yml`
- `.github/workflows/rust-build-matrix.yml`
- `Formula/gestalt.rb`
- `Formula/gestaltd.rb`

## Goal

Make both `gestalt` and `gestaltd` installable on Debian and Ubuntu via `apt` / `apt-get` through an official, first-party package repository.

The target user experience is:

```sh
sudo apt update && sudo apt install curl gpg
curl -fsSL https://apt.gestaltd.ai/gpg | sudo gpg --dearmor -o /usr/share/keyrings/gestalt-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gestalt-archive-keyring.gpg] https://apt.gestaltd.ai $(. /etc/os-release && echo ${VERSION_CODENAME}) main" \
  | sudo tee /etc/apt/sources.list.d/gestalt.list
sudo apt update
sudo apt install gestalt gestaltd
```

`apt` and `apt-get` use the same repository, so no separate packaging work is needed for `apt-get`.

## Current State

Gestalt currently ships:

- Homebrew formulas for both binaries
- GitHub release tarballs for `gestalt`
- GitHub release tarballs for `gestaltd`
- Docker images for both binaries

It does not currently ship:

- `.deb` artifacts
- a signed apt repository
- distro support policy for Debian / Ubuntu
- installation docs for apt

Relevant in-repo anchors:

- `README.md` documents Homebrew-only quick start today.
- `.github/workflows/release-gestalt-cli.yml` publishes CLI release assets and updates the formula.
- `.github/workflows/release-gestaltd.yml` publishes server release assets, Docker images, Helm chart versioning, and the formula update.
- `.github/workflows/rust-build-matrix.yml` already builds Linux release binaries for `amd64`, `arm64`, and `armv7`.

## Packaging Posture

Follow a Terraform-style posture, not a Helm-style posture.

That means:

- apt is a first-party supported installation channel
- the repo is published on a Gestalt-controlled domain
- repository metadata is signed
- supported distro versions are explicit
- prereleases are separated from stable installs

Borrow one thing from `uv`:

- keep direct GitHub release tarballs as the low-friction fallback for CI, containers, and unsupported Linux variants

Do not follow Helm's "community-hosted convenience package" model. That posture makes support ownership unclear and creates avoidable migration risk if hosting changes.

## Core Decisions

### 1. Treat apt as an official Linux channel

If Gestalt is going to document apt alongside Homebrew, users will reasonably interpret it as supported. The project should therefore own:

- signing key lifecycle
- distro support policy
- package smoke tests
- channel semantics
- outage / migration communication

### 2. Split stable and prerelease channels from day one

Current Gestalt releases are `0.0.1-alpha.*`.

Implication:

- initial apt publication should go to a prerelease channel only
- do not update the main install docs or README quick start to recommend apt until `main` contains non-prerelease builds

Recommended channel model:

- `main`: stable releases only
- `test`: prerelease (`-alpha`, `-beta`, `-rc`) packages

This mirrors HashiCorp's stable + test split and avoids training users to install alpha builds from the default repo path.

### 3. Start with a narrow support matrix

Initial official support:

- Ubuntu: `jammy`, `noble`
- Debian: `bookworm`, `trixie`
- Architectures: `amd64`, `arm64`

Keep `armv7` release tarballs, but exclude `armv7` from the first apt rollout.

Reasoning:

- `gestalt` already builds `armv7`, but apt support implies install verification and support commitments across suites
- `amd64` and `arm64` cover the overwhelming majority of Debian / Ubuntu server installs

### 4. Keep package behavior conservative

`gestalt` is straightforward: install the CLI binary into `/usr/bin`.

`gestaltd` needs more care because it is a daemon. The package should:

- install the server binary
- install a `systemd` unit
- install an example config
- create a dedicated system user / group
- install but not auto-start the service until the operator provides `/etc/gestaltd/config.yaml`

Do not generate a live config in `postinst`. The current server's auto-generated default config is user-home oriented (`~/.gestaltd/config.yaml`) and not appropriate to silently create during system package installation.

### 5. Prefer simple, explicit packaging tools

Recommended stack:

- `.deb` creation: `nfpm`
- apt repo metadata generation: `reprepro`
- repo hosting: object storage + CDN behind `apt.gestaltd.ai`

Default hosting recommendation:

- S3 bucket + CloudFront distribution + DNS entry for `apt.gestaltd.ai`

Alternative acceptable hosting:

- equivalent object storage + CDN stack

The important constraint is first-party domain ownership and static-file publication, not the specific cloud vendor.

## Proposed Repo Layout

Add a dedicated packaging tree:

```text
packaging/
  nfpm/
    gestalt.yaml
    gestaltd.yaml
  systemd/
    gestaltd.service
  deb/
    gestaltd.postinstall.sh
    gestaltd.preremove.sh
    gestaltd.tmpfiles.conf
```

Add release and verification workflows:

```text
.github/workflows/
  publish-apt-repo.yml
  test-apt-install.yml
```

Docs changes after the repo is live:

```text
docs/content/install/
  apt.mdx
  _meta.ts
docs/content/install/index.mdx
docs/content/getting-started.mdx
README.md
```

Keep `Formula/gestalt.rb` and `Formula/gestaltd.rb` unchanged in scope. Homebrew remains a supported install path.

## Package Definitions

### `gestalt`

Package intent:

- interactive CLI
- no daemon behavior
- safe for user shells and CI

Recommended metadata:

- package name: `gestalt`
- arch: `amd64`, `arm64`
- section: `utils`
- priority: `optional`
- depends: `ca-certificates`
- license: `Apache-2.0`

Installed files:

- `/usr/bin/gestalt`
- `/usr/share/doc/gestalt/README.md` or generated package docs if desired

Package test command:

```sh
gestalt --version
```

### `gestaltd`

Package intent:

- system daemon
- managed by `systemd`
- explicit operator-owned config

Recommended metadata:

- package name: `gestaltd`
- arch: `amd64`, `arm64`
- section: `admin`
- priority: `optional`
- depends: `ca-certificates`
- recommends: `systemd`
- license: `Apache-2.0`

Installed files:

- `/usr/bin/gestaltd`
- `/lib/systemd/system/gestaltd.service`
- `/usr/share/doc/gestaltd/examples/config.yaml`
- `/usr/lib/tmpfiles.d/gestaltd.conf`

Package-maintainer behavior:

- create system user and group `gestaltd`
- create `/var/lib/gestaltd`
- do not create `/etc/gestaltd/config.yaml`
- do not start the service if config is absent

Recommended unit:

```ini
[Unit]
Description=Gestalt server daemon
After=network-online.target
Wants=network-online.target
ConditionPathExists=/etc/gestaltd/config.yaml

[Service]
User=gestaltd
Group=gestaltd
WorkingDirectory=/var/lib/gestaltd
ExecStart=/usr/bin/gestaltd serve --config /etc/gestaltd/config.yaml --locked --artifacts-dir /var/lib/gestaltd/artifacts --lockfile /var/lib/gestaltd/gestalt.lock.json
Restart=on-failure
StateDirectory=gestaltd
StateDirectoryMode=0750

[Install]
WantedBy=multi-user.target
```

Package test commands:

```sh
gestaltd version
systemd-analyze verify /lib/systemd/system/gestaltd.service
```

## Release Workflow Changes

### `gestalt` CLI release flow

Current flow:

- build release binaries via `.github/workflows/rust-build-matrix.yml`
- upload tarballs and checksums
- publish GitHub Release
- publish Docker image
- update Homebrew formula

Recommended change:

- extend `.github/workflows/rust-build-matrix.yml` to optionally emit `.deb` artifacts for Linux `amd64` and `arm64`
- keep tarballs as the base artifact format
- attach `.deb` files and `.sha256` files to the GitHub Release

This keeps the CLI release pipeline centralized and avoids adding a second packaging-specific build matrix for the same binary.

### `gestaltd` release flow

Current flow:

- build Linux/macOS tarballs inline
- publish GitHub Release
- publish Docker images
- update Homebrew formula
- bump Helm chart version

Recommended change:

- add `.deb` packaging steps for Linux `amd64` and `arm64` in `.github/workflows/release-gestaltd.yml`
- attach those `.deb` files to the GitHub Release
- keep tarballs and Docker image publication unchanged

### New apt publication workflow

Add `.github/workflows/publish-apt-repo.yml`.

Trigger options:

- `workflow_call` from the two release workflows, after release assets exist
- or `workflow_dispatch` for manual backfills / republishing

Responsibilities:

1. Download `.deb` artifacts for the tagged release.
2. Import the apt signing key.
3. Populate `reprepro` distributions:
   - `jammy main`
   - `noble main`
   - `bookworm main`
   - `trixie main`
   - `jammy test`
   - `noble test`
   - `bookworm test`
   - `trixie test`
4. Route packages by tag type:
   - prerelease tags -> `test`
   - stable tags -> `main`
5. Publish the generated static repo directory.
6. Sync published output to the object store behind `apt.gestaltd.ai`.

The workflow should also support publishing both packages from a single repository state so users can install `gestalt` and `gestaltd` from the same repo endpoint.

## Repository Publishing Model

### Domain and paths

Publish the apt repo on:

- `https://apt.gestaltd.ai`

Repository format:

- suites: distro codenames (`jammy`, `noble`, `bookworm`, `trixie`)
- components:
  - `main` for stable
  - `test` for prerelease

This matches Terraform's support posture better than a single `any` suite.

### Signing

Create a dedicated packaging GPG key for apt metadata.

Required docs and operational controls:

- publish armored public key at `https://apt.gestaltd.ai/gpg`
- publish fingerprint in docs
- store the private key in release automation secrets
- document rotation procedure before first public release

### EOL policy

Adopt an explicit Linux package support policy:

- support actively maintained Ubuntu and Debian releases only
- retire suites on a regular cadence after upstream EOL
- keep archived packages available separately if needed later

Do not promise indefinite suite retention from day one.

## Verification and CI

Add `.github/workflows/test-apt-install.yml`.

Run a smoke matrix against:

- `ubuntu:22.04`
- `ubuntu:24.04`
- `debian:12`
- `debian:13`

Verification steps:

1. Install `curl`, `gpg`, and apt prerequisites.
2. Add the Gestalt keyring and repo stanza.
3. `apt update`
4. `apt install gestalt`
5. `apt install gestaltd`
6. Run:
   - `gestalt --version`
   - `gestaltd version`
7. Verify package ownership and unit placement:
   - `dpkg -L gestalt`
   - `dpkg -L gestaltd`
8. Verify the service unit parses cleanly where `systemd` tooling is available.

Also add one repository-structure check in the publish workflow:

- fail if package publication attempts to push a prerelease build into `main`

## Documentation Rollout

### Before stable apt support exists

Add internal-only implementation docs only.

Do not update:

- `README.md` quick start
- `docs/content/install/index.mdx`
- `docs/content/getting-started.mdx`

Reason:

- current releases are alpha
- the public docs should not imply that stable apt is already supported

### After the first stable apt release

Add:

- `docs/content/install/apt.mdx`
- an apt entry in `docs/content/install/_meta.ts`
- apt as an installation option in `docs/content/install/index.mdx`

Update:

- `docs/content/getting-started.mdx`
- `README.md`

The docs should include:

- stable instructions
- prerelease `test` instructions in a separate section
- key fingerprint verification
- supported suites and architectures
- uninstall instructions

## Recommended PR Breakdown

Use small, integration-testable PRs.

### PR1: internal plan

Scope:

- add this implementation plan

Purpose:

- lock design before touching release automation

### PR2: package scaffolding and release assets

Scope:

- add `packaging/nfpm/*.yaml`
- add `packaging/systemd/gestaltd.service`
- add package maintainer scripts
- teach release workflows to attach `.deb` artifacts to GitHub Releases

Out of scope:

- apt repo publication
- public docs

Success criteria:

- tagged releases produce `.deb` files for `gestalt` and `gestaltd`
- GitHub Releases contain tarballs and `.deb` files

### PR3: apt repo publication and smoke tests

Scope:

- add `publish-apt-repo.yml`
- add `test-apt-install.yml`
- wire object-store publication
- publish prereleases to `test`

Out of scope:

- stable docs

Success criteria:

- `apt.gestaltd.ai` serves signed metadata
- alpha releases are installable from `test`
- smoke tests pass for supported suites

### PR4: stable channel docs and quick start updates

Scope:

- add `/install/apt`
- update install index, getting-started, and README

Gate:

- only after `main` carries a stable, non-prerelease release

Success criteria:

- public install docs match reality
- README quick start includes apt only once stable support exists

## Open Questions

1. Should the package repo be backed by S3 + CloudFront, or does the team prefer another object-storage + CDN stack?
2. What maintainer email should be used in Debian package metadata?
3. Should `gestaltd` install a disabled service only, or also `systemctl preset` it for environments that want auto-enable behavior?
4. Do we want to support `armv7` in apt eventually, or keep it as tarball-only?

## Recommended Immediate Next Step

Implement PR2 first.

Reasoning:

- `.deb` artifacts are the cheapest vertical slice
- they validate the package metadata and file layout
- they unblock manual installation testing before standing up the apt repo
- they keep the current release channels intact while the repo infrastructure is prepared
