# agents.md

## project

this repo builds `dpm`, a tiny macos package manager written in go.

dpm's v1 goal is not to clone homebrew. v1 proves a safe, deterministic install
flow:

```text
dpm install hello
hello
dpm list
dpm remove hello
```

core principles:

- no sudo
- no writes to `/usr`, `/usr/local`, `/opt`, or other system paths
- no arbitrary install scripts
- no mutable “latest” downloads
- no unverified artifacts
- no global file soup
- no hidden package ownership
- boring, testable filesystem behavior

default install root:

```text
~/.dpm
```

default layout:

```text
~/.dpm/
  bin/
  pkgs/
  downloads/
  cache/
  registry/
  state/
```

packages should install into isolated prefixes:

```text
~/.dpm/pkgs/<name>/<version>/
```

exposed executables should be symlinked into:

```text
~/.dpm/bin/
```

## agent operating rules

before editing code:

1. inspect the existing tree
2. read relevant files before changing them
3. prefer small, reviewable diffs
4. preserve existing public behavior unless the task asks otherwise
5. add or update tests for behavior changes
6. run the narrowest relevant tests first
7. run broader tests before claiming completion

never claim success unless the relevant command actually passed.

if a command fails, report:

- command run
- failure summary
- likely cause
- files changed so far

do not paper over errors.

## go toolchain expectations

this project is go-first. use the go toolchain aggressively.

always prefer:

```sh
go test ./...
go test ./internal/...
go test ./cmd/...
go vet ./...
go fmt ./...
```

for focused work, run package-specific tests:

```sh
go test ./internal/manifest
go test ./internal/install
go test ./internal/registry
```

before finishing any code change, run:

```sh
go fmt ./...
go test ./...
```

run `go vet ./...` when touching package boundaries, filesystem code, process
execution, archive handling, or error paths.

## use gopls

use `gopls` as the main feedback loop when available.

prefer `gopls` for:

- finding definitions
- checking diagnostics
- locating references
- validating imports
- understanding package apis
- safely renaming symbols
- navigating unfamiliar code

use commands like:

```sh
gopls version
gopls check ./...
gopls references <file.go>:<line>:<col>
gopls definition <file.go>:<line>:<col>
gopls imports -w <file.go>
```

when unsure about an identifier, type, or package boundary, ask `gopls` before
guessing.

do not manually fight imports. use `gopls imports -w` or `go fmt`.

## use godoc / go doc

use `go doc` constantly.

before using unfamiliar stdlib or third-party apis, check docs locally:

```sh
go doc os
go doc os.Symlink
go doc os.RemoveAll
go doc filepath
go doc net/http
go doc archive/tar
go doc compress/gzip
go doc crypto/sha256
go doc encoding/hex
go doc testing
```

for project packages:

```sh
go doc ./internal/manifest
go doc ./internal/registry
go doc ./internal/install
```

do not hallucinate go apis. verify with `go doc`.

when choosing between a dependency and the stdlib, prefer the stdlib unless the
dependency clearly reduces complexity.

## code style

write boring go.

preferred:

- small packages
- small interfaces
- explicit errors
- table-driven tests
- `context.Context` for network operations
- `t.TempDir()` for filesystem tests
- `filepath.Join`, never string-concatenated paths
- `errors.Is` / `errors.As` where useful
- wrapped errors with context using `fmt.Errorf("...: %w", err)`

avoid:

- clever abstractions
- global mutable state
- package-level configuration
- hidden filesystem writes
- shelling out unless explicitly required
- broad interfaces with one implementation
- goroutines unless there is a clear need
- panics outside impossible programmer errors

bad:

```go
func Install(name string) {
    // mutates ~/.dpm directly, exits on error
}
```

good:

```go
func Install(ctx context.Context, cfg Config, name string) error {
    // explicit config, explicit error
}
```

## package layout

prefer this repo shape:

```text
cmd/dpm/
  main.go

internal/archive/
internal/checksum/
internal/config/
internal/install/
internal/link/
internal/manifest/
internal/registry/
internal/state/
internal/testutil/
```

package responsibilities:

### `cmd/dpm`

cli parsing and presentation only.

it should not contain install logic.

### `internal/config`

dpm root path, default paths, env overrides.

### `internal/manifest`

parse and validate `dpm.toml`.

manifest format should be declarative only. no executable recipes.

### `internal/registry`

find packages in a local registry folder.

registry layout:

```text
registry/
  packages/
    hello/
      1.0.0/
        dpm.toml
```

### `internal/checksum`

sha256 verification.

checksum mismatch must be a hard failure.

### `internal/archive`

extract `.tar.gz` safely.

must defend against path traversal.

reject archive entries that escape the target directory.

### `internal/install`

orchestrates install:

1. resolve manifest
2. choose artifact for current platform
3. fetch or read artifact
4. verify sha256
5. extract into isolated package prefix
6. link bins
7. write state

### `internal/link`

create/remove symlinks in `~/.dpm/bin`.

must detect conflicts.

do not overwrite unrelated files.

### `internal/state`

track installed packages.

state should include at least:

```text
name
version
source url or local source
sha256
install prefix
bins linked
dependencies
installed_at
```

## manifest rules

prefer toml.

example:

```toml
name = "hello"
version = "1.0.0"

dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://./fixtures/hello-1.0.0-darwin-arm64.tar.gz"
sha256 = "..."

[install]
bins = ["hello"]
```

rules:

- `name` is required
- `version` is required
- artifact checksums are required
- artifact os/arch are required
- install bins must be declared
- reject unknown dangerous behavior
- no postinstall scripts
- no arbitrary shell
- no implicit bin discovery in v1 unless explicitly requested

## filesystem safety

filesystem code must be paranoid.

requirements:

- never write outside configured dpm root
- never delete outside configured dpm root
- use temp dirs for staging
- prefer atomic-ish rename when practical
- verify paths after cleaning/resolving
- reject archive path traversal
- reject absolute paths in archives
- reject symlink tricks during extraction where relevant
- never overwrite user files in `~/.dpm/bin`

tests must cover dangerous paths like:

```text
../evil
../../.ssh/id_rsa
/absolute/path
bin/../../evil
```

## network rules

network fetches are allowed only for pinned artifacts.

allowed:

```toml
url = "https://example.com/foo-1.0.0.tar.gz"
sha256 = "..."
```

not allowed:

```toml
url = "https://example.com/foo-latest.tar.gz"
```

not allowed:

```toml
url = "https://github.com/org/repo/archive/main.tar.gz"
```

rules:

- verify sha256 after every download
- cache by checksum, not just filename
- support local `file://` artifacts for tests
- design for future `--offline`
- use timeouts/context for http

## cli behavior

initial commands:

```sh
dpm install <name>
dpm remove <name>
dpm list
dpm search <query>
dpm info <name>
dpm update
dpm doctor
```

success output should be terse.

example:

```text
installing hello 1.0.0
linked ~/.dpm/bin/hello
done
```

errors should be specific and useful.

bad:

```text
install failed
```

good:

```text
error: checksum mismatch for hello 1.0.0

expected: abc...
actual:   def...

refusing to install because the artifact does not match the registry manifest
```

## testing standards

use real tests from the start.

minimum coverage:

- parse valid manifest
- reject invalid manifest
- resolve package from local registry
- reject missing package
- reject bad checksum
- install local tarball
- create bin symlink
- refuse bin conflict
- list installed packages
- remove installed package
- reinstall idempotently
- reject archive path traversal
- preserve files outside dpm root

use:

```go
t.TempDir()
```

do not write tests against the real home directory.

tests must not require network unless explicitly marked integration tests.

prefer fixture archives generated inside tests when possible.

## dependencies

prefer stdlib.

acceptable early dependencies:

- toml parser
- cli parser, only if it genuinely helps

do not add heavy frameworks.

do not add a logging framework for v1.

do not add a database.

state can be toml or json on disk.

## error handling

all exported-ish internal functions should return errors.

wrap errors with useful context:

```go
return fmt.Errorf("extract %s into %s: %w", archivePath, dst, err)
```

do not call `os.Exit` outside `cmd/dpm`.

do not print from internal packages unless the package is explicitly for cli
output.

## security posture

v1 must avoid arbitrary code execution.

forbidden unless a later task explicitly changes policy:

- postinstall scripts
- shell recipes
- build scripts
- curl-pipe-shell
- sudo
- modifying shell profile files
- automatically editing PATH
- installing launch agents
- installing kernel/system extensions

dpm may print a suggestion to add `~/.dpm/bin` to PATH, but must not mutate
shell config automatically.

## macos support

target macos first.

support metadata for:

```text
darwin/arm64
darwin/amd64
```

actual ci/support may start with apple silicon only.

use `runtime.GOOS` and `runtime.GOARCH`.

remember:

```text
go arch "arm64" = apple silicon
go arch "amd64" = intel
manifest arch may use "arm64" and "x86_64" only if normalized
```

centralize platform normalization.

## done criteria

a task is done only when:

- code is formatted
- relevant tests pass
- new behavior has tests
- no unsafe filesystem behavior was introduced
- cli behavior is documented if user-visible
- errors are useful
- no unrelated refactors were included

final response should include:

- files changed
- tests run
- behavior added/fixed
- known limitations
