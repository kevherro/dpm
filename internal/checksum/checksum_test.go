// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package checksum

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestNormalizeSHA256(t *testing.T) {
	got, err := NormalizeSHA256(strings.ToUpper(helloSHA256))
	if err != nil {
		t.Fatalf("NormalizeSHA256() error = %v", err)
	}
	if got != helloSHA256 {
		t.Fatalf("NormalizeSHA256() = %q, want %q", got, helloSHA256)
	}
}

func TestNormalizeSHA256RejectsInvalidDigest(t *testing.T) {
	tests := []string{
		"",
		"abc",
		strings.Repeat("g", 64),
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if _, err := NormalizeSHA256(tt); err == nil {
				t.Fatal("NormalizeSHA256() error = nil, want error")
			}
		})
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}
	if got != helloSHA256 {
		t.Fatalf("FileSHA256() = %q, want %q", got, helloSHA256)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := VerifyFileSHA256(path, helloSHA256); err != nil {
		t.Fatalf("VerifyFileSHA256() error = %v", err)
	}
}

func TestVerifyFileSHA256ReturnsMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := VerifyFileSHA256(path, strings.Repeat("0", 64))
	var mismatch MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("VerifyFileSHA256() error = %v, want MismatchError", err)
	}
	if mismatch.Actual != helloSHA256 {
		t.Fatalf("Actual = %q, want %q", mismatch.Actual, helloSHA256)
	}
}
