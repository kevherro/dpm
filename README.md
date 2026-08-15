# dpm

`dpm` is a tiny macOS package manager written in Go.

The v1 goal is a safe, deterministic binary install flow:

```sh
dpm install hello
hello
dpm list
dpm remove hello
```

Principles:

- no sudo
- no system path writes
- no arbitrary install scripts
- fixed artifact URLs with SHA-256 checksums
- isolated package prefixes under `~/.dpm/pkgs`
- executable symlinks under `~/.dpm/bin`

V1 supports macOS 14+ on Apple silicon. See [THREAT_MODEL.md](THREAT_MODEL.md)
for the filesystem boundary, state compatibility, exclusions, and exit-code
contract.

Commands:

```sh
dpm install <name>
dpm remove <name>
dpm list
dpm search <query>
dpm info <name>
dpm update
dpm doctor
dpm version [--verbose]
dpm help [command]
dpm help registry <command>
dpm registry validate [--verify-artifacts] <path>
dpm registry prepare [options] <path>
dpm registry generate-index <path>
```

The registry is a Git checkout at `~/.dpm/registry` with package
metadata under `packages/<name>/package.toml` and version manifests under
`packages/<name>/versions/<version>/dpm.toml`.

`dpm update` fetches that checkout from the anonymous official registry at
`https://github.com/kevherro/dpm-registry.git`. Set `DPM_REGISTRY_URL` to
another HTTPS Git URL or `file:///...` local repo while developing.

`dpm search` matches package names, summaries, homepages, and categories.
`dpm info` shows package metadata plus the selected non-yanked version.
`dpm help <command>` shows command-specific help without reading local dpm
configuration.
`dpm registry validate` checks registry structure and can optionally verify
artifact checksums.
`dpm registry prepare` is a maintainer helper that fetches one artifact URL,
computes its SHA-256, suggests executable `bins` from the archive, writes
package/version metadata, verifies a local install in a temporary `DPM_ROOT`,
and prints a review diff.
`dpm registry generate-index` builds generated static metadata under
`index/`, including `packages.json`, per-package `versions.json`, normalized
per-version manifests, and `.sha256` sidecars. Set
`DPM_REGISTRY_STATIC_INDEX=1` to make client reads use that generated index.
Pass `--snapshot-version <n> --signing-key-file <path>` to also write signed
`index/snapshot.json` metadata. Configure trusted Ed25519 public keys with
`DPM_REGISTRY_PUBLIC_KEYS` as comma-separated base64 keys; `dpm update` verifies
the signed snapshot and refuses rollback to an older accepted snapshot version.

Example package preparation:

```sh
go run ./cmd/dpm registry prepare \
  --name ripgrep \
  --version 15.1.0 \
  --url https://github.com/BurntSushi/ripgrep/releases/download/15.1.0/ripgrep-15.1.0-aarch64-apple-darwin.tar.gz \
  --summary "Recursively search directories for a regex pattern" \
  --homepage https://github.com/BurntSushi/ripgrep \
  --license "MIT OR Unlicense" \
  --category search \
  --category cli \
  /Users/kevin/Development/oss/dpm-registry
```

Example signed static index generation:

```sh
go run ./cmd/dpm registry generate-index \
  --snapshot-version 1 \
  --signing-key-file ./registry-signing.key \
  /Users/kevin/Development/oss/dpm-registry
```

Use `--bin <path>` when the executable archive paths need to be declared
manually. Use `--skip-install-verify` only when preparing metadata on a machine
that cannot run the local install verification step.

## Install dpm

dpm v1 supports macOS 14 or newer on Apple silicon. Git from the Xcode Command
Line Tools is required for registry updates:

```sh
xcode-select --install
```

Download `dpm_1.0.0_darwin_arm64.tar.gz` and `SHA256SUMS` from the
[v1.0.0 release](https://github.com/kevherro/dpm/releases/tag/v1.0.0), then
verify and install it without `sudo`:

```sh
shasum -a 256 -c SHA256SUMS
tar -xzf dpm_1.0.0_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
cp dpm_1.0.0_darwin_arm64/dpm "$HOME/.local/bin/dpm"
export PATH="$HOME/.local/bin:$HOME/.dpm/bin:$PATH"
dpm update
dpm install ripgrep
rg --version
dpm list
dpm install ripgrep
dpm remove ripgrep
```

Add the two PATH entries to your shell profile yourself if you want them in
future shells. dpm never edits shell configuration.

## Development and registry maintenance

End-to-end `hello` test:

```sh
go test ./cmd/dpm -run TestRunHelloEndToEnd -v
```

Real `ripgrep` flow using a local development registry:

```sh
export DPM_ROOT="$(mktemp -d /tmp/dpm-root.XXXXXX)"
export DPM_REGISTRY_URL="file:///Users/kevin/Development/oss/dpm-registry"

go run ./cmd/dpm update
go run ./cmd/dpm install ripgrep
"$DPM_ROOT/bin/rg" --version
go run ./cmd/dpm list
```

The local registry can be checked before installing:

```sh
go run ./cmd/dpm registry validate --verify-artifacts /Users/kevin/Development/oss/dpm-registry
```

Versioned builds:

```sh
bash scripts/build.sh
dist/dpm version --verbose
```

`VERSION` is the source version for local release builds. `scripts/build.sh`
injects `VERSION`, the current git commit, and a UTC build timestamp into the
binary with Go linker flags. It targets `darwin/arm64` by default; override
`GOOS`, `GOARCH`, `DPM_VERSION`, `DPM_COMMIT`, `DPM_DATE`, or `DPM_OUT` for
specific release jobs.

Troubleshooting:

- Checksum mismatch: dpm refuses to install when a downloaded artifact does not
  match the registry manifest. Run `dpm update` and try again. If it still
  fails, the registry entry or upstream artifact needs maintainer review.
- Link conflict: dpm will not overwrite unrelated files in `~/.dpm/bin`. Remove
  or rename the conflicting file, or run `dpm remove <name>` if the link belongs
  to an installed dpm package.
- State, prefix, link, or interrupted-operation drift: run `dpm doctor`. Doctor
  is read-only and names the evidence to inspect. V1 intentionally has no
  automatic repair command; preserve anything you need, then remove only the
  named stale path or reset the whole dpm root.
- Missing Git: install the Xcode Command Line Tools with
  `xcode-select --install`, then rerun `dpm update`.
- Rejected registry update: the previous checkout remains active. Run
  `dpm doctor`; if interrupted-update staging is reported, inspect and remove
  the named staging path before retrying.

## Storage, trust, and limitations

- Installed state is under `~/.dpm/state`; isolated package prefixes are under
  `~/.dpm/pkgs`; links are under `~/.dpm/bin`.
- Downloads are cached by SHA-256 under `~/.dpm/downloads`. Every cache hit and
  download is verified. `dpm remove` removes package state, its prefix, and its
  owned links, but intentionally retains cached downloads. Remove
  `~/.dpm/downloads` manually to clear the cache when no dpm command is running.
- Registry metadata is obtained over anonymous HTTPS Git. Artifact bytes are
  fetched from the pinned HTTPS URL in that metadata. SHA-256 proves the bytes
  match the registry declaration; it does not prove a declared binary is safe.
  Optional signed registry snapshots can additionally authenticate generated
  metadata when trusted keys are configured.
- V1 has no upgrades, downgrades, version constraints, dependency garbage
  collection, automatic repair, self-update, Intel support, source builds, or
  package install scripts. See [THREAT_MODEL.md](THREAT_MODEL.md) for the full
  support and security boundary.

Security issues should be reported privately as described in
[SECURITY.md](SECURITY.md).
