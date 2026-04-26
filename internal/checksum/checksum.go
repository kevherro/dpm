// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// MismatchError reports a checksum mismatch.
type MismatchError struct {
	Path     string
	Expected string
	Actual   string
}

func (e MismatchError) Error() string {
	return fmt.Sprintf("checksum mismatch for %s: expected %s, actual %s", e.Path, e.Expected, e.Actual)
}

// NormalizeSHA256 validates and canonicalizes a SHA-256 hex digest.
func NormalizeSHA256(sum string) (string, error) {
	sum = strings.ToLower(strings.TrimSpace(sum))
	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("sha256 must be hex: %w", err)
	}

	return sum, nil
}

// FileSHA256 returns the SHA-256 hex digest for path.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for checksum: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s for checksum: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFileSHA256 verifies path against expected.
func VerifyFileSHA256(path, expected string) error {
	expected, err := NormalizeSHA256(expected)
	if err != nil {
		return err
	}

	actual, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return MismatchError{
			Path:     path,
			Expected: expected,
			Actual:   actual,
		}
	}

	return nil
}
