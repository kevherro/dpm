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

This project is early and intentionally boring. The first milestone is a small
end-to-end demo package, backed by real tests.
