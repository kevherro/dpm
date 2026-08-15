# dpm v1 plan

Status: proposed release plan, written against `v0.1.0` on 2026-07-11.

## Outcome

`v1.0.0` is ready when a new user on a supported Mac with the documented Git
prerequisite can install a published `dpm` binary without `sudo` and complete
this flow using the default public configuration:

```sh
dpm update
dpm install ripgrep
~/.dpm/bin/rg --version
dpm list
dpm install ripgrep
dpm remove ripgrep
```

The second install must report a healthy existing installation. The hermetic
`hello` end-to-end test remains the offline proof of the same lifecycle; the
`ripgrep` flow is the release smoke test.

This is a deliberately narrow v1. It proves a safe, deterministic,
supportable binary-install model. It does not try to match Homebrew's catalog,
version-management, or build features.

## Current baseline

The core is already implemented:

- [x] Strict declarative manifests with explicit artifacts, SHA-256 digests,
      dependencies, and bins.
- [x] Git-backed registry update, resolution, search, info, and yanking.
- [x] Checksum-addressed downloads with cache re-verification.
- [x] Traversal-resistant `.tar.gz` extraction that rejects links and special
      entries.
- [x] Isolated prefixes, conflict-safe bin links, JSON ownership state, list,
      and remove.
- [x] A state-based same-version short-circuit.
- [x] Registry validation/preparation tooling, generated indexes, and optional
      signed-snapshot primitives.
- [x] Hermetic CLI and package tests.

`REGISTRY_PLAN.md` records the completed registry experiments. This plan covers
the remaining client-safety, lifecycle, registry-production, onboarding, and
release work.

## V1 contract

V1 supports:

- macOS 14 or later on Apple silicon (`darwin/arm64`). Intel metadata continues
  to parse, but Intel runtime support and release artifacts are post-v1.
- One official Git registry, fetched explicitly with `dpm update` over anonymous
  HTTPS. Apple Git from the Xcode Command Line Tools is a v1 prerequisite.
- Git checkout plus pinned artifact SHA-256 values as the v1 trust model, as
  specified in `REGISTRY_PLAN.md`.
- Prebuilt `.tar.gz` artifacts over HTTPS. `file://` remains for hermetic tests
  and explicit local-development registries.
- The existing user commands: `update`, `search`, `info`, `install`, `list`,
  `remove`, `doctor`, `version`, and `help`.
- Named dependencies. One install resolves the complete dependency graph from
  one immutable registry revision before writing anything, installs in
  dependency order, and refuses to remove an installed dependency still in use.
- One installed version per package. Installing a different version remains a
  safe refusal.

V1 explicitly defers:

- `name@version`, upgrade/downgrade, lockfiles, constraints, and a solver.
- Multiple simultaneous registries or taps.
- Source builds, scripts/hooks, implicit bin discovery, and extra archive
  formats.
- Automatic PATH edits, registry updates, dependency garbage collection,
  doctor repair, and self-update.
- An `--offline` flag, artifact mirroring, static HTTP distribution, mandatory
  signed snapshots, full TUF, transparency logs, OCI, and IPFS.
- Intel support, a large package catalog, benchmarks, mandatory fuzzing,
  bit-for-bit reproducible builds, and build-provenance infrastructure.

The existing static-index and signed-snapshot code may remain available behind
development configuration, but it is not part of the v1 compatibility or trust
promise. If configured, a failed verification must still leave the previous
registry checkout active.

## V1 invariants

1. Client lifecycle commands (`update`, `install`, `remove`, cache/link/state
   writes) never mutate outside the canonical configured dpm root.
2. Registry maintainer commands mutate only beneath the explicit registry root
   passed to them.
3. Pre-existing symlinks in managed path components cannot redirect a write or
   deletion outside the applicable root.
4. Dpm never overwrites or removes a bin entry or package prefix it cannot prove
   it owns.
5. Every installed artifact matches the manifest SHA-256; cache hits are
   verified again before use.
6. One install reads one immutable registry revision, so dependencies cannot be
   resolved from a mixed update.
7. For an initially valid installed graph, enumerated ordinary error paths
   restore the prior owned prefixes, links, and state. If interruption or a
   second I/O failure prevents rollback, dpm must retain detectable recovery
   evidence and refuse to report success.
8. Writers are serialized, and state/registry readers cannot observe a
   half-committed mutation.
9. Dpm never executes package- or registry-provided installation logic and never
   edits shell configuration.

The threat model covers hostile archive structure, corrupt state, symlinked
layout components, ordinary I/O failures, interrupted operations, conflicting
files, and concurrent dpm processes. It does not claim protection from a
malicious process already running as the same user, nor does a checksum prove
that an installed binary is benign when the user later runs it.

## Critical path

```text
M0 contract and red tests
  -> M1 filesystem ownership boundary
  -> M2 lifecycle and concurrency
  -> M3 production registry update
  -> M4 diagnostics and onboarding
  -> M5 CI, release candidate, v1.0.0
```

Registry CI and release packaging can begin in parallel after M0, but their
final smoke gates depend on M1-M4.

## M0: Freeze decisions and add failing tests

Goal: make the v1 promise testable before changing implementation.

Work:

- [x] Add a concise threat model containing the scope and exclusions above.
- [x] Freeze manifest and registry schema 1 semantics.
- [x] Restrict package versions to canonical numeric `MAJOR.MINOR.PATCH` for v1
      and replace the current loose comparison behavior with tests for that
      grammar and ordering.
- [x] Add `schema` to installed state and choose a tested `v0.1.0` migration or
      explicit incompatibility path before the on-disk format freezes.
- [x] Define the root policy exactly: the default is canonical `~/.dpm`; a
      `DPM_ROOT` override must be a strict descendant of the canonical user home
      or canonical `os.TempDir()`; the root itself and managed children may not
      be symlinks. Client mutations invoked with effective UID 0 are refused.
- [x] Document exit codes: `0` success, `1` operational failure, `2` usage
      failure.
- [x] Add failing outside-sentinel tests for symlinked `bin`, `pkgs`,
      `downloads`, `cache`, `registry`, and `state` directories.
- [x] Add failing crafted-state tests that claim the whole `pkgs` directory,
      another package prefix, another package bin, and an outside path.
- [x] Add failing concurrent reader/writer and lifecycle rollback tests before
      implementing the fixes.

Exit criteria:

- The threat model, support matrix, root rule, state compatibility, version
  ordering, and deferred features contain no open design alternatives.
- Each known release blocker has a focused test that fails for the intended
  reason on the current implementation.

## M1: Make filesystem ownership exact

Goal: close the current gap between lexical path checks and real filesystem
containment.

Work:

- [x] Canonicalize the root through its longest existing ancestor and validate
      it against the M0 root rule.
- [x] Inspect every existing managed component with `Lstat`; reject symlinks and
      non-directories before mutation.
- [x] Centralize a small safe-path API for canonical root validation, safe join,
      real-parent containment, and exact owned-path checks.
- [x] Require a state prefix to equal `pkgs/<name>/<version>`, each bin source to
      be inside that exact prefix, and each link to equal `bin/<bin-name>`.
- [x] Derive deletion targets from validated names and versions. Never grant
      deletion authority merely because a JSON path is lexically inside the
      root.
- [x] Reject trailing state JSON and validate stored checksums, duplicate bins,
      and duplicate dependencies.
- [x] Preserve archive rejection of traversal, absolute/backslash paths,
      symlinks, hard links, special entries, and duplicate regular-file outputs.
- [x] Apply the same containment model to maintainer commands using their
      explicit registry output root rather than `DPM_ROOT`.
- [x] Surface cleanup failures instead of silently discarding them.

Acceptance:

- Every managed-directory symlink case fails before mutation and leaves its
  outside sentinel byte-for-byte unchanged.
- `../evil`, `../../.ssh/id_rsa`, `/absolute/path`, `bin/../../evil`, malicious
  state, another package's state, and a symlinked package-name directory cannot
  cause an outside or unowned mutation.
- Existing archive, link-conflict, checksum, state, and end-to-end tests remain
  green.

## M2: Make lifecycle and concurrency predictable

Goal: make the already-supported dependency/install/remove behavior safe under
ordinary failure and concurrent commands without adding upgrade semantics.

Work:

- [x] Add a root-wide shared/exclusive operation lock. `install`, `remove`, and
      `update` take the exclusive lock; `list`, `search`, `info`, and `doctor`
      take the appropriate shared state/registry lock. `help` and `version` do
      not need the root.
- [x] Introduce an immutable registry view identified by one Git revision and
      resolve the complete, de-duplicated dependency graph through it.
- [x] Preflight cycles, missing dependencies, platform artifacts, installed
      version conflicts, duplicate bin names, and existing bin ownership before
      committing a prefix, link, or state record.
- [x] Fetch, verify, extract, and validate every new package in staging before
      committing the graph.
- [x] On an ordinary returned error, roll back only prefixes, links, and state
      created by that operation; preserve all pre-existing packages and verified
      content-addressed cache entries.
- [x] Preflight every remove target and link before changing anything. Refuse
      removal when installed state names a dependent; list the blockers.
- [x] Before reporting “already installed,” verify the exact prefix, declared
      executables, owned links, and dependency records. Drift returns a precise
      integrity error and points to `dpm doctor`.
- [x] Detect stale staging, prefix-without-state, link-without-state, and other
      interrupted-operation evidence. V1 may refuse with safe manual recovery
      guidance; automatic crash recovery and a general repair command are
      deferred.

Acceptance:

- A dependency chain, diamond, cycle, missing transitive dependency, shared
  dependency, duplicate graph bin, and parent link conflict have deterministic
  results. At recoverable failpoints, a returned error leaves no newly owned
  partial install.
- Removing a required dependency changes nothing and names each dependent.
- Same-version reinstall succeeds only for a healthy installation; missing or
  retargeted paths never produce false success.
- Competing install/remove/update processes cannot interleave, and readers see
  either the before-state or after-state covered by the lock.
- Injected cleanup failure returns both the primary and cleanup/recovery context
  and leaves detectable ownership evidence.

## M3: Productionize the Git registry

Goal: make the v1 Git-and-SHA trust model usable from a clean account and ensure
a failed update cannot replace the last usable checkout.

Client work:

- [x] Change the default URL to the anonymous HTTPS Git URL
      `https://github.com/kevherro/dpm-registry.git`.
- [x] Preflight Git before creating or changing the registry directory. Missing
      Git gets an actionable error explaining the tested Apple Command Line
      Tools prerequisite.
- [x] Clone or fetch into a candidate directory beneath `DPM_ROOT` on the same
      filesystem. Validate the checkout root, registry/package/version schemas,
      dependency resolution, platform coverage, and generated metadata when
      present before activation.
- [x] Activate with a recoverable old/candidate rename protocol while holding
      the exclusive registry lock. On any returned error, restore or retain the
      previous checkout as active; interrupted swaps are detected before the
      next registry read.
- [x] Bind `install`, `search`, and `info` to one immutable active revision for
      the duration of the command.
- [x] Move update/activation policy out of `cmd/dpm`; keep CLI code responsible
      for parsing, presentation, and exit status.
- [x] If optional signed-snapshot verification is configured, perform it on the
      candidate before activation. Do not make embedded keys or signatures a v1
      prerequisite.

Official registry work:

- [ ] Add required CI for structural validation and verification of every new or
      changed artifact hash.
- [ ] Enforce published-version history: no deletion or rewrite; yanking with a
      reason is the supported withdrawal path.
- [ ] Keep `ripgrep` as the required v1 real-package canary. Its immutable
      `darwin/arm64` artifact, checksum, explicit bin, license metadata, and
      `rg --version` smoke must pass before merge.
- [ ] Run a scheduled clean `update -> install -> execute -> list -> reinstall ->
      remove` canary against the public default.

Acceptance:

- A clean account needs no `DPM_*` overrides, SSH key, or author-local path.
- Invalid schema, missing dependency, failed optional signature, interrupted
  clone/fetch, and activation failure all leave the previous checkout usable.
- `install`, `search`, and `info` cannot mix two registry revisions during an
  update.
- The public ripgrep journey passes on a supported clean Mac.

## M4: Make diagnosis and onboarding complete

Goal: make each enumerated v1 safe refusal understandable without automatic
repair.

Work:

- [x] Expand `doctor` into a read-only audit of canonical layout types, state
      schema/records, exact prefixes, executable sources, owned links,
      dependency edges, stale staging/orphans, operation-lock health, and active
      registry revision/validity.
- [x] Emit package/version-aware checksum errors with expected and actual
      digests.
- [ ] Add actionable errors for bin conflicts, dependent removal, state drift,
      unsupported platform/version syntax, lock contention, missing Git,
      rejected registry update, and unsupported state schema.
- [ ] Test command exit codes and multiline security/recovery errors where
      formatting matters.
- [x] Replace development-only onboarding with public instructions for obtaining
      and verifying dpm, copying it to a user-owned path such as
      `~/.local/bin`, installing Git if necessary, running `dpm update`, adding
      `~/.dpm/bin` to PATH manually, and completing the ripgrep journey.
- [x] Document supported scope, network/cache behavior, state location,
      uninstall/cache cleanup, safe recovery, the v1 trust model, and known
      limitations. Add `SECURITY.md` with a private reporting route.

Acceptance:

- A corruption matrix produces specific doctor findings without modifying the
  root.
- Each enumerated M1-M3 refusal has a CLI test and documented next action.
- A user following only public docs can complete the target journey without
  `sudo`, a local registry path, or maintainer help.

## M5: CI, release candidate, and v1.0.0

Goal: prove the exact published artifact on the exact supported platform.

Required client CI on macOS 14+ Apple silicon:

- [ ] `gofmt -d -s .` is empty.
- [ ] `go test -count=1 ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `go test -race -count=1 ./...` passes.
- [ ] The hermetic hello lifecycle, outside-root sentinel suite, dependency
      rollback suite, concurrency suite, and registry activation suite pass.
- [x] The release target builds and its `version --verbose` metadata is tested.

Release work:

- [x] Make tag and `VERSION` agreement mandatory and build with `-trimpath` plus
      commit/date metadata derived from the tag commit.
- [ ] Publish `dpm_1.0.0_darwin_arm64.tar.gz` containing `dpm`, `LICENSE`, and
      short install instructions, plus `SHA256SUMS`.
- [ ] Sign and notarize the macOS binary. Verify the exact extracted binary with
      `codesign --verify --strict` and `spctl --assess --type execute` before
      publishing. If release credentials are unavailable, publish RCs only; do
      not call an unsigned quarantined download v1 GA.
- [ ] Test the exact packaged binary—not a second local build—through the
      hermetic hello flow and the public ripgrep flow from a fresh root.
- [ ] Add a protected annotated-tag workflow that refuses version mismatch,
      failed tests, failed signing/notarization, checksum mismatch, or missing
      assets.

RC and final audit:

- [ ] Publish `v1.0.0-rc.1` through the real workflow.
- [ ] Exercise clean first run, a missing-Git failure before root mutation,
      `v0.1.0` state migration, checksum mismatch, bin conflict, dependency
      removal refusal, symlinked layout, state drift, concurrent commands, and a
      rejected registry update using the packaged RC binary.
- [ ] Close all release-blocking safety, corruption, clean-install, and
      distribution defects, then rerun every gate on the final commit.
- [ ] Set `VERSION` to `1.0.0`, create annotated tag `v1.0.0`, publish release
      notes with support/deferred/compatibility details, and repeat the public
      download plus ripgrep smoke after publication.

## Suggested PR sequence

1. Contract/threat model, state/version rules, and failing safety tests.
2. Canonical root, symlink-safe layout, exact ownership, and crafted-state
   rejection.
3. Shared/exclusive root lock and immutable registry view.
4. Dependency graph preflight/staging/rollback, dependent removal guard, and
   integrity-aware reinstall.
5. HTTPS default and staged, validated, recoverable registry activation.
6. Expanded read-only doctor, actionable errors, and public onboarding docs.
7. Client CI and exact release-archive smoke tests.
8. Official registry CI, immutability policy, and public ripgrep canary.
9. Release packaging, signing/notarization, RC audit, and `v1.0.0`.

PRs 2 and 7 can start after PR 1. Registry CI can proceed in parallel, but its
release smoke waits for PR 5. Keep each PR focused and run the narrow package
tests before the full gate.

## Final release gate

Do not tag `v1.0.0` unless all answers are yes:

- [ ] Does a clean macOS 14+ Apple-silicon account with the documented Git
      prerequisite install and verify the exact published dpm archive without
      `sudo`?
- [ ] Does default `dpm update` use public anonymous HTTPS and leave the previous
      registry usable after every enumerated rejection/failure test?
- [ ] Does the packaged binary complete the hello and public ripgrep lifecycle?
- [ ] Do traversal, symlink, crafted-state, ownership, conflict, and
      outside-sentinel tests prove the filesystem boundary for client and
      maintainer roots?
- [ ] Do dependency, rollback, interruption-detection, and reader/writer tests
      prove lifecycle consistency for the enumerated cases?
- [ ] Does `doctor` report every corruption class in the documented matrix
      without mutating the root?
- [ ] Are state migration, schema/version rules, exit codes, recovery guidance,
      support scope, and deferred features tested or documented as applicable?
- [ ] Did `go fmt ./...` run with no resulting diff, followed by uncached full
      tests, vet, race, the release workflow, code-signature assessment,
      checksum verification, and clean-machine smoke on the final commit?
- [ ] Are all release-blocking defects closed and the exact release evidence
      retained with the release notes/checklist?
