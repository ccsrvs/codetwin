package cache

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type memoryStorage struct {
	state *Cache
}

type faultFile struct {
	bytes.Buffer
	writeErr error
	syncErr  error
	closeErr error
}

func (f *faultFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(p)
}

func (f *faultFile) Sync() error  { return f.syncErr }
func (f *faultFile) Close() error { return f.closeErr }

func (s *memoryStorage) Load() (*Cache, error) {
	if s.state == nil {
		return New(), nil
	}
	return s.state, nil
}

func (s *memoryStorage) Save(state *Cache) error {
	s.state = state
	return nil
}

func TestStorageContractAllowsAlternativeBackend(t *testing.T) {
	var storage Storage = &memoryStorage{}
	state, err := storage.Load()
	if err != nil {
		t.Fatalf("load memory storage: %v", err)
	}
	state.Put("key", Entry{ContentHash: "hash"})
	if err := storage.Save(state); err != nil {
		t.Fatalf("save memory storage: %v", err)
	}
	loaded, err := storage.Load()
	if err != nil {
		t.Fatalf("reload memory storage: %v", err)
	}
	if _, ok := loaded.Get("key"); !ok {
		t.Fatal("alternative storage lost cached entry")
	}
}

func TestGobStorageImplementsStorageAndUsesExistingFormat(t *testing.T) {
	dir := t.TempDir()
	var storage Storage = NewGobStorage(dir)
	state := New()
	state.Put("key", Entry{ContentHash: "hash"})
	if err := storage.Save(state); err != nil {
		t.Fatalf("save gob storage: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load compatibility wrapper: %v", err)
	}
	if _, ok := loaded.Get("key"); !ok {
		t.Fatal("compatibility wrapper could not read GobStorage format")
	}
	if state.dirty {
		t.Fatal("successful storage save must mark cache clean")
	}
	if leftovers, err := filepath.Glob(filepath.Join(dir, "."+Filename+".*.tmp")); err != nil {
		t.Fatalf("glob temporary files: %v", err)
	} else if len(leftovers) != 0 {
		t.Fatalf("successful save left temporary files: %v", leftovers)
	}
}

func TestNewGobStorageDefaultsEmptyDirectoryToCurrent(t *testing.T) {
	if got := NewGobStorage("").dir; got != "." {
		t.Errorf("empty storage directory = %q, want current directory", got)
	}
}

func TestGobStorageSaveFailurePathsKeepStateDirtyAndCleanUp(t *testing.T) {
	failure := errors.New("injected failure")
	tests := []struct {
		name       string
		file       *faultFile
		createErr  error
		renameErr  error
		syncDirErr error
		wantPrefix string
	}{
		{name: "create", createErr: failure, wantPrefix: "cache create:"},
		{name: "encode", file: &faultFile{writeErr: failure}, wantPrefix: "cache encode:"},
		{name: "file sync", file: &faultFile{syncErr: failure}, wantPrefix: "cache sync:"},
		{name: "close", file: &faultFile{closeErr: failure}, wantPrefix: "cache close:"},
		{name: "rename", file: &faultFile{}, renameErr: failure, wantPrefix: "cache rename:"},
		{name: "directory sync", file: &faultFile{}, syncDirErr: failure, wantPrefix: "cache directory sync:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed := false
			storage := &GobStorage{
				dir: t.TempDir(),
				ops: gobStorageOps{
					createTemp: func(_, _ string) (syncWriteCloser, string, error) {
						if tt.createErr != nil {
							return nil, "", tt.createErr
						}
						return tt.file, "temporary", nil
					},
					remove: func(string) error {
						removed = true
						return nil
					},
					rename: func(_, _ string) error { return tt.renameErr },
					syncDir: func(string) error {
						if tt.syncDirErr != nil {
							return fmt.Errorf("cache directory sync: %w", tt.syncDirErr)
						}
						return nil
					},
				},
			}
			state := New()
			state.Put("key", Entry{ContentHash: "hash"})
			err := storage.Save(state)
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Fatalf("Save error = %v, want prefix %q", err, tt.wantPrefix)
			}
			if !state.dirty {
				t.Fatal("failed save must leave cache dirty for retry")
			}
			if tt.createErr == nil && tt.syncDirErr == nil && !removed {
				t.Fatal("pre-rename failure must remove temporary file")
			}
		})
	}
}

func TestSyncDirectoryOpenFailureIsWrapped(t *testing.T) {
	err := syncDirectory(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.HasPrefix(err.Error(), "cache directory open:") {
		t.Fatalf("syncDirectory error = %v", err)
	}
}

// The cache file must get ordinary file permissions (0666 less the
// umask), like any file the user creates, not the private 0600 that
// os.CreateTemp gives the temporary file it is renamed from: CI
// containers and teammates sharing a checkout need to read it.
func TestGobStorageSaveUsesUmaskPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	reference := filepath.Join(dir, "reference")
	f, err := os.Create(reference)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	want, err := os.Stat(reference)
	if err != nil {
		t.Fatal(err)
	}

	c := New()
	c.Put("k", Entry{})
	if err := NewGobStorage(dir).Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm() != want.Mode().Perm() {
		t.Errorf("cache file mode %v, want %v (0666 less umask)", got.Mode().Perm(), want.Mode().Perm())
	}
}
