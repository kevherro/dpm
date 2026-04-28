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

Commands:

```sh
dpm install <name>
dpm remove <name>
dpm list
dpm search <query>
dpm info <name>
dpm update
dpm doctor
dpm registry validate [--verify-artifacts] <path>
```

The registry is currently a local checkout at `~/.dpm/registry` with package
metadata under `packages/<name>/package.toml` and version manifests under
`packages/<name>/versions/<version>/dpm.toml`.

`dpm update` clones or fast-forwards that checkout. The default registry URL is
a placeholder; set `DPM_REGISTRY_URL` to a Git URL or `file:///...` local repo
while developing.

`dpm search` matches package names, summaries, homepages, and categories.
`dpm info` shows package metadata plus the selected non-yanked version.
`dpm registry validate` checks registry structure and can optionally verify
artifact checksums.

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

Troubleshooting:

- Checksum mismatch: dpm refuses to install when a downloaded artifact does not
  match the registry manifest. Run `dpm update` and try again. If it still
  fails, the registry entry or upstream artifact needs maintainer review.
- Link conflict: dpm will not overwrite unrelated files in `~/.dpm/bin`. Remove
  or rename the conflicting file, or run `dpm remove <name>` if the link belongs
  to an installed dpm package.

This project is early and intentionally boring. The first milestone is a small
end-to-end demo package, backed by real tests.
