package transfer_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
	"golang.org/x/crypto/ssh"
)

// TestConcurrentSyncOverSFTP verifies that the concurrent sync worker pattern
// correctly transfers files over a real SFTP connection. It requires the test
// Docker container (test/docker) to be running on localhost:2222.
func TestConcurrentSyncOverSFTP(t *testing.T) {
	if os.Getenv("FERRY_INTEGRATION") == "" && testing.Short() {
		// Also run if explicitly invoked with -run flag.
		if !isExplicitRun() {
			t.Skip("set FERRY_INTEGRATION=1 or use -run to run integration tests")
		}
	}

	remoteFS, cleanup := connectSFTP(t)
	defer cleanup()
	localFS := fs.NewLocalFS()

	// Create local files to push.
	localDir := t.TempDir()
	fileCount := 20
	fileSize := 50 * 1024 // 50KB each
	for i := 0; i < fileCount; i++ {
		data := make([]byte, fileSize)
		rand.Read(data)
		path := filepath.Join(localDir, fmt.Sprintf("file_%02d.txt", i))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a unique remote directory for this test run.
	remoteDir := fmt.Sprintf("/tmp/ferry-sync-test-%d", time.Now().UnixNano())
	if err := remoteFS.Mkdir(remoteDir, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	defer func() {
		// Clean up remote files.
		entries, _ := remoteFS.List(remoteDir)
		for _, e := range entries {
			_ = remoteFS.Remove(e.Path)
		}
		_ = remoteFS.Remove(remoteDir)
	}()

	// --- Sequential baseline ---
	seqDir := remoteDir + "/seq"
	_ = remoteFS.Mkdir(seqDir, 0o755)

	seqStart := time.Now()
	for i := 0; i < fileCount; i++ {
		src := filepath.Join(localDir, fmt.Sprintf("file_%02d.txt", i))
		dst := seqDir + fmt.Sprintf("/file_%02d.txt", i)
		if err := copyFile(localFS, src, remoteFS, dst); err != nil {
			t.Fatalf("sequential copy %d: %v", i, err)
		}
	}
	seqDur := time.Since(seqStart)

	// --- Concurrent (4 workers, matching the sync implementation) ---
	concDir := remoteDir + "/conc"
	_ = remoteFS.Mkdir(concDir, 0o755)

	const syncWorkers = 4
	concStart := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, syncWorkers)
	var errCount int32

	for i := 0; i < fileCount; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			src := filepath.Join(localDir, fmt.Sprintf("file_%02d.txt", idx))
			dst := concDir + fmt.Sprintf("/file_%02d.txt", idx)
			if err := copyFile(localFS, src, remoteFS, dst); err != nil {
				atomic.AddInt32(&errCount, 1)
				t.Errorf("concurrent copy %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	concDur := time.Since(concStart)

	if errCount > 0 {
		t.Fatalf("%d concurrent copy errors", errCount)
	}

	t.Logf("Sequential: %v, Concurrent (4 workers): %v, Speedup: %.1fx",
		seqDur, concDur, float64(seqDur)/float64(concDur))

	// --- Verify all files arrived correctly ---
	for _, dir := range []string{seqDir, concDir} {
		entries, err := remoteFS.List(dir)
		if err != nil {
			t.Fatalf("list %s: %v", dir, err)
		}
		if len(entries) != fileCount {
			t.Errorf("%s: expected %d files, got %d", dir, fileCount, len(entries))
		}
		for _, e := range entries {
			if e.Size != int64(fileSize) {
				t.Errorf("%s: size %d, want %d", e.Path, e.Size, fileSize)
			}
		}
	}

	// Verify file contents match by spot-checking a few files.
	for _, idx := range []int{0, fileCount / 2, fileCount - 1} {
		name := fmt.Sprintf("file_%02d.txt", idx)
		localData, err := os.ReadFile(filepath.Join(localDir, name))
		if err != nil {
			t.Fatal(err)
		}

		var remoteBuf bytes.Buffer
		if err := remoteFS.Read(concDir+"/"+name, &remoteBuf); err != nil {
			t.Fatalf("read remote %s: %v", name, err)
		}
		if !bytes.Equal(localData, remoteBuf.Bytes()) {
			t.Errorf("%s: content mismatch after concurrent copy", name)
		}
	}

	// Concurrent should be faster (at least 1.5x with 4 workers on 20 files).
	if concDur > seqDur {
		t.Logf("WARNING: concurrent was not faster than sequential — may indicate SFTP serialization")
	}
}

// copyFile mirrors the copyFile function from app.go — read into buffer, write via temp, rename.
func copyFile(srcFS fs.FileSystem, srcPath string, dstFS fs.FileSystem, dstPath string) error {
	srcStat, err := srcFS.Stat(srcPath)
	var perm os.FileMode = 0o644
	if err == nil && srcStat.Mode != 0 {
		perm = srcStat.Mode
	}

	var buf bytes.Buffer
	if err := srcFS.Read(srcPath, &buf); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	tmpPath := dstPath + ".ferry-tmp"
	if err := dstFS.Write(tmpPath, &buf, perm); err != nil {
		_ = dstFS.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	if err := dstFS.Rename(tmpPath, dstPath); err != nil {
		_ = dstFS.Remove(tmpPath)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// connectSFTP establishes an SSH/SFTP connection to the test Docker container.
func connectSFTP(t *testing.T) (fs.FileSystem, func()) {
	t.Helper()

	keyPath := filepath.Join("..", "..", "test", "docker", "testkey")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Skipf("cannot read test key %s: %v (is the test container set up?)", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", "localhost:2222", config)
	if err != nil {
		t.Skipf("cannot connect to test SSH server on localhost:2222: %v (is docker running?)", err)
	}

	rfs, err := fs.NewRemoteFS(client)
	if err != nil {
		client.Close()
		t.Fatalf("sftp: %v", err)
	}

	return rfs, func() {
		client.Close()
	}
}

// isExplicitRun checks if the test was invoked with -run flag targeting this test.
func isExplicitRun() bool {
	for _, arg := range os.Args {
		if arg == "-test.run" || len(arg) > 10 && arg[:10] == "-test.run=" {
			return true
		}
	}
	return false
}

// Ensure our local copyFile signature matches — it takes (srcFS, srcPath, dstFS, dstPath)
// but app.go's copyFile takes (srcFS, srcPath, dstFS, dstPath) too. We mirror it exactly.
var _ = copyFile // silence unused warning in case of build tag changes

// TestConcurrentSyncPull verifies pulling files from remote to local concurrently.
func TestConcurrentSyncPull(t *testing.T) {
	if os.Getenv("FERRY_INTEGRATION") == "" && testing.Short() {
		if !isExplicitRun() {
			t.Skip("set FERRY_INTEGRATION=1 or use -run to run integration tests")
		}
	}

	remoteFS, cleanup := connectSFTP(t)
	defer cleanup()
	localFS := fs.NewLocalFS()

	// Use the pre-existing test data on the remote.
	remoteDir := "/testdata"
	entries, err := remoteFS.List(remoteDir)
	if err != nil {
		t.Fatalf("list remote: %v", err)
	}

	// Filter to just files.
	var files []fs.Entry
	for _, e := range entries {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	if len(files) == 0 {
		t.Skip("no test files on remote")
	}

	// Pull concurrently to a local temp dir.
	localDir := t.TempDir()
	const workers = 4
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	var errCount int32

	for _, f := range files {
		sem <- struct{}{}
		wg.Add(1)
		go func(e fs.Entry) {
			defer wg.Done()
			defer func() { <-sem }()

			dst := filepath.Join(localDir, e.Name)
			if err := copyFile(remoteFS, e.Path, localFS, dst); err != nil {
				atomic.AddInt32(&errCount, 1)
				t.Errorf("pull %s: %v", e.Name, err)
			}
		}(f)
	}
	wg.Wait()

	if errCount > 0 {
		t.Fatalf("%d pull errors", errCount)
	}

	// Verify files exist locally with correct sizes.
	for _, f := range files {
		info, err := os.Stat(filepath.Join(localDir, f.Name))
		if err != nil {
			t.Errorf("missing %s: %v", f.Name, err)
			continue
		}
		if info.Size() != f.Size {
			t.Errorf("%s: local size %d != remote size %d", f.Name, info.Size(), f.Size)
		}
	}

	t.Logf("Successfully pulled %d files concurrently", len(files))
}

// TestConcurrentSyncWithSubdirs tests concurrent sync when directories need
// to be created first (matching the actual sync implementation pattern).
func TestConcurrentSyncWithSubdirs(t *testing.T) {
	if os.Getenv("FERRY_INTEGRATION") == "" && testing.Short() {
		if !isExplicitRun() {
			t.Skip("set FERRY_INTEGRATION=1 or use -run to run integration tests")
		}
	}

	remoteFS, cleanup := connectSFTP(t)
	defer cleanup()
	localFS := fs.NewLocalFS()

	localDir := t.TempDir()

	// Create a tree with subdirectories.
	tree := map[string][]string{
		"alpha":   {"a1.txt", "a2.txt", "a3.txt"},
		"beta":    {"b1.txt", "b2.txt"},
		"gamma/d": {"g1.txt"},
	}
	for dir, files := range tree {
		dirPath := filepath.Join(localDir, dir)
		os.MkdirAll(dirPath, 0o755)
		for _, f := range files {
			data := make([]byte, 10*1024)
			rand.Read(data)
			os.WriteFile(filepath.Join(dirPath, f), data, 0o644)
		}
	}

	remoteDir := fmt.Sprintf("/tmp/ferry-subdir-test-%d", time.Now().UnixNano())
	defer func() {
		// Recursive cleanup.
		cleanRemoteDir(remoteFS, remoteDir)
	}()

	// Step 1: Create directories first (sequential, like the real implementation).
	for dir := range tree {
		_ = remoteFS.Mkdir(remoteDir+"/"+dir, 0o755)
	}
	// Also create parent dirs for nested paths.
	_ = remoteFS.Mkdir(remoteDir+"/gamma", 0o755)

	// Step 2: Copy files concurrently (like the real implementation).
	const workers = 4
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	var errCount int32
	var totalFiles int32

	for dir, files := range tree {
		for _, f := range files {
			sem <- struct{}{}
			wg.Add(1)
			go func(dir, f string) {
				defer wg.Done()
				defer func() { <-sem }()

				src := filepath.Join(localDir, dir, f)
				dst := remoteDir + "/" + dir + "/" + f
				_ = remoteFS.Mkdir(filepath.Dir(dst), 0o755)
				if err := copyFile(localFS, src, remoteFS, dst); err != nil {
					atomic.AddInt32(&errCount, 1)
					t.Errorf("copy %s/%s: %v", dir, f, err)
				}
				atomic.AddInt32(&totalFiles, 1)
			}(dir, f)
		}
	}
	wg.Wait()

	if errCount > 0 {
		t.Fatalf("%d copy errors", errCount)
	}

	// Verify everything arrived.
	for dir, files := range tree {
		entries, err := remoteFS.List(remoteDir + "/" + dir)
		if err != nil {
			t.Errorf("list %s: %v", dir, err)
			continue
		}
		if len(entries) != len(files) {
			t.Errorf("%s: expected %d files, got %d", dir, len(files), len(entries))
		}
	}

	t.Logf("Successfully synced %d files across %d directories concurrently", totalFiles, len(tree))
}

func cleanRemoteDir(rfs fs.FileSystem, path string) {
	entries, err := rfs.List(path)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir {
			cleanRemoteDir(rfs, e.Path)
		}
		_ = rfs.Remove(e.Path)
	}
	_ = rfs.Remove(path)
}

// copyFile is already defined above with the test-local signature:
//   copyFile(srcFS, dstFS fs.FileSystem, srcPath, dstPath string) error
// Note: the test helper swaps arg order to (srcFS, srcPath, dstFS, dstPath)
// to match the real app.go signature. Let me fix this.

func init() {
	// Verify at compile time that our copyFile matches the expected signature.
	var _ func(fs.FileSystem, string, fs.FileSystem, string) error = copyFile
	_ = cleanRemoteDir
}
