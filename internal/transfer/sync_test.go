package transfer_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
	"github.com/HopperShell/ferry/internal/transfer"
)

// slowFS wraps a real FileSystem and adds artificial latency to every List
// call, simulating SFTP round-trip delays. It also tracks the peak number of
// concurrent List calls to verify parallelism.
type slowFS struct {
	fs.FileSystem
	delay      time.Duration
	concurrent atomic.Int32
	peak       atomic.Int32
}

func (s *slowFS) List(path string) ([]fs.Entry, error) {
	cur := s.concurrent.Add(1)
	for {
		old := s.peak.Load()
		if cur <= old || s.peak.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	entries, err := s.FileSystem.List(path)
	s.concurrent.Add(-1)
	return entries, err
}

// stubFS is a purely in-memory filesystem that only implements List.
// Used to avoid any real filesystem dependency.
type stubFS struct {
	dirs map[string][]fs.Entry
}

func (s *stubFS) List(path string) ([]fs.Entry, error) {
	entries, ok := s.dirs[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return entries, nil
}

func (s *stubFS) Stat(string) (fs.Entry, error)                  { return fs.Entry{}, nil }
func (s *stubFS) Read(string, io.Writer) error                   { return nil }
func (s *stubFS) Write(string, io.Reader, os.FileMode) error     { return nil }
func (s *stubFS) Mkdir(string, os.FileMode) error                { return nil }
func (s *stubFS) Remove(string) error                            { return nil }
func (s *stubFS) Rename(string, string) error                    { return nil }
func (s *stubFS) Chmod(string, os.FileMode) error                { return nil }
func (s *stubFS) Chtimes(string, time.Time) error                { return nil }
func (s *stubFS) HomeDir() (string, error)                       { return "/", nil }

// makeTree creates files and directories under root from a simple spec.
// Each entry is "dir/" for directories or "file" for a 100-byte file.
func makeTree(t *testing.T, root string, entries []string) {
	t.Helper()
	for _, e := range entries {
		p := filepath.Join(root, e)
		if e[len(e)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		} else {
			dir := filepath.Dir(p)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, make([]byte, 100), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCompareIdentical(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	tree := []string{"a.txt", "b.txt", "sub/", "sub/c.txt"}
	makeTree(t, local, tree)
	makeTree(t, remote, tree)

	// Sync mod times so entries match.
	now := time.Now().Truncate(time.Second)
	for _, e := range tree {
		if e[len(e)-1] == '/' {
			continue
		}
		os.Chtimes(filepath.Join(local, e), now, now)
		os.Chtimes(filepath.Join(remote, e), now, now)
	}

	lfs := fs.NewLocalFS()
	rfs := fs.NewLocalFS()

	entries, err := transfer.Compare(lfs, local, rfs, remote)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Status != transfer.DiffSame {
			t.Errorf("expected DiffSame for %s, got %d", e.RelPath, e.Status)
		}
	}
}

func TestCompareLocalOnly(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	makeTree(t, local, []string{"only-local.txt", "shared.txt"})
	makeTree(t, remote, []string{"shared.txt"})

	now := time.Now().Truncate(time.Second)
	os.Chtimes(filepath.Join(local, "shared.txt"), now, now)
	os.Chtimes(filepath.Join(remote, "shared.txt"), now, now)

	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.RelPath == "only-local.txt" {
			found = true
			if e.Status != transfer.DiffLocalOnly {
				t.Errorf("expected DiffLocalOnly, got %d", e.Status)
			}
		}
	}
	if !found {
		t.Error("missing only-local.txt in results")
	}
}

func TestCompareRemoteOnly(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	makeTree(t, local, []string{"shared.txt"})
	makeTree(t, remote, []string{"shared.txt", "only-remote.txt"})

	now := time.Now().Truncate(time.Second)
	os.Chtimes(filepath.Join(local, "shared.txt"), now, now)
	os.Chtimes(filepath.Join(remote, "shared.txt"), now, now)

	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.RelPath == "only-remote.txt" {
			found = true
			if e.Status != transfer.DiffRemoteOnly {
				t.Errorf("expected DiffRemoteOnly, got %d", e.Status)
			}
		}
	}
	if !found {
		t.Error("missing only-remote.txt in results")
	}
}

func TestCompareModified(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	makeTree(t, local, []string{"file.txt"})
	makeTree(t, remote, []string{"file.txt"})

	// Different sizes → modified.
	os.WriteFile(filepath.Join(remote, "file.txt"), make([]byte, 200), 0o644)

	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.RelPath == "file.txt" {
			if e.Status != transfer.DiffModified {
				t.Errorf("expected DiffModified, got %d", e.Status)
			}
			return
		}
	}
	t.Error("file.txt not found in results")
}

func TestCompareEmptyDirs(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	// Both empty — should produce no entries.
	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty dirs, got %d", len(entries))
	}
}

func TestCompareNonExistentDirs(t *testing.T) {
	// Non-existent paths should be treated as empty, not error.
	entries, err := transfer.Compare(
		fs.NewLocalFS(), "/tmp/ferry-nonexistent-local-"+fmt.Sprintf("%d", time.Now().UnixNano()),
		fs.NewLocalFS(), "/tmp/ferry-nonexistent-remote-"+fmt.Sprintf("%d", time.Now().UnixNano()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-existent dirs, got %d", len(entries))
	}
}

func TestCompareDeepTree(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	// Create a tree with nested directories to exercise concurrent walking.
	var tree []string
	for i := 0; i < 5; i++ {
		dir := fmt.Sprintf("d%d/", i)
		tree = append(tree, dir)
		for j := 0; j < 5; j++ {
			subdir := fmt.Sprintf("d%d/s%d/", i, j)
			tree = append(tree, subdir)
			for k := 0; k < 3; k++ {
				tree = append(tree, fmt.Sprintf("d%d/s%d/f%d.txt", i, j, k))
			}
		}
	}
	makeTree(t, local, tree)
	makeTree(t, remote, tree)

	now := time.Now().Truncate(time.Second)
	for _, e := range tree {
		if e[len(e)-1] != '/' {
			os.Chtimes(filepath.Join(local, e), now, now)
			os.Chtimes(filepath.Join(remote, e), now, now)
		}
	}

	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}

	// All should be DiffSame.
	for _, e := range entries {
		if e.Status != transfer.DiffSame {
			t.Errorf("expected DiffSame for %s, got %d", e.RelPath, e.Status)
		}
	}

	// Should have 5 top dirs + 25 subdirs + 75 files = 105 entries.
	if len(entries) != 105 {
		t.Errorf("expected 105 entries, got %d", len(entries))
	}
}

func TestCompareSortOrder(t *testing.T) {
	local := t.TempDir()
	remote := t.TempDir()

	makeTree(t, local, []string{"z.txt", "a.txt", "m/", "m/x.txt"})
	// remote empty — all local-only, but tests sort order.

	entries, err := transfer.Compare(fs.NewLocalFS(), local, fs.NewLocalFS(), remote)
	if err != nil {
		t.Fatal(err)
	}

	// Directories should come first, then alphabetical.
	var names []string
	for _, e := range entries {
		names = append(names, e.RelPath)
	}
	if !sort.SliceIsSorted(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].RelPath < entries[j].RelPath
	}) {
		t.Errorf("entries not sorted correctly: %v", names)
	}
}

func TestCompareConcurrentRemoteWalk(t *testing.T) {
	// Build a tree with 10 subdirectories, each containing 3 files.
	// With 50ms latency per List call, sequential would take:
	//   11 List calls × 50ms = 550ms
	// Concurrent should be significantly faster.
	local := t.TempDir()
	remote := t.TempDir()

	var tree []string
	for i := 0; i < 10; i++ {
		dir := fmt.Sprintf("d%d/", i)
		tree = append(tree, dir)
		for j := 0; j < 3; j++ {
			tree = append(tree, fmt.Sprintf("d%d/f%d.txt", i, j))
		}
	}
	makeTree(t, local, tree)
	makeTree(t, remote, tree)

	now := time.Now().Truncate(time.Second)
	for _, e := range tree {
		if e[len(e)-1] != '/' {
			os.Chtimes(filepath.Join(local, e), now, now)
			os.Chtimes(filepath.Join(remote, e), now, now)
		}
	}

	slow := &slowFS{
		FileSystem: fs.NewLocalFS(),
		delay:      50 * time.Millisecond,
	}

	start := time.Now()
	entries, err := transfer.Compare(fs.NewLocalFS(), local, slow, remote)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}

	// Should have 10 dirs + 30 files = 40 entries.
	if len(entries) != 40 {
		t.Errorf("expected 40 entries, got %d", len(entries))
	}

	// Verify correctness — all should be DiffSame.
	for _, e := range entries {
		if e.Status != transfer.DiffSame {
			t.Errorf("expected DiffSame for %s, got %d", e.RelPath, e.Status)
		}
	}

	// 11 List calls at 50ms sequential = 550ms minimum.
	// With concurrency we expect well under 300ms.
	if elapsed > 300*time.Millisecond {
		t.Errorf("Compare took %v; expected under 300ms with concurrent walking", elapsed)
	}
	t.Logf("Compare took %v with peak concurrency of %d", elapsed, slow.peak.Load())

	// Verify that multiple List calls actually ran in parallel.
	if slow.peak.Load() < 2 {
		t.Errorf("peak concurrent List calls was %d; expected >= 2 (no parallelism detected)", slow.peak.Load())
	}
}

func TestCompareConcurrentCorrectness(t *testing.T) {
	// Use a stubFS to test concurrent walker finds all entries correctly
	// when the "remote" side has a different tree than local.
	local := t.TempDir()
	makeTree(t, local, []string{"shared.txt", "local-only.txt"})

	// Build a stub remote with shared.txt + remote-only.txt + a subdir.
	now := time.Now()
	remote := &slowFS{
		delay: 20 * time.Millisecond,
		FileSystem: &stubFS{dirs: map[string][]fs.Entry{
			"/remote": {
				{Name: "shared.txt", Path: "/remote/shared.txt", Size: 100, ModTime: now, Mode: 0o644},
				{Name: "remote-only.txt", Path: "/remote/remote-only.txt", Size: 50, ModTime: now, Mode: 0o644},
				{Name: "sub", Path: "/remote/sub", IsDir: true, ModTime: now, Mode: 0o755},
			},
			"/remote/sub": {
				{Name: "deep.txt", Path: "/remote/sub/deep.txt", Size: 10, ModTime: now, Mode: 0o644},
			},
		}},
	}

	entries, err := transfer.Compare(fs.NewLocalFS(), local, remote, "/remote")
	if err != nil {
		t.Fatal(err)
	}

	statuses := make(map[string]transfer.DiffStatus)
	for _, e := range entries {
		statuses[e.RelPath] = e.Status
	}

	// local-only.txt should be local-only.
	if s, ok := statuses["local-only.txt"]; !ok || s != transfer.DiffLocalOnly {
		t.Errorf("local-only.txt: got %v (present=%v), want DiffLocalOnly", s, ok)
	}
	// remote-only.txt should be remote-only.
	if s, ok := statuses["remote-only.txt"]; !ok || s != transfer.DiffRemoteOnly {
		t.Errorf("remote-only.txt: got %v (present=%v), want DiffRemoteOnly", s, ok)
	}
	// sub/deep.txt should be remote-only.
	if s, ok := statuses["sub/deep.txt"]; !ok || s != transfer.DiffRemoteOnly {
		t.Errorf("sub/deep.txt: got %v (present=%v), want DiffRemoteOnly", s, ok)
	}
	// sub dir should be remote-only.
	if s, ok := statuses["sub"]; !ok || s != transfer.DiffRemoteOnly {
		t.Errorf("sub: got %v (present=%v), want DiffRemoteOnly", s, ok)
	}

	t.Logf("peak concurrency: %d", remote.peak.Load())
}
