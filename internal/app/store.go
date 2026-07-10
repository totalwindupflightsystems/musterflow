// Package app provides DuckDB-backed storage for the API registry.
package app

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// Store provides DuckDB-backed persistence for API connections.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a DuckDB database at the given path in read-write mode.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("duckdb", dbPath+"?access_mode=read_write")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// NewStoreReadOnly opens a DuckDB database in read-only mode.
// It does NOT run migrations (migrations are write operations).
// The caller must ensure the database has already been migrated by a prior read-write open.
func NewStoreReadOnly(dbPath string) (*Store, error) {
	db, err := sql.Open("duckdb", dbPath+"?access_mode=read_only")
	if err != nil {
		return nil, fmt.Errorf("open duckdb (read-only): %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping duckdb (read-only): %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS api_connections (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			spec_url TEXT NOT NULL,
			base_url TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			auth_type TEXT NOT NULL DEFAULT 'none',
			endpoint_count INTEGER NOT NULL DEFAULT 0,
			added_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Add inserts or replaces an API connection.
func (s *Store) Add(conn *APIConnection) error {
	conn.AddedAt = time.Now()
	conn.UpdatedAt = time.Now()
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO api_connections
		(id, name, spec_url, base_url, version, description, auth_type, endpoint_count, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, conn.ID, conn.Name, conn.SpecURL, conn.BaseURL, conn.Version,
		conn.Description, conn.AuthType, conn.EndpointCount,
		conn.AddedAt, conn.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert connection: %w", err)
	}
	return nil
}

// Get retrieves an API connection by ID.
func (s *Store) Get(id string) (*APIConnection, error) {
	row := s.db.QueryRow(`
		SELECT id, name, spec_url, base_url, version, description, auth_type, endpoint_count, added_at, updated_at
		FROM api_connections WHERE id = ?
	`, id)
	return scanConnection(row)
}

// List returns all API connections.
func (s *Store) List() ([]*APIConnection, error) {
	rows, err := s.db.Query(`
		SELECT id, name, spec_url, base_url, version, description, auth_type, endpoint_count, added_at, updated_at
		FROM api_connections ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()

	var conns []*APIConnection
	for rows.Next() {
		conn, err := scanConnectionRow(rows)
		if err != nil {
			return nil, err
		}
		conns = append(conns, conn)
	}
	if conns == nil {
		conns = []*APIConnection{}
	}
	return conns, rows.Err()
}

// Remove deletes an API connection by ID.
func (s *Store) Remove(id string) error {
	res, err := s.db.Exec(`DELETE FROM api_connections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api %q not found", id)
	}
	return nil
}

// Has returns true if a connection with the given ID exists.
func (s *Store) Has(id string) bool {
	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM api_connections WHERE id = ?)`, id).Scan(&exists)
	return exists
}

func scanConnection(scanner interface{ Scan(...interface{}) error }) (*APIConnection, error) {
	var c APIConnection
	var addedAt, updatedAt time.Time
	err := scanner.Scan(&c.ID, &c.Name, &c.SpecURL, &c.BaseURL, &c.Version,
		&c.Description, &c.AuthType, &c.EndpointCount, &addedAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan connection: %w", err)
	}
	c.AddedAt = addedAt
	c.UpdatedAt = updatedAt
	return &c, nil
}

func scanConnectionRow(rows *sql.Rows) (*APIConnection, error) {
	var c APIConnection
	var addedAt, updatedAt time.Time
	err := rows.Scan(&c.ID, &c.Name, &c.SpecURL, &c.BaseURL, &c.Version,
		&c.Description, &c.AuthType, &c.EndpointCount, &addedAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan row: %w", err)
	}
	c.AddedAt = addedAt
	c.UpdatedAt = updatedAt
	return &c, nil
}
