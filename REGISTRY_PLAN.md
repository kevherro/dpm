# Git Registry Plan

## Goal

Make this work with a curated Git-backed registry:

```sh
dpm update
dpm install ripgrep
rg --version
```

The registry is the trust root for package metadata. Artifacts remain hosted
upstream and must be pinned by immutable-looking URLs and SHA-256 checksums.

## Milestone 1: Registry Schema

- [ ] Define root `registry.toml` schema.
- [ ] Define package metadata schema in `packages/<name>/package.toml`.
- [ ] Move install manifests to `packages/<name>/versions/<version>/dpm.toml`.
- [ ] Decide yanking fields for version manifests.
- [ ] Document schema examples for `ripgrep`.

Target registry layout:

```text
dpm-registry/
  registry.toml
  packages/
    ripgrep/
      package.toml
      versions/
        15.1.0/
          dpm.toml
```

## Milestone 2: Client Registry Compatibility

- [ ] Teach `internal/registry` to read the new `versions/` layout.
- [ ] Preserve compatibility with the current test fixture layout only if it
      stays cheap; otherwise migrate tests.
- [ ] Add package metadata parsing for `package.toml`.
- [ ] Update `dpm search` to use package metadata: name, summary, categories.
- [ ] Update `dpm info` to include package metadata plus selected version data.

## Milestone 3: Git-Backed `dpm update`

- [ ] Add config for `DPM_REGISTRY_URL`.
- [ ] Pick default registry URL placeholder.
- [ ] Implement clone when `~/.dpm/registry` is missing.
- [ ] Implement `git -C ~/.dpm/registry pull --ff-only` when present.
- [ ] Reject dirty/non-git registry directories with useful errors.
- [ ] Support local registry URLs, including `file:///...`, for tests.
- [ ] Add focused update tests using a local temporary Git repo.

## Milestone 4: Registry Validation

- [ ] Add `dpm registry validate <path>` or `dpm doctor --registry`.
- [ ] Validate `registry.toml`.
- [ ] Validate package directory names match `package.toml`.
- [ ] Validate version directory names match `dpm.toml`.
- [ ] Validate package names, versions, dependencies, artifacts, and bins.
- [ ] Reject mutable URLs such as `latest` and branch archives.
- [ ] Ensure every package version has a `darwin/arm64` artifact for v1.
- [ ] Ensure dependencies resolve inside the registry.
- [ ] Add optional artifact verification mode later.

## Milestone 5: First Real Registry

- [ ] Create a separate `dpm-registry` Git repo.
- [ ] Add `registry.toml`.
- [ ] Add `ripgrep/package.toml`.
- [ ] Add `ripgrep` `15.1.0` manifest with the verified Apple Silicon asset.
- [ ] Run registry validation.
- [ ] Test `dpm update && dpm install ripgrep` from a clean `DPM_ROOT`.

## Milestone 6: User Experience

- [ ] Improve missing-registry install errors to suggest `dpm update`.
- [ ] Make `dpm doctor` report registry URL, registry path, and current commit.
- [ ] Add README quickstart for real package installs.
- [ ] Add a troubleshooting section for checksum mismatch and link conflicts.

## Deferred

- [ ] Static HTTP registry generated from Git.
- [ ] Signed registry tags or commits.
- [ ] Content-addressed manifest index.
- [ ] Transparency log.
- [ ] Multiple taps/registries.
- [ ] Exact version install syntax, such as `dpm install ripgrep@15.1.0`.
- [ ] Binary artifact mirroring.
