// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	got, err := ParseMetadata(strings.NewReader(`
# root registry metadata
schema = 1
name = "dpm-core"
description = "Core # registry"
`))
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	want := Metadata{
		Schema:      CurrentSchema,
		Name:        "dpm-core",
		Description: "Core # registry",
	}
	if got != want {
		t.Fatalf("ParseMetadata() = %#v, want %#v", got, want)
	}
}

func TestParseMetadataAllowsOptionalDescription(t *testing.T) {
	got, err := ParseMetadata(strings.NewReader(`
schema = 1
name = "dpm-core"
`))
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if got.Description != "" {
		t.Fatalf("Description = %q, want empty", got.Description)
	}
}

func TestRegistryMetadataLoadsRootFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, MetadataFile), []byte(`
schema = 1
name = "dpm-core"
description = "Core dpm package registry"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	reg := newRegistry(t, root)

	got, err := reg.Metadata()
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	if got.Name != "dpm-core" {
		t.Fatalf("Name = %q, want dpm-core", got.Name)
	}
}

func TestParseMetadataRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "missing schema",
			input: `
name = "dpm-core"
`,
			wantErr: "schema is required",
		},
		{
			name: "unsupported schema",
			input: `
schema = 2
name = "dpm-core"
`,
			wantErr: "not supported",
		},
		{
			name: "missing name",
			input: `
schema = 1
`,
			wantErr: "name is required",
		},
		{
			name: "path name",
			input: `
schema = 1
name = "org/dpm-core"
`,
			wantErr: "must not contain path separators",
		},
		{
			name: "unknown key",
			input: `
schema = 1
name = "dpm-core"
url = "https://example.com"
`,
			wantErr: "unknown registry metadata key",
		},
		{
			name: "duplicate key",
			input: `
schema = 1
schema = 1
name = "dpm-core"
`,
			wantErr: "duplicate key",
		},
		{
			name: "section",
			input: `
schema = 1
name = "dpm-core"
[packages]
`,
			wantErr: "sections are not allowed",
		},
		{
			name: "quoted schema",
			input: `
schema = "1"
name = "dpm-core"
`,
			wantErr: "schema must be an integer",
		},
		{
			name: "bare name",
			input: `
schema = 1
name = dpm-core
`,
			wantErr: "expected quoted string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMetadata(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("ParseMetadata() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseMetadata() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}
