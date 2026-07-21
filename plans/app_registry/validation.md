# Install-Time Validation

Validate registry app candidates on `POST …/add` and `POST …/upgrade` **before** creating a rollout or appending a change request. Failed validation returns an HTTP error and writes nothing to IndexedDB.

Related docs:

- [plan.md](./plan.md) — implementation path
- [lifecycle.md](./lifecycle.md#install-time-validation) — where validation runs in the install handler
- [models.md](./models.md) — `requires` and `compatibility` on published version JSON
- [tests.md](./tests.md#install-time-validation-tests) — planned handler and installer tests

Implementation (planned):

- Validator — `gestaltd/internal/appregistry/install_validator.go` (new)
- Install admission — `gestaltd/internal/appregistry/installer.go` (`install` before `Rollouts.Create`)

---

## Goals

Install-time validation closes the gap between **publish-time** checks (manifest, static dependency declarations) and **runtime** checks (each replica materializes and starts the package). It answers: *should the fleet accept this version declaration before replicas spend work converging?*

Operators recover from a bad rollout by **`POST …/upgrade` with an older published version** — the same route used for forward upgrades. There is no separate rollback API, fleet head, or promotion step.

---

## Already shipped

Fleet **activation** and **concurrency** are not part of install-time validation. They landed with the catalog poller and installer:

| Concern | Where |
|---------|--------|
| Version exists in registry | `fetchConfiguredRegistryEntry` → `RegistryReader.FetchEntry` on install |
| Registry name matches deploy `source.registry` | `Installer.install` (`ErrRegistrySourceMismatch`) |
| `add` / `upgrade` catalog rules | Empty vs non-empty known versions, `from_version` server-written |
| Duplicate `(app, version)` | `HasKnownVersion` → **400** |
| One active rollout per app | `Rollouts.Get` / `Create` → **409** |
| Install admission lock | `app_version_install_locks` for the duration of install work |
| Per-replica materialize + package validation | Poller + `ValidateInstalledPublishedPackage` at materialize/start |
| Per-app provider lifecycle lease | `StopApp` / `StartApp` serialization in poller |

Install-time validation adds **fleet admission validation** only: extra checks on the handling instance before `Rollouts.Create` and `AppendRequest`.

---

## Validation pipeline

`POST /admin/api/v1/app-registries/{registry}/apps/{app}/add|upgrade` runs validation in this order (after path/body parsing and install-lock acquisition):

1. **Admission** (existing) — registry binding, `add`/`upgrade` mode, no active rollout, lock held.
2. **Registry fetch** (existing) — `FetchEntry` for `(app, version)`; **404** when missing.
3. **Install-time validation** (planned) — `InstallValidator.Validate(ctx, ValidateInput)` using the fetched `Entry`, deploy config, and fleet-known catalog. **400** on failure.
4. **Fleet write** (existing) — `Rollouts.Create`, `ChangeRequests.AppendRequest`; roll back rollout to `failed` if append fails.

Validation must be **read-only** with respect to fleet state except for the install lock already held. It must not download artifacts or start providers on the handling instance.

```text
Client POST add/upgrade
  → acquire install lock
  → fetch PublishedVersion from registry
  → InstallValidator.Validate
  → create rollout + append change request
  → release install lock
  → 200 + installation projection
```

Replicas still re-validate the package when materializing. Install-time validation is an early reject for problems visible from registry metadata and the current fleet catalog.

---

## Checks

### 1. Runtime platform artifact

Ensure the published version includes an artifact for the **gestaltd process platform** (for example `linux/amd64` in Kubernetes).

- Source: `entry.Artifacts[platform]` via existing `resolveRegistryArtifact` rules (URL + `sha256` present).
- Failure: **400** — `registry version has no artifact for platform {platform}`.

This is a cheap guard before any replica download attempt.

### 2. Gestalt compatibility

Ensure the running `gestaltd` satisfies `entry.compatibility.minGestaltdVersion` when set.

- Source: `PublishedVersion.compatibility` ([models.md](./models.md#publishedversioncompatibility--compatibility)).
- Compare against the server build version (same source used for support diagnostics).
- Failure: **400** — `registry version requires gestaltd {min} or newer`.

### 3. Declared app dependencies

For each entry in `entry.requires.apps`, ensure the fleet can satisfy the requirement against **known installed versions** of dependency apps (projected from `app_version_change_requests`).

Per dependency app:

| Check | Rule |
|-------|------|
| Presence | Dependency app has at least one fleet-known version, or is satisfied by a deploy-time pinned provider that is not registry-only |
| Version range | Known version satisfies the semver constraint in `requires.apps.{app}.version` |
| Operations | For each `requires.apps.{app}.operations` entry, the dependency's published `interface` exposes the operation; optional `inputSchemaHash` matches |

Registry-only dependencies are validated against their fleet-known version's published metadata (fetch `versions/{version}.json` from the same or named registry when needed).

Failure: **400** with a specific dependency reason (unknown app, version not installed, range not satisfied, operation missing).

### 4. Reverse dependents (optional first cut)

When upgrading app `A`, optionally verify that no **other fleet-known registry app** has a `requires.apps.A` entry that would be broken by the candidate version's published `interface` (operation removal or incompatible schema hash).

This can be deferred after checks 1–3 if dependency graphs stay small. Document as **recommended** in the first implementation, not blocking.

---

## Explicitly out of scope

| Topic | Rationale |
|-------|-----------|
| Dedicated rollback route | Use `upgrade` with an older published `version` |
| Fleet head / promotion | Not required; latest change request wins via `LatestKnownVersion` |
| Auto-rollback on failed rollout | Operators choose a prior published version manually |
| Artifact download on install handler | Replicas download during poller materialization |
| Full static analysis of app bytecode | Publish-time + per-replica package validation suffice |

---

## Reverting to an older version

To roll back fleet-wide:

1. Confirm the target version is still listed in `GET …/app-registries/{registry}/apps/{app}/versions`.
2. `POST …/upgrade` with `{"version":"<older-version>","actor":"..."}`.
3. Wait for rollout convergence (admin UI makes this observable once shipped).

No new HTTP method. Catalog history retains both versions; `from_version` on the new change request reflects the previous fleet-known version.

---

## Error responses

Reuse the admin error envelope (`{"error":"..."}`). Prefer **400 Bad Request** for validation failures so clients distinguish user error from missing registry documents (**404** from fetch) and active rollout (**409**).

Example:

```json
{
  "error": "dependency slack: fleet-known version ^1.4.0 not satisfied (installed 1.3.0)"
}
```

Validation errors must not create or advance `app_rollouts` or `app_version_change_requests`.

---

## Testing

See [tests.md](./tests.md#install-time-validation-tests). Summary:

- HTTP integration tests on `add` / `upgrade` for each failure mode
- Unit tests on `InstallValidator` with stub catalog and registry entries
- Confirm failed validation leaves rollout and change-request stores unchanged

---

## Follow-up

- Admin read APIs and UI for rollout and replica convergence ([admin.md](./admin.md))
- **Future** — validate against live provider operation catalogs without an extra registry fetch; dry-run materialization on the handling instance (heavier, rarely needed)
