# Gestalt Python SDK

This package provides the Python authoring surface for executable Gestalt
provider plugins.

It is intended to be used by source plugins discovered through
`[tool.gestalt]` in `pyproject.toml` and by packaged plugins built from that
same source tree.

Python source plugins are developed locally via `from.source.path` and
released through `gestaltd plugin release` as current-platform executable
artifacts.

## Publishing

The SDK is published as the `gestalt` package to a Google Artifact Registry
Python repository.

Release tags stay aligned with the repo's SDK tag convention:

- `sdk/python/v0.0.1`
- `sdk/python/v0.0.1-alpha.1`
- `sdk/python/v0.0.1-beta.1`
- `sdk/python/v0.0.1-rc.1`

The release workflow normalizes those tag versions to PEP 440 before building
and publishing with `uv`, so `sdk/python/v0.0.1-alpha.1` becomes package
version `0.0.1a1`.

The GitHub Actions workflow expects this repository configuration:

- secret `GCP_PROJECT_ID`
- secret `GCP_PROJECT_NUMBER`
- variable `GCP_REGION`
- variable `GAR_PYTHON_REPOSITORY`

The publish job uses GitHub OIDC plus `google-github-actions/auth` to mint a
short-lived Google access token, then calls `uv publish` against:

```text
https://<REGION>-python.pkg.dev/<PROJECT_ID>/<REPOSITORY>/
```

## Consuming From Google Artifact Registry

In an internal plugin repo, pin `gestalt` to the Artifact Registry index with
`uv` so the package does not fall back to PyPI:

```toml
[[tool.uv.index]]
name = "valon-python"
url = "https://<REGION>-python.pkg.dev/<PROJECT_ID>/<REPOSITORY>/simple/"
explicit = true
authenticate = "always"

[tool.uv.sources]
gestalt = { index = "valon-python" }

[project]
dependencies = ["gestalt==0.0.1a1"]
```

For CI or non-interactive installs, get a short-lived access token from Google
Cloud and pass it to `uv` with the index credentials that Artifact Registry
expects:

```sh
export ARTIFACT_REGISTRY_TOKEN="$(
  gcloud auth application-default print-access-token
)"
export UV_INDEX_VALON_PYTHON_USERNAME=oauth2accesstoken
export UV_INDEX_VALON_PYTHON_PASSWORD="$ARTIFACT_REGISTRY_TOKEN"
```

Locally, you can also use `keyring` with
`keyrings.google-artifactregistry-auth`, but the access-token flow above is the
simplest baseline and works cleanly with `uv`.

That lets `~/src/gestalt-plugins` depend on `gestalt` like any other Python
package while keeping the SDK on Google-managed infrastructure instead of the
public package indexes.
