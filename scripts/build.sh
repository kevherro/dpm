#!/usr/bin/env bash
# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

version="${DPM_VERSION:-$(tr -d '[:space:]' < "$root/VERSION")}"
tag="${DPM_TAG:-}"
if [[ -n "$tag" ]]; then
	if [[ "$tag" != "v$version" ]]; then
		printf 'tag %s does not match VERSION %s\n' "$tag" "$version" >&2
		exit 1
	fi
	commit="${DPM_COMMIT:-$(git -C "$root" rev-list -n 1 "$tag")}"
	date="${DPM_DATE:-$(git -C "$root" show -s --format=%cI "$commit")}"
else
	commit="${DPM_COMMIT:-$(git -C "$root" rev-parse HEAD 2>/dev/null || printf unknown)}"
	date="${DPM_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
fi
out="${DPM_OUT:-$root/dist/dpm}"

mkdir -p "$(dirname "$out")"

ldflags="-s -w"
ldflags="$ldflags -X github.com/kevherro/dpm/internal/version.Version=$version"
ldflags="$ldflags -X github.com/kevherro/dpm/internal/version.Commit=$commit"
ldflags="$ldflags -X github.com/kevherro/dpm/internal/version.Date=$date"

(
	cd "$root"
	GOOS="${GOOS:-darwin}" GOARCH="${GOARCH:-arm64}" go build -trimpath -ldflags "$ldflags" -o "$out" ./cmd/dpm
)

printf '%s\n' "$out"
