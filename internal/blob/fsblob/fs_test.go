package fsblob_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob/blobtest"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/fsblob"
)

func TestStoreContract(t *testing.T) {
	s, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobtest.Run(t, s)
}

func TestNewCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b")
	if _, err := fsblob.New(root); err != nil {
		t.Fatal(err)
	}
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
