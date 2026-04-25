# Copyright (c) 2026 Kevin Herro
# SPDX-License-Identifier: MIT

#!/usr/bin/env bash

set -e
set -x

# Packages that have any tests.
PKG=$(go list -f '{{if .TestGoFiles}} {{.ImportPath}} {{end}}' ./...)

go test $PKG

go vet -all ./...

gofmt -d -s .
