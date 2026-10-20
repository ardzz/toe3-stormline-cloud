package fsblob

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type recordingDirs struct {
	hostDirs
	opened []string
}

func (d *recordingDirs) Open(name string) (*os.File, error) {
	d.opened = append(d.opened, name)
	return d.hostDirs.Open(name)
}

func TestMkdirDurableSyncsNewDirectoryParents(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "a", "b", "c")
	d := &recordingDirs{}
	if err := mkdirDurable(d, dir); err != nil {
		t.Fatal(err)
	}
	want := []string{base, filepath.Join(base, "a"), filepath.Join(base, "a", "b")}
	if !slices.Equal(d.opened, want) {
		t.Fatalf("synced directories = %v; want %v", d.opened, want)
	}
	d.opened = nil
	if err := mkdirDurable(d, dir); err != nil {
		t.Fatal(err)
	}
	if len(d.opened) != 0 {
		t.Fatalf("existing path synced directories = %v; want none", d.opened)
	}
}

type recordingRoot struct {
	*os.Root
	opened []string
}

func (r *recordingRoot) Open(name string) (*os.File, error) {
	r.opened = append(r.opened, name)
	return r.Root.Open(name)
}

func TestSyncChainSyncsEveryAncestorOfExistingDirectories(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := root.MkdirAll(filepath.Join("u", "7"), 0o750); err != nil {
		t.Fatal(err)
	}
	rec := &recordingRoot{Root: root}
	if err := syncChain(rec, filepath.Join("u", "7")); err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join("u", "7"), "u", "."}
	if !slices.Equal(rec.opened, want) {
		t.Fatalf("synced directories = %v; want %v", rec.opened, want)
	}
}
