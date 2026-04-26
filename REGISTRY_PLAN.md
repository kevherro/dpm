# Registry Plan

## Goal

Make this work for real packages:

```sh
dpm update
dpm install ripgrep
rg --version
```

The immediate registry should be simple enough to build now, but should not
force a rewrite if dpm later needs faster distribution, stronger trust, or
multiple registries.

## Mental Model

Treat the registry as a pipeline with separate layers:

```text
human-reviewed source -> generated distribution format -> verified client cache
```

Layer responsibilities:

- Authoring: humans edit and review package metadata.
- Distribution: clients fetch package metadata efficiently.
- Artifact fetching: clients download pinned upstream artifacts.
- Trust: clients decide whether registry metadata and artifacts are acceptable.

The recommended path is:

```text
Authoring source:       Git repo
Client v1 distribution: git clone/pull
Client v2 distribution: generated static HTTP index
Trust v1:               Git repo + SHA-256 artifact pins
Trust v2:               signed registry snapshots
Trust v3:               transparency log / attestations
```

## Approaches Considered

### Git Registry

A curated Git repo contains package metadata and version manifests.

Strengths:

- Simple to implement.
- Easy to review with pull requests.
- Works offline after `dpm update`.
- No server or database required.
- Git history gives a basic audit trail.

Weaknesses:

- Git is not an ideal query protocol.
- Large registries become slower to clone/pull.
- Trust is mostly “trust this Git repo.”

Use for:

- v1 source of truth.
- v1 client distribution.

### Generated Static HTTP Registry

Git remains the source of truth, but CI generates static indexes and manifests.

Strengths:

- CDN-friendly.
- Fast package metadata fetches.
- No Git dependency in the client.
- Natural migration from a Git source registry.

Weaknesses:

- Requires a publishing pipeline.
- Creates both source files and generated files.
- Needs snapshot/versioning rules.

Use for:

- v2 distribution layer.

### TUF-Style Signed Registry

Use signed metadata roles for root, targets, snapshot, and timestamp.

Strengths:

- Designed for package/update systems.
- Protects against rollback, freeze, and mix-and-match attacks.
- Supports key rotation and threshold signatures.

Weaknesses:

- Adds key management.
- Adds metadata generation.
- Adds client verification complexity.

Use for:

- v2/v3 trust layer, after the basic registry works.

### Apt-Style Signed Release Index

Publish a signed top-level release/index containing checksums of package indexes,
which themselves contain checksums of artifacts.

Strengths:

- Proven repository model.
- Mirror-friendly.
- Simpler than full TUF.

Weaknesses:

- Still needs trusted keys.
- Less complete rollback/freeze protection than TUF unless extended.
- More distro-shaped than dpm needs.

Use for:

- Inspiration for signed snapshot metadata.

### OCI Artifact Registry

Store dpm manifests and artifacts as OCI artifacts in an OCI registry.

Strengths:

- Reuses existing registry infrastructure.
- Content-addressed blobs and manifests are built in.
- Auth, replication, and hosted registries already exist.

Weaknesses:

- Awkward human authoring workflow.
- Package search/version semantics are not natural.
- More client complexity.

Use for:

- Possible future binary mirroring backend.

### Content-Addressed Store

Use content hashes as primary identifiers for manifests and artifacts.

Strengths:

- Strong determinism.
- Excellent cache and mirror properties.
- Clean supply-chain model.

Weaknesses:

- Poor hand-authoring UX.
- Needs tooling around hash-addressed objects.
- More ceremony for a small package manager.

Use for:

- Future generated index and cache format.

### Transparency Log

Record accepted manifests or registry snapshots in an append-only log.

Strengths:

- Makes quiet history rewriting detectable.
- Gives public auditability.
- Complements signatures.

Weaknesses:

- Needs log service or integration.
- Needs inclusion proof verification or monitoring.
- Does not replace registry validation.

Use for:

- Future trust/audit layer.

## Recommendation

Use a Git registry as the authoring source and initial client distribution
format. Design its schema so it can later generate a static HTTP registry and
signed snapshots.

Do not build a hosted API, database registry, OCI backend, or TUF implementation
until the package model is proven.

## Target Git Registry Layout

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

Root registry metadata:

```toml
schema = 1
name = "dpm-core"
description = "Core dpm package registry"
```

Rules:

- `schema` is required and must be integer `1`.
- `name` is required and must be a quoted string.
- `name` must not contain path separators.
- `description` is optional and must be a quoted string when present.
- Sections are not allowed in `registry.toml`.
- Unknown keys are rejected.
- Duplicate keys are rejected.

Package metadata:

```toml
schema = 1
name = "ripgrep"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
categories = ["search", "cli"]
```

Version install manifest:

```toml
schema = 1
name = "ripgrep"
version = "15.1.0"
dependencies = []
yanked = false

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "https://github.com/BurntSushi/ripgrep/releases/download/15.1.0/ripgrep-15.1.0-aarch64-apple-darwin.tar.gz"
sha256 = "378e973289176ca0c6054054ee7f631a065874a352bf43f0fa60ef079b6ba715"

[install]
bins = ["ripgrep-15.1.0-aarch64-apple-darwin/rg"]
```

## Milestone 1: Registry Schema

- [x] Define `registry.toml` schema.
- [ ] Define `package.toml` schema.
- [ ] Move version manifests to `packages/<name>/versions/<version>/dpm.toml`.
- [ ] Add `schema = 1` support to manifests.
- [ ] Add yanking fields: `yanked` and optional `yank_reason`.
- [ ] Document the `ripgrep` example.
- [ ] Decide whether deleted versions are forbidden by policy.

## Milestone 2: Client Registry Compatibility

- [ ] Teach `internal/registry` to read the new `versions/` layout.
- [ ] Add package metadata parsing for `package.toml`.
- [ ] Update version resolution to skip yanked versions by default.
- [ ] Preserve compatibility with current test fixtures only if cheap.
- [ ] Update `dpm search` to use name, summary, homepage, and categories.
- [ ] Update `dpm info` to show package metadata plus selected version data.
- [ ] Add exact-version parsing design for later `name@version` support.

## Milestone 3: Git-Backed `dpm update`

- [ ] Add `DPM_REGISTRY_URL`.
- [ ] Pick a default registry URL placeholder.
- [ ] Clone registry when `~/.dpm/registry` is missing.
- [ ] Pull with `git -C ~/.dpm/registry pull --ff-only` when present.
- [ ] Reject non-git registry directories.
- [ ] Reject dirty registry checkout with a useful error.
- [ ] Support local registry URLs, including `file:///...`, for tests.
- [ ] Make install errors suggest `dpm update` when registry is missing.
- [ ] Add focused update tests using a temporary local Git repo.

## Milestone 4: Registry Validation

- [ ] Add `dpm registry validate <path>` or `dpm doctor --registry`.
- [ ] Validate `registry.toml`.
- [ ] Validate package directory names match `package.toml`.
- [ ] Validate version directory names match `dpm.toml`.
- [ ] Validate package names, versions, dependencies, artifacts, and bins.
- [ ] Reject mutable URLs such as `latest` and branch archives.
- [ ] Ensure every package version has a `darwin/arm64` artifact for v1.
- [ ] Ensure dependencies resolve inside the registry.
- [ ] Ensure yanked versions include useful reasons when applicable.
- [ ] Add optional artifact verification mode that downloads and checks hashes.

## Milestone 5: First Real Registry

- [ ] Create a separate `dpm-registry` Git repo.
- [ ] Add `registry.toml`.
- [ ] Add `ripgrep/package.toml`.
- [ ] Add `ripgrep` `15.1.0` manifest with the verified Apple Silicon asset.
- [ ] Run registry validation.
- [ ] Test `dpm update && dpm install ripgrep` from a clean `DPM_ROOT`.
- [ ] Document the real-package quickstart.

## Milestone 6: User Experience

- [ ] Improve missing-registry errors to suggest `dpm update`.
- [ ] Improve stale-registry errors when a package is not found.
- [ ] Make `dpm doctor` report registry URL, registry path, and current commit.
- [ ] Add README troubleshooting for checksum mismatch.
- [ ] Add README troubleshooting for link conflicts.
- [ ] Consider auto-running `dpm update` only after explicit user opt-in.

## Milestone 7: Package Maintenance Tooling

- [ ] Add a maintainer helper to create/bump package manifests.
- [ ] Fetch release asset metadata from a provided URL.
- [ ] Compute SHA-256 automatically.
- [ ] Inspect archive layout and suggest `bins`.
- [ ] Run local install verification in a temporary `DPM_ROOT`.
- [ ] Generate a ready-to-review registry diff.

## Milestone 8: Generated Static Index

- [ ] Generate `index/packages.json`.
- [ ] Generate per-package `versions.json`.
- [ ] Generate per-version normalized manifests.
- [ ] Add checksums for generated metadata.
- [ ] Teach client to consume the static index behind a feature flag.
- [ ] Keep Git as the authoring source.

## Milestone 9: Signed Snapshots

- [ ] Define signed snapshot metadata.
- [ ] Decide whether to implement TUF directly or a smaller signed-release file.
- [ ] Add registry public-key configuration.
- [ ] Verify snapshot signatures in `dpm update`.
- [ ] Prevent rollback using stored snapshot version/checkpoint state.
- [ ] Add key rotation story before relying on signatures.

## Failure Points To Watch

Authoring:

- Upstream archives change layout and break declared bins.
- Maintainers paste wrong checksums.
- Manual package updates become repetitive.
- Version sorting fails on pre-releases or date-based versions.
- Yanking rules are unclear.

Distribution:

- Git registry grows too large.
- Dirty user registry checkouts block updates.
- Network failures make updates flaky.
- Some machines lack Git or block GitHub.

Artifact fetching:

- Upstream release assets disappear.
- Rate limits or CDN outages block installs.
- Immutable-looking URLs may still be replaced upstream.
- Large interrupted downloads need careful cache cleanup.

Trust:

- Registry Git repo is a single trust root.
- Compromised maintainer can merge malicious URL plus checksum.
- Compromised upstream can publish malicious binaries.
- Unsigned metadata permits rollback if update transport is compromised.

User experience:

- Forgetting `dpm update` before install is annoying.
- Link conflicts are safe but can be confusing.
- No exact version install becomes limiting quickly.
- No upgrade command makes long-term use clumsy.

## Deferred

- [ ] Multiple registries/taps.
- [ ] Exact version install syntax, such as `dpm install ripgrep@15.1.0`.
- [ ] Upgrade command.
- [ ] Binary artifact mirroring.
- [ ] OCI artifact backend.
- [ ] IPFS/CID mirror backend.
- [ ] Transparency log integration.
