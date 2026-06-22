// Package app provides the core application state for MusterFlow:
// API registry (connected APIs), local storage, and lifecycle management.
package app

import (
	"encoding/json"
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
type Registry struct {
	mu          sync.RWMutex
	connections map[string]*APIConnection
	dataDir     string
	dbPath      string
}

// NewRegistry creates a new API registry backed by a JSON file.
func NewRegistry(dataDir string) *Registry {
	return &Registry{
		connections: make(map[string]*APIConnection),
		dataDir:     dataDir,
		dbPath:      filepath.Join(dataDir, "registry.json"),
	}
}

// Load reads the registry from disk.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	data, err := os.ReadFile(r.dbPath)
	if os.IsNotExist(err) {
		return nil // empty registry
	}
	if err != nil {
		return fmt.Errorf("read registry: %w", err)
	}

	return json.Unmarshal(data, &r.connections)
}

// Save writes the registry to disk.
func (r *Registry) save() error {
	if err := os.MkdirAll(r.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := json.MarshalIndent(r.connections, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	return os.WriteFile(r.dbPath, data, 0644)
}

// Add adds a new API connection and persists it.
func (r *Registry) Add(conn *APIConnection) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn.AddedAt = time.Now()
	conn.UpdatedAt = time.Now()
	r.connections[conn.ID] = conn
	return r.save()
}

// Get returns an API connection by ID.
func (r *Registry) Get(id string) (*APIConnection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, ok := r.connections[id]
	if !ok {
		return nil, fmt.Errorf("api %q not found", id)
	}
	return conn, nil
}

// List returns all connected APIs.
func (r *Registry) List() []*APIConnection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*APIConnection, 0, len(r.connections))
	for _, conn := range r.connections {
		result = append(result, conn)
	}
	return result
}

// Remove removes an API connection.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.connections[id]; !ok {
		return fmt.Errorf("api %q not found", id)
	}
	delete(r.connections, id)
	return r.save()
}

// DefaultDataDir returns the default data directory for MusterFlow.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".musterflow")
	}
	return filepath.Join(home, ".musterflow")
}
