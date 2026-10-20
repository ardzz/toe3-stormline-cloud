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
