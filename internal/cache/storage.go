package cache

import (
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
		createTemp: createUmaskTemp,
		remove:     os.Remove,
		rename:     os.Rename,
		syncDir:    syncDirectory,
	}
}

// createUmaskTemp is os.CreateTemp with ordinary permissions: it opens
// with mode 0666 so the umask decides, as for any file the user makes.
// os.CreateTemp forces 0600, and the rename would carry that private
// mode onto the cache file itself.
func createUmaskTemp(dir, pattern string) (syncWriteCloser, string, error) {
	prefix, suffix, _ := strings.Cut(pattern, "*")
	for attempt := 0; attempt < 16; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", err
		}
		name := filepath.Join(dir, prefix+hex.EncodeToString(random[:])+suffix)
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return f, name, nil
	}
	return nil, "", fmt.Errorf("create temporary cache file in %s: too many name collisions", dir)
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
