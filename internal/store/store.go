// Package store manages Beeket's on-disk layout.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store provides typed access to Beeket's data directory.
type Store struct {
	dataDir string
}

// New creates a Store rooted at dataDir, creating subdirectories as needed.
func New(dataDir string) (*Store, error) {
	for _, sub := range []string{"blobs", "manifests", "mmproj", "tmp", "lib"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", sub, err)
		}
	}
	return &Store{dataDir: dataDir}, nil
}

// DataDir returns the root data directory.
func (s *Store) DataDir() string { return s.dataDir }

// BlobPath returns the path for a blob identified by its hex digest.
func (s *Store) BlobPath(digest string) string {
	return filepath.Join(s.dataDir, "blobs", "sha256-"+digest)
}

// MMProjPath returns the path for a vision projector blob.
func (s *Store) MMProjPath(digest string) string {
	return filepath.Join(s.dataDir, "mmproj", "sha256-"+digest)
}

// TmpPath returns a unique tmp file path for in-flight downloads.
func (s *Store) TmpPath(name string) string {
	return filepath.Join(s.dataDir, "tmp", name)
}

// ManifestPath returns the path for a manifest file given name and tag.
func (s *Store) ManifestPath(name, tag string) string {
	return filepath.Join(s.dataDir, "manifests", name, tag+".json")
}

// WriteManifest atomically writes v as JSON to the manifest file for name:tag.
func (s *Store) WriteManifest(name, tag string, v any) error {
	dir := filepath.Join(s.dataDir, "manifests", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir manifests/%s: %w", name, err)
	}
	dest := filepath.Join(dir, tag+".json")
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("store: create tmp manifest: %w", err)
	}
	if err := json.NewEncoder(f).Encode(v); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("store: encode manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("store: close manifest tmp: %w", err)
	}
	return os.Rename(tmp, dest)
}

// ReadManifest reads and JSON-decodes the manifest for name:tag into v.
func (s *Store) ReadManifest(name, tag string, v any) error {
	f, err := os.Open(s.ManifestPath(name, tag))
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close() //nolint:errcheck // read-only file; close error is not actionable
	}()
	return json.NewDecoder(f).Decode(v)
}

// DeleteManifest removes the manifest file for name:tag.
func (s *Store) DeleteManifest(name, tag string) error {
	err := os.Remove(s.ManifestPath(name, tag))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ListManifests returns all (name, tag) pairs in the manifests directory.
func (s *Store) ListManifests() ([][2]string, error) {
	root := filepath.Join(s.dataDir, "manifests")
	var result [][2]string
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	for _, nameEntry := range entries {
		if !nameEntry.IsDir() {
			continue
		}
		tagEntries, err := os.ReadDir(filepath.Join(root, nameEntry.Name()))
		if err != nil {
			continue
		}
		for _, tagEntry := range tagEntries {
			tag := tagEntry.Name()
			if len(tag) > 5 && tag[len(tag)-5:] == ".json" {
				tag = tag[:len(tag)-5]
			}
			result = append(result, [2]string{nameEntry.Name(), tag})
		}
	}
	return result, nil
}

// BlobExists returns true if the blob with the given digest is present.
func (s *Store) BlobExists(digest string) bool {
	_, err := os.Stat(s.BlobPath(digest))
	return err == nil
}

// AtomicWriteBlob writes data to a blob file, using a tmp file to ensure atomicity.
// The writer function receives an io.Writer to write blob bytes to.
func (s *Store) AtomicWriteBlob(digest string, write func(io.Writer) error) error {
	dest := s.BlobPath(digest)
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("store: create blob tmp: %w", err)
	}
	if err := write(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("store: write blob: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("store: close blob tmp: %w", err)
	}
	return os.Rename(tmp, dest)
}

// LibDir returns the path to the managed shared library directory.
func (s *Store) LibDir() string {
	return filepath.Join(s.dataDir, "lib")
}
