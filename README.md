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
```

The registry is currently a local checkout at `~/.dpm/registry` with package
manifests under `packages/<name>/<version>/dpm.toml`.

End-to-end `hello` demo:

```sh
go test ./cmd/dpm -run TestRunHelloEndToEnd -v
```

This project is early and intentionally boring. The first milestone is a small
end-to-end demo package, backed by real tests.
