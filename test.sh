#!/usr/bin/env bash
# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

set -euo pipefail
set -x

test -z "$(gofmt -d -s .)"
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
./scripts/release_test.sh
