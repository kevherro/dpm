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

The registry is currently a local checkout at `~/.dpm/registry` with package
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

End-to-end `hello` demo:

```sh
go test ./cmd/dpm -run TestRunHelloEndToEnd -v
```

Real `ripgrep` demo using the local development registry:

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

This project is at `v0.1.0`: the core end-to-end flow works, and the remaining
work is safety hardening, trusted distribution, and release productization. See
[V1_PLAN.md](V1_PLAN.md) for the path to `v1.0.0`.
