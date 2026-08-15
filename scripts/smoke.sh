#!/usr/bin/env bash
# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

set -euo pipefail

if [[ $# -ne 1 ]]; then
	printf 'usage: %s <dpm-binary>\n' "$0" >&2
	exit 2
fi
binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
artifact_root="$tmp/artifact"
registry="$tmp/registry"
export DPM_ROOT="$tmp/root"
export DPM_REGISTRY_URL="file://$registry"

mkdir -p "$artifact_root/bin" "$registry/packages/hello/versions/1.0.0"
cat > "$artifact_root/bin/hello" <<'EOF'
#!/bin/sh
printf 'hello from dpm\n'
EOF
chmod +x "$artifact_root/bin/hello"
tar -C "$artifact_root" -czf "$tmp/hello.tar.gz" bin/hello
sha="$(shasum -a 256 "$tmp/hello.tar.gz" | awk '{print $1}')"
cat > "$registry/registry.toml" <<'EOF'
schema = 1
name = "dpm-smoke"
description = "Release smoke registry"
EOF
cat > "$registry/packages/hello/package.toml" <<'EOF'
schema = 1
name = "hello"
summary = "Release smoke package"
homepage = "https://example.invalid/hello"
license = "MIT"
EOF
cat > "$registry/packages/hello/versions/1.0.0/dpm.toml" <<EOF
schema = 1
name = "hello"
version = "1.0.0"
dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://$tmp/hello.tar.gz"
sha256 = "$sha"

[install]
bins = ["bin/hello"]
EOF
if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
	cat >> "$registry/packages/hello/versions/1.0.0/dpm.toml" <<EOF

[[artifacts]]
os = "$(go env GOOS)"
arch = "$(go env GOARCH)"
url = "file://$tmp/hello.tar.gz"
sha256 = "$sha"
EOF
fi
git -C "$registry" init -q
git -C "$registry" add .
git -C "$registry" -c user.name=dpm-smoke -c user.email=dpm@example.invalid commit -q -m smoke

"$binary" update
install_output="$("$binary" install hello)"
grep -Fx "installing hello 1.0.0" <<<"$install_output"
grep -Fx "done" <<<"$install_output"
test "$("$DPM_ROOT/bin/hello")" = "hello from dpm"
test "$("$binary" list)" = "hello 1.0.0"
test "$("$binary" install hello)" = $'hello 1.0.0 already installed\ndone'
test "$("$binary" remove hello)" = "removed hello 1.0.0"
test -z "$("$binary" list)"
