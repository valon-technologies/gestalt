# Bugbot fix log — registry app install (gestalt#2730)

Historical notes from the step 6 implementation on branch `app-registry-install-v2`.

PR: https://github.com/valon-technologies/gestalt/pull/2730

Supersedes gestalt#2729 (backup/pending/`app_installations` design) and gestalt#2720.

---

## gestalt#2730 design (current)

- **Events → catalog** — `app_installation_events` / fleet head projection replaced by `app_version_catalog` (`version_added`, `install_failed`).
- **Fleet install lock** — `app_version_install_locks` with per-request holder id; 409 while another install is in flight.
- **No re-install** — `POST …/install` returns 400 when `(app, version)` is already known; no re-materialize.
- **No artifact delete on failure** — `failInstall` does not `RemoveAll` materialized trees.
- **Catalog write failures** — post-materialize IndexedDB errors do not append `install_failed`; a later install can recover the catalog row.
- **Early failures audited** — registry fetch / validation failures append `install_failed` before download.

See [plan.md](./plan.md), [api.md](./api.md), and [indexeddb.md](./indexeddb.md) for the current model.

---

## gestalt#2729 fixes (obsolete — pending/backup design)

<details>
<summary>Archived</summary>

- **Pending update wipes install metadata** — `applyPendingInstall` preserved gold fields during pending upgrades.
- **Failed mark after on-disk promote** — Staging directory pattern before DB promote.
- **Success returns error on audit** — Best-effort promoted lifecycle events.
- **Promoted before artifact promote** — DB promote only after artifacts on disk.
- **Failed upgrade drops gold metadata** — `restoreBaselineForInstall` on failure.
- **Retry from failed install preserves metadata** — `shouldPreservePriorMetadata`.
- **Previous version lost on retry** — `previousVersionForInstall`.

</details>
