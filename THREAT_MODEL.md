# dpm v1 threat model

## Supported boundary

dpm v1 supports macOS 14 or later on Apple silicon (`darwin/arm64`). It
installs prebuilt `.tar.gz` artifacts declared by schema 1 manifests from one
official Git registry. Registry updates use anonymous HTTPS and require Apple
Git from the Xcode Command Line Tools. Artifact URLs are pinned and every
download and cache hit is verified against its declared SHA-256.

The default client root is the canonical `~/.dpm`. `DPM_ROOT`, when set, must
be a strict descendant of the canonical user home or canonical temporary
directory. The root and its managed `bin`, `pkgs`, `downloads`, `cache`,
`registry`, and `state` children may not be symlinks. Client mutations refuse
to run with effective UID 0.

Schema 1 package versions use canonical numeric `MAJOR.MINOR.PATCH`, with no
leading zeroes. Installed state uses schema 1. State written by v0.1.0 has no
schema and is explicitly incompatible; remove the old test/development root
before using dpm v1. There is no automatic state migration.

## Threats in scope

- hostile archive paths, links, special entries, and duplicate outputs
- corrupt or malicious installed state claiming unowned paths
- symlinked managed layout components
- checksum mismatch and poisoned cache entries
- conflicting package prefixes and bin entries
- ordinary filesystem and network failures
- interrupted operations and concurrent dpm processes

dpm must not mutate outside its canonical root, overwrite or remove paths it
cannot prove it owns, execute package-provided install logic, or edit shell
configuration.

## Exclusions

dpm does not defend against a malicious process already running as the same
user. SHA-256 proves that bytes match the registry declaration; it does not
prove that an installed binary is benign when the user later executes it.

V1 does not include Intel runtime support, source builds, scripts or hooks,
multiple registries, version constraints, upgrades or downgrades, lockfiles,
automatic PATH edits, automatic repair, self-update, or dependency garbage
collection. See `V1_PLAN.md` for the complete deferred list.

## CLI status

Process exit codes are stable: `0` means success, `1` means an operational
failure, and `2` means command-line usage failure.
