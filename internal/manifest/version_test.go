// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package manifest

import "testing"

func TestValidateVersion(t *testing.T) {
	for _, version := range []string{"0.0.0", "1.2.3", "10.200.3000"} {
		if err := ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q) error = %v", version, err)
		}
	}
	for _, version := range []string{"", "1", "1.2", "1.2.3.4", "v1.2.3", "1.2.3-beta", "01.2.3", "1.02.3", "1.2.03", "1.-2.3", "1.+2.3"} {
		if err := ValidateVersion(version); err == nil {
			t.Errorf("ValidateVersion(%q) error = nil, want error", version)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	versions := []string{"0.9.9", "1.0.0", "1.2.0", "1.10.0", "2.0.0", "10.0.0", "100000000000000000000.0.0"}
	for i := 0; i < len(versions)-1; i++ {
		if got := CompareVersions(versions[i], versions[i+1]); got >= 0 {
			t.Errorf("CompareVersions(%q, %q) = %d, want less than zero", versions[i], versions[i+1], got)
		}
	}
	if got := CompareVersions("1.2.3", "1.2.3"); got != 0 {
		t.Errorf("CompareVersions(equal) = %d, want zero", got)
	}
}
