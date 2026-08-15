#!/usr/bin/env bash
# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

set -euo pipefail

source_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
repo="$tmp/dpm"
mkdir -p "$repo"
tar -C "$source_root" --exclude=.git --exclude=dist -cf - . | tar -C "$repo" -xf -
git -C "$repo" init -q
git -C "$repo" add .
git -C "$repo" -c user.name=dpm-test -c user.email=dpm@example.invalid commit -q -m release
version="$(tr -d '[:space:]' < "$repo/VERSION")"
git -C "$repo" tag -a "v$version" -m "v$version"
commit="$(git -C "$repo" rev-parse HEAD)"
date="$(git -C "$repo" show -s --format=%cI HEAD)"

DPM_TAG="v$version" DPM_OUT="$tmp/dpm-native" GOOS="$(go env GOOS)" GOARCH="$(go env GOARCH)" "$repo/scripts/build.sh" >/dev/null
"$tmp/dpm-native" version --verbose > "$tmp/version"
grep -Fx "version $version" "$tmp/version"
grep -Fx "commit $commit" "$tmp/version"
grep -Fx "date $date" "$tmp/version"

DPM_DIST="$tmp/dist" "$repo/scripts/release.sh" >/dev/null
archive="$tmp/dist/dpm_${version}_darwin_arm64.tar.gz"
test -f "$archive"
test -f "$tmp/dist/SHA256SUMS"
tar -tzf "$archive" | sort > "$tmp/contents"
cat > "$tmp/want" <<EOF
dpm_${version}_darwin_arm64/
dpm_${version}_darwin_arm64/INSTALL.md
dpm_${version}_darwin_arm64/LICENSE
dpm_${version}_darwin_arm64/dpm
EOF
diff -u "$tmp/want" "$tmp/contents"
(
	cd "$tmp/dist"
	shasum -a 256 -c SHA256SUMS
)

if DPM_TAG=v9.9.9 "$repo/scripts/build.sh" >/dev/null 2>"$tmp/mismatch"; then
	printf 'build unexpectedly accepted mismatched tag\n' >&2
	exit 1
fi
grep -q 'does not match VERSION' "$tmp/mismatch"
