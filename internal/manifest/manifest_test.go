// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package manifest

import (
	"strings"
	"testing"
)

func TestParseValidManifest(t *testing.T) {
	got, err := Parse(strings.NewReader(`
schema = 1
name = "hello"
version = "1.0.0"
dependencies = ["libhello"]
yanked = true
yank_reason = "bad artifact"

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://./fixtures/hello-1.0.0-darwin-arm64.tar.gz"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[install]
bins = ["hello"]
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := Manifest{
		Schema:       CurrentSchema,
		Name:         "hello",
		Version:      "1.0.0",
		Dependencies: []string{"libhello"},
		Yanked:       true,
		YankReason:   "bad artifact",
		Artifacts: []Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				URL:    "file://./fixtures/hello-1.0.0-darwin-arm64.tar.gz",
				SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
		Install: Install{Bins: []string{"hello"}},
	}

	if !equalManifest(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseAllowsCommentsAndMultipleArtifacts(t *testing.T) {
	got, err := Parse(strings.NewReader(`
# package metadata
schema = 1
name = "hello"
version = "1.0.0"
dependencies = [] # no deps

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "https://example.com/hello#fragment"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[[artifacts]]
os = "darwin"
arch = "amd64"
url = "https://example.com/hello-amd64"
sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

[install]
bins = ["bin/hello"]
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(got.Artifacts) != 2 {
		t.Fatalf("len(Artifacts) = %d, want 2", len(got.Artifacts))
	}
	if got.Artifacts[0].URL != "https://example.com/hello#fragment" {
		t.Fatalf("URL = %q, want URL with fragment", got.Artifacts[0].URL)
	}
	if len(got.Dependencies) != 0 {
		t.Fatalf("Dependencies = %#v, want empty", got.Dependencies)
	}
}

func TestParseRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "missing schema",
			input: `
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["hello"]
`,
			wantErr: "manifest schema is required",
		},
		{
			name: "unsupported schema",
			input: `
schema = 2
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["hello"]
`,
			wantErr: "manifest schema 2 is not supported",
		},
		{
			name: "missing name",
			input: `
schema = 1
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["hello"]
`,
			wantErr: "manifest name is required",
		},
		{
			name: "missing artifact checksum",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
[install]
bins = ["hello"]
`,
			wantErr: "artifact 1 sha256 is required",
		},
		{
			name: "missing install bins",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
`,
			wantErr: "install bins must be declared",
		},
		{
			name: "unknown section",
			input: `
schema = 1
name = "hello"
[postinstall]
run = "echo no"
`,
			wantErr: "unknown section",
		},
		{
			name: "unknown key",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
script = "echo no"
`,
			wantErr: "unknown root key",
		},
		{
			name: "duplicate key",
			input: `
schema = 1
name = "hello"
name = "hello-again"
`,
			wantErr: "duplicate key",
		},
		{
			name: "absolute bin",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["/usr/bin/hello"]
`,
			wantErr: "must be relative",
		},
		{
			name: "traversal bin",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["../hello"]
`,
			wantErr: "must be relative",
		},
		{
			name: "invalid yanked bool",
			input: `
schema = 1
name = "hello"
version = "1.0.0"
dependencies = []
yanked = yes
[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "abc"
[install]
bins = ["hello"]
`,
			wantErr: "expected boolean true or false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "bare string",
			input:   `name = hello`,
			wantErr: "expected quoted string",
		},
		{
			name:    "non array dependencies",
			input:   `dependencies = "hello"`,
			wantErr: "expected string array",
		},
		{
			name:    "quoted schema",
			input:   `schema = "1"`,
			wantErr: "schema must be an integer",
		},
		{
			name:    "non string yank reason",
			input:   `yank_reason = false`,
			wantErr: "expected quoted string",
		},
		{
			name:    "unclosed string",
			input:   `name = "hello`,
			wantErr: "parse quoted string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func equalManifest(a, b Manifest) bool {
	if a.Schema != b.Schema ||
		a.Name != b.Name ||
		a.Version != b.Version ||
		a.Yanked != b.Yanked ||
		a.YankReason != b.YankReason {
		return false
	}
	if !equalStrings(a.Dependencies, b.Dependencies) {
		return false
	}
	if len(a.Artifacts) != len(b.Artifacts) {
		return false
	}
	for i := range a.Artifacts {
		if a.Artifacts[i] != b.Artifacts[i] {
			return false
		}
	}

	return equalStrings(a.Install.Bins, b.Install.Bins)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
