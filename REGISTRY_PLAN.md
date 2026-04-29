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

Rules:

- `schema` is required and must be integer `1`.
- `name` is required and must match the package directory name.
- `name` must be a local path component with no path separators.
- `summary` is required and must be a quoted string.
- `homepage` is required and must be a quoted string.
- `license` is required and must be a quoted string.
- `categories` is optional and must be a string array when present.
- Category names must be local path components with no path separators.
- Sections are not allowed in `package.toml`.
- Unknown keys are rejected.
- Duplicate keys are rejected.

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

Rules:

- `schema` is required and must be integer `1`.
- `name` and `version` are required and must match the registry path.
- `dependencies` is required and must be a string array.
- Every artifact must declare `os`, `arch`, `url`, and `sha256`.
- `[install]` must declare explicit `bins`.
- `yanked` is optional and defaults to `false`.
- `yank_reason` is optional and must be a quoted string when present.
- Unknown keys and duplicate keys are rejected.
- No executable install behavior is allowed.

## Ripgrep Example

The first real registry package should look like this:

```text
packages/
  ripgrep/
    package.toml
    versions/
      15.1.0/
        dpm.toml
```

`packages/ripgrep/package.toml`:

```toml
schema = 1
name = "ripgrep"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
categories = ["search", "cli"]
```

`packages/ripgrep/versions/15.1.0/dpm.toml`:

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

## Version Deletion Policy

Published versions should not be deleted from the registry. A deleted version
breaks reproducibility for anyone with a lockfile, cached state, old commit, or
support request that refers to that version.

Use yanking instead:

- Set `yanked = true` when a version should not be chosen by default.
- Add `yank_reason` when the reason is useful for users or maintainers.
- Keep the original `dpm.toml` path in place.
- Publish a new version when the artifact or metadata needs replacement.

The registry validator should eventually flag deleted versions by comparing
against registry history, but the policy is useful before enforcement exists.

## Milestone 1: Registry Schema

- [x] Define `registry.toml` schema.
- [x] Define `package.toml` schema.
- [x] Move version manifests to `packages/<name>/versions/<version>/dpm.toml`.
- [x] Add `schema = 1` support to manifests.
- [x] Add yanking fields: `yanked` and optional `yank_reason`.
- [x] Document the `ripgrep` example.
- [x] Decide whether deleted versions are forbidden by policy.

## Milestone 2: Client Registry Compatibility

- [x] Teach `internal/registry` to read the new `versions/` layout.
- [x] Add package metadata parsing for `package.toml`.
- [x] Update version resolution to skip yanked versions by default.
- [x] Preserve compatibility with current test fixtures only if cheap.
- [x] Update `dpm search` to use name, summary, homepage, and categories.
- [x] Update `dpm info` to show package metadata plus selected version data.
- [x] Add exact-version parsing design for later `name@version` support.

Exact-version parsing design:

- A package spec is either `<name>` or `<name>@<version>`.
- `<name>` resolves to the newest non-yanked version.
- `<name>@<version>` resolves exactly that version with `ResolveVersion`.
- Empty names, empty versions, extra `@`, and path separators are invalid.
- Reserve `@` out of package names before enabling this syntax.
- Exact CLI installs should still reject yanked versions by default and include
  `yank_reason` in the error when present.
- Future lockfile restore can explicitly allow yanked exact versions for
  reproducibility, because a lockfile represents an existing prior resolution.

## Milestone 3: Git-Backed `dpm update`

- [x] Add `DPM_REGISTRY_URL`.
- [x] Pick a default registry URL placeholder.
- [x] Clone registry when `~/.dpm/registry` is missing.
- [x] Pull with `git -C ~/.dpm/registry pull --ff-only` when present.
- [x] Reject non-git registry directories.
- [x] Reject dirty registry checkout with a useful error.
- [x] Support local registry URLs, including `file:///...`, for tests.
- [x] Make install errors suggest `dpm update` when registry is missing.
- [x] Add focused update tests using a temporary local Git repo.

## Milestone 4: Registry Validation

- [x] Add `dpm registry validate <path>` or `dpm doctor --registry`.
- [x] Validate `registry.toml`.
- [x] Validate package directory names match `package.toml`.
- [x] Validate version directory names match `dpm.toml`.
- [x] Validate package names, versions, dependencies, artifacts, and bins.
- [x] Reject mutable URLs such as `latest` and branch archives.
- [x] Ensure every package version has a `darwin/arm64` artifact for v1.
- [x] Ensure dependencies resolve inside the registry.
- [x] Ensure yanked versions include useful reasons when applicable.
- [x] Add optional artifact verification mode that downloads and checks hashes.

## Milestone 5: First Real Registry

- [x] Create a separate `dpm-registry` Git repo.
- [x] Add `registry.toml`.
- [x] Add `ripgrep/package.toml`.
- [x] Add `ripgrep` `15.1.0` manifest with the verified Apple Silicon asset.
- [x] Run registry validation.
- [x] Test `dpm update && dpm install ripgrep` from a clean `DPM_ROOT`.
- [x] Document the real-package quickstart.

## Milestone 6: User Experience

- [x] Improve missing-registry errors to suggest `dpm update`.
- [x] Improve stale-registry errors when a package is not found.
- [x] Make `dpm doctor` report registry URL, registry path, and current commit.
- [x] Add README troubleshooting for checksum mismatch.
- [x] Add README troubleshooting for link conflicts.
- [x] Consider auto-running `dpm update` only after explicit user opt-in.

Auto-update decision: do not run `dpm update` implicitly in v1. Missing or stale
registry errors should suggest the explicit command. A future opt-in flag or
config setting can add auto-update without making installs unexpectedly touch
the network by default.

## Milestone 7: Package Maintenance Tooling

- [x] Add a maintainer helper to create/bump package manifests.
- [x] Fetch release asset metadata from a provided URL.
- [x] Compute SHA-256 automatically.
- [x] Inspect archive layout and suggest `bins`.
- [x] Run local install verification in a temporary `DPM_ROOT`.
- [x] Generate a ready-to-review registry diff.

Milestone 7 command:

```sh
dpm registry prepare [options] <registry-path>
```

The helper fetches one `file://` or `https://` artifact, computes its SHA-256,
uses executable archive entries as default `bins`, writes `package.toml` when
missing, writes a new `versions/<version>/dpm.toml`, validates the registry,
verifies `dpm install <name>` against a temporary root, and prints a review diff
for the generated files. Existing `package.toml` files are reused for version
bumps.

## Milestone 8: Generated Static Index

- [x] Generate `index/packages.json`.
- [x] Generate per-package `versions.json`.
- [x] Generate per-version normalized manifests.
- [x] Add checksums for generated metadata.
- [x] Teach client to consume the static index behind a feature flag.
- [x] Keep Git as the authoring source.

Milestone 8 command:

```sh
dpm registry generate-index <registry-path>
```

Generated static distribution lives under `index/`:

```text
index/
  packages.json
  packages.json.sha256
  packages/<name>/versions.json
  packages/<name>/versions.json.sha256
  packages/<name>/versions/<version>/dpm.json
  packages/<name>/versions/<version>/dpm.json.sha256
```

`packages.json` includes checksums for per-package version indexes, and each
`versions.json` includes checksums for normalized per-version manifests. The
source `packages/<name>/package.toml` and `versions/<version>/dpm.toml` layout
remains the authoring source. Clients can read the generated index with:

```sh
DPM_REGISTRY_STATIC_INDEX=1 dpm install <name>
```

## Milestone 9: Signed Snapshots

- [x] Define signed snapshot metadata.
- [x] Decide whether to implement TUF directly or a smaller signed-release file.
- [x] Add registry public-key configuration.
- [x] Verify snapshot signatures in `dpm update`.
- [x] Prevent rollback using stored snapshot version/checkpoint state.
- [x] Add key rotation story before relying on signatures.

Milestone 9 uses a smaller signed-release file instead of full TUF. The source
registry remains Git-authored, while generated distribution metadata can include:

```text
index/
  snapshot.json
  snapshot.json.sha256
  snapshot.json.sig
  snapshot.json.sig.sha256
```

`snapshot.json` contains schema, monotonic version, registry name, and checksums
for generated metadata files. `snapshot.json.sig` is a detached Ed25519
signature over the canonical generated `snapshot.json` bytes. The command shape
is:

```sh
dpm registry generate-index \
  --snapshot-version <n> \
  --signing-key-file <base64-ed25519-key-file> \
  <registry-path>
```

Clients trust keys via comma-separated base64 Ed25519 public keys:

```sh
DPM_REGISTRY_PUBLIC_KEYS=<key1>,<key2> dpm update
```

When keys are configured, `dpm update` verifies the signed snapshot after the
Git clone/pull and records the accepted snapshot version, checksum, key id, and
verification time in state. A future update is rejected if it presents a lower
snapshot version, or if the same version has different snapshot bytes.

Key rotation story:

- Publish a client/config update that trusts both old and new public keys.
- Sign snapshots with the new key after enough clients trust both keys.
- Keep the old key trusted during the overlap window.
- Remove the old key only after old-key-only clients are no longer supported.
- If a key is compromised, publish a higher-version snapshot signed by a still
  trusted replacement key and remove the compromised key from trusted config.

## Milestone 10: Performance And Benchmarks

- [ ] Add focused benchmarks for manifest parsing.
- [ ] Add registry search benchmarks over synthetic package sets.
- [ ] Add version-resolution benchmarks with yanked and non-yanked versions.
- [ ] Add install-path benchmarks around link planning and state writes.
- [ ] Add a fixture generator for benchmark registries.
- [ ] Track benchmark commands in `test.sh` or a separate `bench.sh`.

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
