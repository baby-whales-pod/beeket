// Package models manages the Beeket model registry: manifests, aliases, metadata.
package models

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/baby-whales-pod/beeket/internal/store"
)

// Details holds metadata about a model extracted from its GGUF header.
type Details struct {
	Family             string `json:"family"`
	ParameterSize      string `json:"parameter_size"`
	QuantizationLevel  string `json:"quantization_level"`
	ContextLength      int    `json:"context_length"`
	EmbeddingLength    int    `json:"embedding_length,omitempty"`
	Format             string `json:"format"`
	HasVision          bool   `json:"has_vision,omitempty"`
	HasEmbeddings      bool   `json:"has_embeddings,omitempty"`
	ChatTemplate       string `json:"chat_template,omitempty"`
}

// Manifest is the on-disk record for a pulled model.
type Manifest struct {
	Name       string    `json:"name"`
	Tag        string    `json:"tag"`
	Digest     string    `json:"digest"`      // hex SHA-256 of the GGUF blob
	MMProjDigest string  `json:"mmproj_digest,omitempty"`
	Size       int64     `json:"size"`
	Source     string    `json:"source"`      // original pull URL / HF ref
	ModifiedAt time.Time `json:"modified_at"`
	Details    Details   `json:"details"`
}

// FullName returns "name:tag".
func (m *Manifest) FullName() string {
	return m.Name + ":" + m.Tag
}

// Manager is the model registry backed by the Store.
type Manager struct {
	store   *store.Store
	aliases *AliasTable
}

// New creates a Manager.
func New(st *store.Store) *Manager {
	return &Manager{
		store:   st,
		aliases: DefaultAliases(),
	}
}

// Resolve resolves a model reference to a (name, tag) pair.
// It expands built-in aliases and normalises bare names.
func (m *Manager) Resolve(ref string) (name, tag string) {
	// Already has a colon — split on last colon.
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}
	// Check aliases table.
	if entry := m.aliases.Lookup(ref); entry != nil {
		return entry.Name, entry.Tag
	}
	return ref, "latest"
}

// Get returns the manifest for name:tag.
func (m *Manager) Get(name, tag string) (*Manifest, error) {
	var mf Manifest
	if err := m.store.ReadManifest(name, tag, &mf); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("model %s:%s not found", name, tag)
		}
		return nil, err
	}
	return &mf, nil
}

// Save persists a manifest.
func (m *Manager) Save(mf *Manifest) error {
	return m.store.WriteManifest(mf.Name, mf.Tag, mf)
}

// Delete removes a model manifest (does not touch the blob).
func (m *Manager) Delete(name, tag string) error {
	return m.store.DeleteManifest(name, tag)
}

// List returns all installed model manifests.
func (m *Manager) List() ([]*Manifest, error) {
	pairs, err := m.store.ListManifests()
	if err != nil {
		return nil, err
	}
	var manifests []*Manifest
	for _, p := range pairs {
		mf, err := m.Get(p[0], p[1])
		if err != nil {
			continue // skip corrupt entries
		}
		manifests = append(manifests, mf)
	}
	return manifests, nil
}

// BlobPath returns the filesystem path for a model's GGUF blob.
func (m *Manager) BlobPath(mf *Manifest) string {
	return m.store.BlobPath(mf.Digest)
}

// AliasLookup looks up a model reference in the built-in alias table.
func (m *Manager) AliasLookup(ref string) *AliasEntry {
	return m.aliases.Lookup(ref)
}
