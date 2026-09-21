package cache

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ccsrvs/codetwin/internal/fingerprint"
)

// Storage persists CodeTwin's cache state. Implementations may use any
// backend as long as Load returns a usable current-schema cache and Save is
// safe to call repeatedly.
type Storage interface {
	Load() (*Cache, error)
	Save(*Cache) error
}

// GobStorage is the default CGO-free Storage implementation. It preserves
// the existing .codetwin-cache.bin format and replaces it atomically.
type GobStorage struct {
	dir string
	ops gobStorageOps
}

type syncWriteCloser interface {
	io.Writer
	Sync() error
	Close() error
}

type gobStorageOps struct {
	createTemp func(dir, pattern string) (syncWriteCloser, string, error)
	remove     func(string) error
	rename     func(string, string) error
	syncDir    func(string) error
}

func defaultGobStorageOps() gobStorageOps {
	return gobStorageOps{
		createTemp: func(dir, pattern string) (syncWriteCloser, string, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, "", err
			}
			return f, f.Name(), nil
		},
		remove:  os.Remove,
		rename:  os.Rename,
		syncDir: syncDirectory,
	}
}

// NewGobStorage returns a gob-backed cache store rooted at dir.
func NewGobStorage(dir string) *GobStorage {
	if dir == "" {
		dir = "."
	}
	return &GobStorage{dir: dir, ops: defaultGobStorageOps()}
}

func (s *GobStorage) path() string {
	return filepath.Join(s.dir, Filename)
}

// Load returns current-schema cache state. Missing, corrupt, or stale cache
// data is treated as an empty cache; other I/O errors are returned.
func (s *GobStorage) Load() (*Cache, error) {
	f, err := os.Open(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("cache open: %w", err)
	}
	defer f.Close()

	var c Cache
	if err := gob.NewDecoder(f).Decode(&c); err != nil {
		return New(), nil
	}
	if c.Version != Version || c.Schema != SchemaTag() || c.Entries == nil {
		return New(), nil
	}
	for key, e := range c.Entries {
		for _, ch := range e.Chunks {
			if ch.K != fingerprint.DefaultK {
				delete(c.Entries, key)
				break
			}
		}
	}
	return &c, nil
}

// Save writes dirty cache state using a uniquely named temporary file, syncs
// its contents, atomically renames it into place, and syncs the containing
// directory where the platform supports directory handles.
func (s *GobStorage) Save(c *Cache) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}

	f, tmp, err := s.ops.createTemp(s.dir, "."+Filename+".*.tmp")
	if err != nil {
		return fmt.Errorf("cache create: %w", err)
	}
	cleanup := func() {
		_ = f.Close()
		_ = s.ops.remove(tmp)
	}
	if err := gob.NewEncoder(f).Encode(c); err != nil {
		cleanup()
		return fmt.Errorf("cache encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("cache sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.ops.remove(tmp)
		return fmt.Errorf("cache close: %w", err)
	}
	if err := s.ops.rename(tmp, s.path()); err != nil {
		_ = s.ops.remove(tmp)
		return fmt.Errorf("cache rename: %w", err)
	}
	if err := s.ops.syncDir(s.dir); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

func syncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cache directory open: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("cache directory sync: %w", err)
	}
	return nil
}
