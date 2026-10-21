package fsblob

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type dirs interface {
	Stat(name string) (fs.FileInfo, error)
	Mkdir(name string, perm fs.FileMode) error
	Open(name string) (*os.File, error)
}

type hostDirs struct{}

func (hostDirs) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }
func (hostDirs) Mkdir(name string, perm fs.FileMode) error {
	return os.Mkdir(name, perm)
}
func (hostDirs) Open(name string) (*os.File, error) { return os.Open(name) }

func mkdirDurable(d dirs, dir string) error {
	_, err := d.Stat(dir)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return err
	}
	if err := mkdirDurable(d, parent); err != nil {
		return err
	}
	if err := d.Mkdir(dir, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return syncDir(d, parent)
}

func syncDir(d dirs, dir string) error {
	f, err := d.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// syncChain syncs dir and each of its parents up to the root ".", so that every entry leading to a new file is
// durable even if a concurrent Put created the directories and has not synced them yet.
func syncChain(d dirs, dir string) error {
	for {
		if err := syncDir(d, dir); err != nil {
			return err
		}
		if dir == "." {
			return nil
		}
		dir = filepath.Dir(dir)
	}
}
