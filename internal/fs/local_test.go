package fs_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/HopperShell/ferry/internal/fs"
)

func TestLocalFS_List(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	lfs := fs.NewLocalFS()
	entries, err := lfs.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLocalFS_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	lfs := fs.NewLocalFS()
	err := lfs.Write(path, bytes.NewReader([]byte("ferry")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = lfs.Read(path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "ferry" {
		t.Fatalf("expected 'ferry', got %q", buf.String())
	}
}

func TestLocalFS_MkdirRemoveRename(t *testing.T) {
	dir := t.TempDir()
	lfs := fs.NewLocalFS()

	sub := filepath.Join(dir, "newdir")
	if err := lfs.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	renamed := filepath.Join(dir, "renamed")
	if err := lfs.Rename(sub, renamed); err != nil {
		t.Fatal(err)
	}

	if err := lfs.Remove(renamed); err != nil {
		t.Fatal(err)
	}

	_, err := lfs.Stat(renamed)
	if err == nil {
		t.Fatal("expected error after remove")
	}
}
