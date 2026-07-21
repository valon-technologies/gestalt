# Install-Time Validation

Checks on `POST …/add` and `POST …/upgrade` after the registry entry is fetched and before `Rollouts.Create` and `AppendRequest`. Failure returns **400** and writes nothing to IndexedDB.

Related docs:

- [lifecycle.md](./lifecycle.md) — handler pipeline
- [models.md](./models.md) — `requires` and `compatibility` on published version JSON
- [tests.md](./tests.md#install-time-validation-tests) — tests

Implementation (planned): `gestaltd/internal/appregistry/install_validator.go`; called from `installer.go` before `Rollouts.Create`.

## Checks

| Check | Source | Failure |
|-------|--------|---------|
| Platform artifact | `entry.Artifacts` for gestaltd host OS/arch (e.g. `linux/amd64`) | **400** — no artifact for platform |
| Gestalt version | `entry.compatibility.minGestaltdVersion` vs running `gestaltd` (when set) | **400** — gestaltd too old |
| Dependency present | Each `entry.requires.apps` entry has a fleet-known version, or a deploy-pinned non-registry provider | **400** |
| Dependency version | Fleet-known version satisfies `requires.apps.{app}.version` | **400** |
| Dependency operations | Dependency published `interface` exposes each required operation; `inputSchemaHash` matches when set | **400** |
| Reverse dependents | No other fleet-known app has a `requires.apps.{app}` entry broken by the candidate `interface` | **400** |

Registry-only dependencies are validated against the fleet-known version's published metadata (`versions/{version}.json`).

Other admission rules (registry binding, `add`/`upgrade` mode, duplicate version, active rollout, install lock) are enforced in `Installer.install` — see [lifecycle.md](./lifecycle.md).
