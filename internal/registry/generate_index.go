// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// GenerateIndexOptions configures static index generation.
type GenerateIndexOptions struct {
	Root            string
	SigningKey      string
	SnapshotVersion int
}

// GeneratedIndexFile describes one generated metadata file.
type GeneratedIndexFile struct {
	Path   string
	SHA256 string
}

// GenerateIndexResult reports the generated static index.
type GenerateIndexResult struct {
	Root            string
	IndexDir        string
	Signed          bool
	SnapshotVersion int
	Files           []GeneratedIndexFile
}

// GenerateIndex builds generated static metadata under index/.
func GenerateIndex(ctx context.Context, opts GenerateIndexOptions) (GenerateIndexResult, error) {
	reg, err := New(opts.Root)
	if err != nil {
		return GenerateIndexResult{}, err
	}
	if err := validateSourceRegistry(ctx, reg.Root); err != nil {
		return GenerateIndexResult{}, err
	}
	metadata, err := reg.Metadata()
	if err != nil {
		return GenerateIndexResult{}, err
	}
	names, err := sourcePackageNames(reg.Root)
	if err != nil {
		return GenerateIndexResult{}, err
	}

	tmp, err := os.MkdirTemp(reg.Root, ".index-*")
	if err != nil {
		return GenerateIndexResult{}, fmt.Errorf("create temporary static index in %s: %w", reg.Root, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.RemoveAll(tmp)
		}
	}()

	var packageEntries []staticPackage
	for _, name := range names {
		pkg, err := reg.Package(name)
		if err != nil {
			return GenerateIndexResult{}, err
		}
		versionsEntry, err := writeStaticVersionsIndex(tmp, reg, pkg)
		if err != nil {
			return GenerateIndexResult{}, err
		}

		entry := staticPackageFromPackage(pkg)
		entry.VersionsPath = versionsEntry.relPath
		entry.VersionsSHA256 = versionsEntry.sha256
		packageEntries = append(packageEntries, entry)
	}
	slices.SortFunc(packageEntries, func(a, b staticPackage) int {
		return strings.Compare(a.Name, b.Name)
	})

	packagesIndex := staticPackagesIndex{
		Schema: CurrentSchema,
		Registry: staticMetadata{
			Schema:      metadata.Schema,
			Name:        metadata.Name,
			Description: metadata.Description,
		},
		Packages: packageEntries,
	}
	if _, _, err := writeStaticJSON(tmp, "packages.json", packagesIndex); err != nil {
		return GenerateIndexResult{}, err
	}
	if opts.SigningKey != "" {
		files, err := generatedFiles(tmp)
		if err != nil {
			return GenerateIndexResult{}, err
		}
		if err := writeSignedSnapshot(tmp, metadata.Name, opts.SnapshotVersion, opts.SigningKey, files); err != nil {
			return GenerateIndexResult{}, err
		}
	} else if opts.SnapshotVersion != 0 {
		return GenerateIndexResult{}, fmt.Errorf("snapshot version requires a signing key")
	}

	indexDir := filepath.Join(reg.Root, StaticIndexDir)
	if err := replaceIndexDir(tmp, indexDir); err != nil {
		return GenerateIndexResult{}, err
	}
	removeTmp = false

	generated, err := generatedFiles(indexDir)
	if err != nil {
		return GenerateIndexResult{}, err
	}

	return GenerateIndexResult{
		Root:            reg.Root,
		IndexDir:        indexDir,
		Signed:          opts.SigningKey != "",
		SnapshotVersion: opts.SnapshotVersion,
		Files:           generated,
	}, nil
}

type generatedPath struct {
	relPath string
	sha256  string
}

func writeStaticVersionsIndex(root string, reg Registry, pkg Package) (generatedPath, error) {
	versions, err := reg.Versions(pkg.Name)
	if err != nil {
		return generatedPath{}, err
	}
	var versionEntries []staticVersion
	for _, version := range versions {
		m, err := reg.ResolveVersion(pkg.Name, version)
		if err != nil {
			return generatedPath{}, err
		}
		rel := path.Join("packages", pkg.Name, "versions", version, "dpm.json")
		_, manifestSHA, err := writeStaticJSON(root, rel, staticManifestFromManifest(m))
		if err != nil {
			return generatedPath{}, err
		}
		versionEntries = append(versionEntries, staticVersion{
			Version:        m.Version,
			Yanked:         m.Yanked,
			YankReason:     m.YankReason,
			ManifestPath:   rel,
			ManifestSHA256: manifestSHA,
		})
	}
	sortVersionsInEntries(versionEntries)

	rel := path.Join("packages", pkg.Name, "versions.json")
	_, indexSHA, err := writeStaticJSON(root, rel, staticVersionsIndex{
		Schema:   CurrentSchema,
		Package:  staticPackageMetadataFromPackage(pkg),
		Versions: versionEntries,
	})
	if err != nil {
		return generatedPath{}, err
	}

	return generatedPath{relPath: rel, sha256: indexSHA}, nil
}

func sortVersionsInEntries(entries []staticVersion) {
	slices.SortFunc(entries, func(a, b staticVersion) int {
		return compareVersions(a.Version, b.Version)
	})
}

func writeStaticJSON(root, rel string, value any) (string, string, error) {
	if err := validateStaticPath(rel); err != nil {
		return "", "", err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal static json %s: %w", rel, err)
	}
	data = append(data, '\n')
	sum := sha256Hex(data)
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", "", fmt.Errorf("create static index parent %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write static index file %s: %w", target, err)
	}
	if err := os.WriteFile(target+".sha256", []byte(sum+"\n"), 0o644); err != nil {
		return "", "", fmt.Errorf("write static index checksum %s.sha256: %w", target, err)
	}

	return target, sum, nil
}

func validateSourceRegistry(ctx context.Context, root string) error {
	report, err := Validate(ctx, ValidateOptions{Root: root})
	if err != nil {
		return err
	}
	if report.Valid() {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "registry is invalid")
	for _, issue := range report.Issues {
		fmt.Fprintf(&b, "\n%s: %s", issue.Path, issue.Message)
	}

	return fmt.Errorf("%s", b.String())
}

func sourcePackageNames(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "packages"))
	if err != nil {
		return nil, fmt.Errorf("read registry packages: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := validatePathPart("package", name); err != nil {
			return nil, fmt.Errorf("invalid registry package directory %q: %w", name, err)
		}
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

func replaceIndexDir(tmp, indexDir string) error {
	root := filepath.Dir(indexDir)
	if err := requireInside(root, indexDir); err != nil {
		return err
	}
	if err := os.RemoveAll(indexDir); err != nil {
		return fmt.Errorf("remove old static index %s: %w", indexDir, err)
	}
	if err := os.Rename(tmp, indexDir); err != nil {
		return fmt.Errorf("install static index %s: %w", indexDir, err)
	}

	return nil
}

func generatedFiles(indexDir string) ([]GeneratedIndexFile, error) {
	var files []GeneratedIndexFile
	if err := filepath.WalkDir(indexDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".sha256") {
			return nil
		}
		sum, err := readStaticSidecar(path)
		if err != nil {
			return err
		}
		files = append(files, GeneratedIndexFile{
			Path:   path,
			SHA256: sum,
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list generated static index %s: %w", indexDir, err)
	}
	slices.SortFunc(files, func(a, b GeneratedIndexFile) int {
		return strings.Compare(a.Path, b.Path)
	})

	return files, nil
}

func requireInside(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("compare %s to root %s: %w", target, root, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}

	return fmt.Errorf("path %s escapes root %s", target, root)
}
