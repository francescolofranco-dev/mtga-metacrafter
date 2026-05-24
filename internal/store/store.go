package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/francescolofranco-dev/mtga-metacrafter/internal/model"
)

// Store holds the current Dataset in memory and persists it to a JSON file
// on disk so cold starts recover the last good snapshot.
type Store struct {
	path string

	mu       sync.Mutex // guards persistence (writes serialized)
	current  atomic.Pointer[model.Dataset]
}

// Open returns a Store backed by the file at path. If the file exists, the
// dataset is loaded into memory.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir: %w", err)
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var ds model.Dataset
		if err := json.Unmarshal(data, &ds); err != nil {
			return nil, fmt.Errorf("store: parse %s: %w", path, err)
		}
		s.current.Store(&ds)
	case os.IsNotExist(err):
		// fresh start; current stays nil
	default:
		return nil, fmt.Errorf("store: read %s: %w", path, err)
	}

	return s, nil
}

// Get returns the current Dataset, or nil if none has been loaded.
func (s *Store) Get() *model.Dataset {
	return s.current.Load()
}

// Swap atomically replaces the in-memory dataset and persists it to disk.
// Writes are serialized but reads are lock-free.
func (s *Store) Swap(ds *model.Dataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tmp := s.path + ".tmp"
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("store: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("store: rename: %w", err)
	}
	s.current.Store(ds)
	return nil
}

// SeedFromFile loads an initial dataset from a seed JSON file IF the store is
// currently empty. Used on first boot before the scraper has run.
func (s *Store) SeedFromFile(path string) error {
	if s.current.Load() != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("store: read seed %s: %w", path, err)
	}
	var ds model.Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return fmt.Errorf("store: parse seed %s: %w", path, err)
	}
	return s.Swap(&ds)
}
