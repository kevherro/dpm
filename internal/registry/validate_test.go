// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevherro/dpm/internal/checksum"
)

func TestValidateAcceptsValidRegistry(t *testing.T) {
	root := t.TempDir()
	artifactURL, artifactSHA := writeValidationArtifact(t, root, "hello")
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:    "hello",
		version: "1.0.0",
		url:     artifactURL,
		sha256:  artifactSHA,
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !report.Valid() {
		t.Fatalf("Validate() issues = %#v, want valid", report.Issues)
	}
}

func TestValidateAcceptsMatchingGeneratedMetadata(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))
	if _, err := GenerateIndex(context.Background(), GenerateIndexOptions{Root: root}); err != nil {
		t.Fatalf("GenerateIndex() error = %v", err)
	}

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !report.Valid() {
		t.Fatalf("Validate() issues = %#v, want valid", report.Issues)
	}
}

func TestValidateReportsStaleGeneratedMetadata(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))
	if _, err := GenerateIndex(context.Background(), GenerateIndexOptions{Root: root}); err != nil {
		t.Fatalf("GenerateIndex() error = %v", err)
	}
	writeValidationPackage(t, root, "goodbye")
	writeValidationManifest(t, root, validManifestSpec(t, root, "goodbye", "2.0.0"))

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "generated package names do not match source packages")
}

func TestValidateReportsRegistryMetadataError(t *testing.T) {
	root := t.TempDir()
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "registry.toml")
}

func TestValidateReportsPackageAndVersionMismatches(t *testing.T) {
	root := t.TempDir()
	spec := validManifestSpec(t, root, "hello", "1.0.0")
	writeValidationMetadata(t, root)
	writePackageMetadata(t, root, "hello", validPackageMetadata("goodbye"))
	writeValidationManifest(t, root, spec)
	mismatch := strings.Replace(validationManifest(spec), `version = "1.0.0"`, `version = "2.0.0"`, 1)
	writeManifestContents(t, root, "hello", "1.0.0", mismatch)

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "does not match package directory")
	assertValidationIssue(t, report, "does not match registry version")
}

func TestValidateReportsRegistryWideRules(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:         "hello",
		version:      "1.0.0",
		dependencies: []string{"missing", "../bad"},
		yanked:       true,
		url:          "https://github.com/example/hello/releases/download/latest/hello.tar.gz",
		sha256:       strings.Repeat("a", 64),
		os:           "darwin",
		arch:         "amd64",
		bin:          "hello",
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, want := range []string{
		"yanked version must include yank_reason",
		"missing darwin/arm64 artifact",
		"latest downloads are not allowed",
		"invalid dependency \"../bad\"",
		"dependency \"missing\" is not in registry",
	} {
		assertValidationIssue(t, report, want)
	}
}

func TestValidateReportsBranchArchiveURL(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:    "hello",
		version: "1.0.0",
		url:     "https://github.com/example/hello/archive/refs/heads/main.tar.gz",
		sha256:  strings.Repeat("a", 64),
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "branch archives are not allowed")
}

func TestValidateReportsInvalidManifestFields(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	spec := validManifestSpec(t, root, "hello", "1.0.0")
	spec.sha256 = "not-hex"
	spec.bin = "../hello"
	writeValidationManifest(t, root, spec)

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "parse manifest")
}

func TestValidateReportsInvalidArtifactChecksum(t *testing.T) {
	root := t.TempDir()
	artifactURL, _ := writeValidationArtifact(t, root, "hello")
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:    "hello",
		version: "1.0.0",
		url:     artifactURL,
		sha256:  "not-hex",
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "invalid sha256")
}

func TestValidateVerifiesArtifacts(t *testing.T) {
	root := t.TempDir()
	artifactURL, artifactSHA := writeValidationArtifact(t, root, "hello")
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:    "hello",
		version: "1.0.0",
		url:     artifactURL,
		sha256:  artifactSHA,
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root, VerifyArtifacts: true})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !report.Valid() {
		t.Fatalf("Validate() issues = %#v, want valid", report.Issues)
	}
}

func TestValidateReportsArtifactChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	artifactURL, _ := writeValidationArtifact(t, root, "hello")
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, manifestSpec{
		name:    "hello",
		version: "1.0.0",
		url:     artifactURL,
		sha256:  strings.Repeat("a", 64),
	})

	report, err := Validate(context.Background(), ValidateOptions{Root: root, VerifyArtifacts: true})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	assertValidationIssue(t, report, "checksum mismatch")
}

type manifestSpec struct {
	name         string
	version      string
	dependencies []string
	yanked       bool
	yankReason   string
	url          string
	sha256       string
	os           string
	arch         string
	bin          string
}

func validManifestSpec(t *testing.T, root, name, version string) manifestSpec {
	t.Helper()
	artifactURL, artifactSHA := writeValidationArtifact(t, root, name)
	return manifestSpec{
		name:    name,
		version: version,
		url:     artifactURL,
		sha256:  artifactSHA,
	}
}

func writeValidationMetadata(t *testing.T, root string) {
	t.Helper()
	writeValidationFile(t, filepath.Join(root, MetadataFile), `schema = 1
name = "dpm-core"
description = "Test registry"
`)
}

func writeValidationPackage(t *testing.T, root, name string) {
	t.Helper()
	writePackageMetadata(t, root, name, validPackageMetadata(name))
}

func writeValidationManifest(t *testing.T, root string, spec manifestSpec) {
	t.Helper()
	writeManifestContents(t, root, spec.name, spec.version, validationManifest(spec))
}

func validationManifest(spec manifestSpec) string {
	deps := "[]"
	if len(spec.dependencies) > 0 {
		quoted := make([]string, 0, len(spec.dependencies))
		for _, dep := range spec.dependencies {
			quoted = append(quoted, `"`+dep+`"`)
		}
		deps = "[" + strings.Join(quoted, ", ") + "]"
	}
	osName := spec.os
	if osName == "" {
		osName = "darwin"
	}
	arch := spec.arch
	if arch == "" {
		arch = "arm64"
	}
	bin := spec.bin
	if bin == "" {
		bin = spec.name
	}
	yankFields := ""
	if spec.yanked {
		yankFields += "yanked = true\n"
	}
	if spec.yankReason != "" {
		yankFields += `yank_reason = "` + spec.yankReason + `"` + "\n"
	}

	return `schema = 1
name = "` + spec.name + `"
version = "` + spec.version + `"
dependencies = ` + deps + `
` + yankFields + `
[[artifacts]]
os = "` + osName + `"
arch = "` + arch + `"
url = "` + spec.url + `"
sha256 = "` + spec.sha256 + `"

[install]
bins = ["` + bin + `"]
`
}

func writeValidationArtifact(t *testing.T, root, name string) (string, string) {
	t.Helper()
	path := filepath.Join(root, "artifacts", name+".txt")
	writeValidationFile(t, path, "artifact for "+name+"\n")
	sum, err := checksum.FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}

	return "file://" + filepath.ToSlash(path), sum
}

func writeValidationFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertValidationIssue(t *testing.T, report ValidationReport, want string) {
	t.Helper()
	for _, issue := range report.Issues {
		if strings.Contains(issue.Message, want) || strings.Contains(issue.Path, want) {
			return
		}
	}
	t.Fatalf("Validate() issues = %#v, want substring %q", report.Issues, want)
}
