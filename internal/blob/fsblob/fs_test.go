package fsblob_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/blobtest"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/fsblob"
)

func TestStoreContract(t *testing.T) {
	s, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	blobtest.Run(t, s)
}

func TestNewCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b")
	s, err := fsblob.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("root = %v, %v; want a directory", info, err)
	}
}

func TestPutLeavesOnlyTheFinalFile(t *testing.T) {
	root := t.TempDir()
	s, err := fsblob.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := s.Put(context.Background(), "u/1/1.json.gz", []byte("x")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "u", "1"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "1.json.gz" {
		t.Fatalf("directory = %v, %v; want only 1.json.gz", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v; want 0640", info.Mode().Perm())
	}
}

func TestStoreStaysInsideRootThroughSymlinks(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "u")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "v"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "v", "1")); err != nil {
		t.Fatal(err)
	}
	s, err := fsblob.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	for _, key := range []string{"u/secret", "v/1"} {
		if got, err := s.Get(ctx, key); err == nil || errors.Is(err, blob.ErrNotFound) || len(got) != 0 {
			t.Errorf("Get(%q) = %q, %v; want confinement error without content", key, got, err)
		}
	}
	if err := s.Put(ctx, "u/new", []byte("x")); err == nil {
		t.Error("Put through outside symlink succeeded")
	}
	if err := s.Delete(ctx, "u/secret"); err == nil {
		t.Error("Delete through outside symlink succeeded")
	}
	if got, err := os.ReadFile(secret); err != nil || string(got) != "secret" {
		t.Errorf("outside secret = %q, %v; want intact secret", got, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Errorf("outside/new stat = %v; want not found", err)
	}
}

func TestDeleteOfMissingBlobSucceeds(t *testing.T) {
	s, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	if err := s.Delete(ctx, "u/9/1.json.gz"); err != nil {
		t.Fatalf("Delete with missing directory = %v; want nil", err)
	}
	if err := s.Put(ctx, "u/9/1.json.gz", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "u/9/2.json.gz"); err != nil {
		t.Fatalf("Delete with existing directory = %v; want nil", err)
	}
}
