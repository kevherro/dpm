// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kevherro/dpm/internal/checksum"
)

const (
	// SnapshotFile is the signed snapshot metadata filename.
	SnapshotFile = "snapshot.json"
	// SnapshotSignatureFile is the detached snapshot signature filename.
	SnapshotSignatureFile = "snapshot.json.sig"

	snapshotSignatureAlgorithm = "ed25519"
)

// PublicKey is a trusted Ed25519 registry snapshot public key.
type PublicKey struct {
	ID  string
	Key ed25519.PublicKey
}

// Snapshot describes the signed generated registry index.
type Snapshot struct {
	Schema   int                 `json:"schema"`
	Version  int                 `json:"version"`
	Registry string              `json:"registry"`
	Files    []SnapshotFileEntry `json:"files"`
}

// SnapshotFileEntry pins one generated metadata file.
type SnapshotFileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// VerifiedSnapshot reports a verified signed registry snapshot.
type VerifiedSnapshot struct {
	Version int
	SHA256  string
	KeyID   string
	Files   []SnapshotFileEntry
}

type snapshotSignature struct {
	Schema    int    `json:"schema"`
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// ParsePublicKeys parses comma-separated base64 Ed25519 public keys.
func ParsePublicKeys(raw string) ([]PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	keys := make([]PublicKey, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		data, err := decodeBase64(part)
		if err != nil {
			return nil, fmt.Errorf("parse registry public key: %w", err)
		}
		if len(data) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("registry public key has %d bytes, want %d", len(data), ed25519.PublicKeySize)
		}
		key := ed25519.PublicKey(data)
		keys = append(keys, PublicKey{
			ID:  KeyID(key),
			Key: key,
		})
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("registry public keys are empty")
	}

	return keys, nil
}

// KeyID returns the registry key identifier for an Ed25519 public key.
func KeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// VerifySnapshot verifies index/snapshot.json and its detached signature.
func VerifySnapshot(root string, keys []PublicKey) (VerifiedSnapshot, error) {
	if len(keys) == 0 {
		return VerifiedSnapshot{}, fmt.Errorf("no registry public keys configured")
	}
	reg, err := New(root)
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	indexDir := filepath.Join(reg.Root, StaticIndexDir)
	snapshotPath := filepath.Join(indexDir, SnapshotFile)
	snapshotData, err := readStaticFile(snapshotPath, "")
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	var snapshot Snapshot
	if err := decodeStaticJSON(snapshotPath, snapshotData, &snapshot); err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return VerifiedSnapshot{}, err
	}
	snapshotSHA := sha256Hex(snapshotData)

	var sig snapshotSignature
	signaturePath := filepath.Join(indexDir, SnapshotSignatureFile)
	signatureData, err := readStaticFile(signaturePath, "")
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := decodeStaticJSON(signaturePath, signatureData, &sig); err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := verifySnapshotSignature(snapshotData, sig, keys); err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := verifySnapshotFiles(indexDir, snapshot.Files); err != nil {
		return VerifiedSnapshot{}, err
	}

	return VerifiedSnapshot{
		Version: snapshot.Version,
		SHA256:  snapshotSHA,
		KeyID:   sig.KeyID,
		Files:   slices.Clone(snapshot.Files),
	}, nil
}

func writeSignedSnapshot(indexDir, registryName string, version int, signingKey string, files []GeneratedIndexFile) error {
	if version <= 0 {
		return fmt.Errorf("snapshot version must be greater than zero")
	}
	privateKey, err := parsePrivateKey(signingKey)
	if err != nil {
		return err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	snapshot := Snapshot{
		Schema:   CurrentSchema,
		Version:  version,
		Registry: registryName,
		Files:    snapshotFiles(indexDir, files),
	}
	if _, _, err := writeStaticJSON(indexDir, SnapshotFile, snapshot); err != nil {
		return err
	}
	snapshotData, err := os.ReadFile(filepath.Join(indexDir, SnapshotFile))
	if err != nil {
		return fmt.Errorf("read snapshot for signing: %w", err)
	}
	signature := snapshotSignature{
		Schema:    CurrentSchema,
		Algorithm: snapshotSignatureAlgorithm,
		KeyID:     KeyID(publicKey),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, snapshotData)),
	}
	if _, _, err := writeStaticJSON(indexDir, SnapshotSignatureFile, signature); err != nil {
		return err
	}

	return nil
}

func snapshotFiles(indexDir string, files []GeneratedIndexFile) []SnapshotFileEntry {
	result := make([]SnapshotFileEntry, 0, len(files))
	for _, file := range files {
		rel, err := filepath.Rel(indexDir, file.Path)
		if err != nil {
			continue
		}
		result = append(result, SnapshotFileEntry{
			Path:   filepath.ToSlash(rel),
			SHA256: file.SHA256,
		})
	}
	slices.SortFunc(result, func(a, b SnapshotFileEntry) int {
		return strings.Compare(a.Path, b.Path)
	})

	return result
}

func parsePrivateKey(raw string) (ed25519.PrivateKey, error) {
	data, err := decodeBase64(raw)
	if err != nil {
		return nil, fmt.Errorf("parse registry signing key: %w", err)
	}
	switch len(data) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(data), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(data), nil
	default:
		return nil, fmt.Errorf("registry signing key has %d bytes, want %d-byte seed or %d-byte private key", len(data), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func decodeBase64(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty base64 value")
	}
	if data, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return data, nil
	}
	if data, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		return data, nil
	}

	return nil, fmt.Errorf("value is not standard or raw base64")
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Schema != CurrentSchema {
		return fmt.Errorf("snapshot schema %d is not supported", snapshot.Schema)
	}
	if snapshot.Version <= 0 {
		return fmt.Errorf("snapshot version must be greater than zero")
	}
	if err := validatePathPart("registry", snapshot.Registry); err != nil {
		return err
	}
	if len(snapshot.Files) == 0 {
		return fmt.Errorf("snapshot files are required")
	}
	for _, file := range snapshot.Files {
		if err := validateStaticPath(file.Path); err != nil {
			return fmt.Errorf("snapshot file path %q: %w", file.Path, err)
		}
		if _, err := checksum.NormalizeSHA256(file.SHA256); err != nil {
			return fmt.Errorf("snapshot file %q sha256: %w", file.Path, err)
		}
	}

	return nil
}

func verifySnapshotSignature(snapshotData []byte, sig snapshotSignature, keys []PublicKey) error {
	if sig.Schema != CurrentSchema {
		return fmt.Errorf("snapshot signature schema %d is not supported", sig.Schema)
	}
	if sig.Algorithm != snapshotSignatureAlgorithm {
		return fmt.Errorf("snapshot signature algorithm %q is not supported", sig.Algorithm)
	}
	signature, err := decodeBase64(sig.Signature)
	if err != nil {
		return fmt.Errorf("parse snapshot signature: %w", err)
	}
	for _, key := range keys {
		if key.ID != sig.KeyID {
			continue
		}
		if ed25519.Verify(key.Key, snapshotData, signature) {
			return nil
		}

		return fmt.Errorf("snapshot signature verification failed for key %s", sig.KeyID)
	}

	return fmt.Errorf("snapshot signature key %s is not trusted", sig.KeyID)
}

func verifySnapshotFiles(indexDir string, files []SnapshotFileEntry) error {
	for _, file := range files {
		path := filepath.Join(indexDir, filepath.FromSlash(file.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read snapshot file %s: %w", path, err)
		}
		actual := sha256Hex(data)
		if actual != file.SHA256 {
			return checksum.MismatchError{Path: path, Expected: file.SHA256, Actual: actual}
		}
	}

	return nil
}

func readStaticFile(path, expectedSHA string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if expectedSHA == "" {
		expectedSHA, err = readStaticSidecar(path)
		if err != nil {
			return nil, err
		}
	}
	actual := sha256Hex(data)
	if actual != expectedSHA {
		return nil, checksum.MismatchError{Path: path, Expected: expectedSHA, Actual: actual}
	}

	return data, nil
}

func decodeStaticJSON(path string, data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("parse static json %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse static json %s: trailing data", path)
	}

	return nil
}
