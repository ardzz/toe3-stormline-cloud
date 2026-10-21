// Package fsblob stores blobs as files under a root directory.
package fsblob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

var _ blob.Store = (*Store)(nil)

// Store keeps each blob in its own file below root.
type Store struct{ root *os.Root }

// New creates root if needed and returns a Store on it.
func New(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := mkdirDurable(hostDirs{}, abs); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &Store{root: r}, nil
}

// Close releases the storage root directory handle.
func (s *Store) Close() error { return s.root.Close() }

func (s *Store) path(key string) (string, error) {
	if err := blob.CheckKey(key); err != nil {
		return "", err
	}
	return filepath.FromSlash(key), nil
}

// Put writes data durably: a temporary file is synced and renamed into place, then every directory up to the root is synced.
func (s *Store) Put(ctx context.Context, key string, data []byte) (retErr error) {
	name, err := s.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(name)
	if err := s.root.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".tmp-"+hex.EncodeToString(random[:]))
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			retErr = errors.Join(retErr, s.root.Remove(tmp))
		}
	}()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0o640)
	}
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	// Do not publish after the deadline: once an upload's window has passed, its queued key may already have been swept.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Rename(tmp, name); err != nil {
		return err
	}
	renamed = true
	return syncChain(s.root, dir)
}

// Get reads a blob, mapping a missing file to blob.ErrNotFound.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := s.root.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, blob.ErrNotFound
	}
	return data, err
}

// Delete removes a blob durably; a missing file or directory is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Persist the unlink before reporting success, because the caller then forgets the blob. A retry that finds the
	// file already gone syncs again in case an earlier sync failed.
	if err := syncDir(s.root, filepath.Dir(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
