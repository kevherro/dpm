#!/usr/bin/env bash
# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"
tag="${DPM_TAG:-$(git -C "$root" describe --tags --exact-match 2>/dev/null || true)}"

if [[ -z "$tag" ]]; then
	printf 'release build requires an exact v%s Git tag or DPM_TAG=v%s\n' "$version" "$version" >&2
	exit 1
fi
if [[ "$tag" != "v$version" ]]; then
	printf 'tag %s does not match VERSION %s\n' "$tag" "$version" >&2
	exit 1
fi
tag_commit="$(git -C "$root" rev-list -n 1 "$tag")"
if [[ "$(git -C "$root" rev-parse HEAD)" != "$tag_commit" ]]; then
	printf 'HEAD is not the tagged release commit %s\n' "$tag_commit" >&2
	exit 1
fi

dist="${DPM_DIST:-$root/dist}"
name="dpm_${version}_darwin_arm64"
stage="$dist/$name"
archive="$dist/$name.tar.gz"
rm -rf "$stage" "$archive" "$dist/SHA256SUMS"
mkdir -p "$stage"

DPM_TAG="$tag" DPM_OUT="$stage/dpm" GOOS=darwin GOARCH=arm64 "$root/scripts/build.sh" >/dev/null
cp "$root/LICENSE" "$root/INSTALL.md" "$stage/"
tar -C "$dist" -czf "$archive" "$name"
(
	cd "$dist"
	shasum -a 256 "$(basename "$archive")" > SHA256SUMS
)

printf '%s\n%s\n' "$archive" "$dist/SHA256SUMS"
