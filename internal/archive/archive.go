// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarGz extracts a .tar.gz archive into dst.
func ExtractTarGz(archivePath, dst string) error {
	if archivePath == "" {
		return fmt.Errorf("archive path is empty")
	}
	if dst == "" {
		return fmt.Errorf("extract destination is empty")
	}

	dst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve extract destination %q: %w", dst, err)
	}
	dst = filepath.Clean(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create extract destination %s: %w", dst, err)
	}
	dstReal, err := filepath.EvalSymlinks(dst)
	if err != nil {
		return fmt.Errorf("resolve extract destination %s: %w", dst, err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip archive %s: %w", archivePath, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header from %s: %w", archivePath, err)
		}
		if err := extractEntry(tr, dst, dstReal, header); err != nil {
			return err
		}
	}

	return nil
}

func extractEntry(r io.Reader, dst, dstReal string, header *tar.Header) error {
	name, err := safeEntryName(header.Name)
	if err != nil {
		return err
	}
	target := filepath.Join(dst, name)
	if err := requireInside(dst, target); err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, modePerm(header, 0o755)); err != nil {
			return fmt.Errorf("create archive directory %s: %w", target, err)
		}
		return requireRealParentInside(dstReal, target)
	case tar.TypeReg, tar.TypeRegA:
		parent := filepath.Dir(target)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create archive file parent %s: %w", parent, err)
		}
		if err := requireRealParentInside(dstReal, target); err != nil {
			return err
		}
		return writeFile(target, r, modePerm(header, 0o755))
	case tar.TypeSymlink, tar.TypeLink:
		return fmt.Errorf("archive entry %q uses links, which are not allowed", header.Name)
	default:
		return fmt.Errorf("archive entry %q has unsupported type %q", header.Name, header.Typeflag)
	}
}

func writeFile(path string, r io.Reader, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("create archive file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write archive file %s: %w", path, err)
	}

	return nil
}

func safeEntryName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("archive entry has empty path")
	}
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("archive entry %q contains backslash path separators", name)
	}
	clean := filepath.Clean(name)
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("archive entry %q escapes extraction root", name)
	}

	return clean, nil
}

func requireInside(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("compare archive path %s to extraction root %s: %w", path, root, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}

	return fmt.Errorf("archive path %s escapes extraction root %s", path, root)
}

func requireRealParentInside(rootReal, path string) error {
	parent := filepath.Dir(path)
	parentReal, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve archive parent %s: %w", parent, err)
	}
	if err := requireInside(rootReal, parentReal); err != nil {
		return fmt.Errorf("archive parent %s escapes real extraction root %s: %w", parent, rootReal, err)
	}

	return nil
}

func modePerm(header *tar.Header, fallback os.FileMode) os.FileMode {
	perm := header.FileInfo().Mode().Perm()
	if perm == 0 {
		return fallback
	}

	return perm
}
