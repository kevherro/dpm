// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package manifest

import (
	"fmt"
	"strings"
)

// ValidateVersion requires the canonical numeric MAJOR.MINOR.PATCH grammar.
func ValidateVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return fmt.Errorf("version %q must use canonical MAJOR.MINOR.PATCH", version)
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return fmt.Errorf("version %q must use canonical MAJOR.MINOR.PATCH", version)
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return fmt.Errorf("version %q must use canonical MAJOR.MINOR.PATCH", version)
			}
		}
	}

	return nil
}

// CompareVersions compares two validated canonical versions.
func CompareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := range 3 {
		if len(aParts[i]) != len(bParts[i]) {
			return len(aParts[i]) - len(bParts[i])
		}
		if aParts[i] != bParts[i] {
			return strings.Compare(aParts[i], bParts[i])
		}
	}

	return 0
}
