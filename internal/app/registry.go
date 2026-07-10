// Package app provides the core application state for MusterFlow:
// API registry (connected APIs), local storage, and lifecycle management.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// APIConnection represents a connected API in the user's local registry.
type APIConnection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SpecURL     string    `json:"spec_url"`
	BaseURL     string    `json:"base_url"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	AuthType    string    `json:"auth_type"` // "none", "apikey", "oauth2", "bearer", "mtls"
	AddedAt     time.Time `json:"added_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	EndpointCount int     `json:"endpoint_count"`
}

// Registry stores connected APIs and manages persistence.
// Uses DuckDB for storage, with automatic migration from legacy JSON.
type Registry struct {
	mu      sync.RWMutex
	dataDir string
	dbPath  string
	store   *Store
}

// NewRegistry creates a new API registry backed by DuckDB.
func NewRegistry(dataDir string) *Registry {
	return &Registry{
		dataDir: dataDir,
		dbPath:  filepath.Join(dataDir, "musterflow.db"),
	}
}

// Load opens the DuckDB store and migrates from legacy JSON if needed.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := NewStore(r.dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	r.store = store

	// Auto-migrate from legacy JSON
	n, err := MigrateJSONToStore(store, r.dataDir)
	if err != nil {
		store.Close()
		return fmt.Errorf("migrate JSON: %w", err)
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "Migrated %d connections from JSON to DuckDB\n", n)
	}

	return nil
}

// LoadReadOnly opens the DuckDB store in read-only mode.
// It creates the data dir if needed but skips JSON migration (a write operation).
// Use this when another process (e.g. the dashboard) holds the write lock.
func (r *Registry) LoadReadOnly() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := NewStoreReadOnly(r.dbPath)
	if err != nil {
		return fmt.Errorf("open store (read-only): %w", err)
	}
	r.store = store
	return nil
}

// Storedb returns the underlying DuckDB store for direct access.
func (r *Registry) Storedb() *Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store
}

// Add adds a new API connection and persists it.
func (r *Registry) Add(conn *APIConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.store == nil {
		return fmt.Errorf("registry not loaded")
	}

	conn.AddedAt = time.Now()
	conn.UpdatedAt = time.Now()
	return r.store.Add(conn)
}

// Get returns an API connection by ID.
func (r *Registry) Get(id string) (*APIConnection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.store == nil {
		return nil, fmt.Errorf("registry not loaded")
	}
	return r.store.Get(id)
}

// List returns all connected APIs.
func (r *Registry) List() []*APIConnection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.store == nil {
		return nil
	}
	conns, err := r.store.List()
	if err != nil {
		return nil
	}
	return conns
}

// Remove removes an API connection.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.store == nil {
		return fmt.Errorf("registry not loaded")
	}
	return r.store.Remove(id)
}

// Store returns the underlying DuckDB store (for export/import).
func (r *Registry) Store() *Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store
}

// DataDir returns the data directory for this registry.
func (r *Registry) DataDir() string {
	return r.dataDir
}

// Close closes the registry's database connection.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		return r.store.Close()
	}
	return nil
}

// DefaultDataDir returns the default data directory for MusterFlow.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".musterflow")
	}
	return filepath.Join(home, ".musterflow")
}
