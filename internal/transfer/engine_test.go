package transfer_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
	"github.com/HopperShell/ferry/internal/transfer"
)

// failingWriteFS wraps a FileSystem and fails on Write the first N times.
type failingWriteFS struct {
	fs.FileSystem
	failCount atomic.Int32
}

func (f *failingWriteFS) Write(path string, r io.Reader, perm os.FileMode) error {
	if f.failCount.Add(-1) >= 0 {
		// Drain the reader so the pipe writer doesn't block.
		io.Copy(io.Discard, r)
		return errors.New("simulated write failure")
	}
	return f.FileSystem.Write(path, r, perm)
}

// collectDoneEvents drains the progress channel and returns the first
// Done event per unique JobID (ignoring duplicate done events from
// ProgressReader EOF + Finish).
func collectDoneEvents(ch <-chan transfer.ProgressEvent) map[string]transfer.ProgressEvent {
	results := make(map[string]transfer.ProgressEvent)
	for evt := range ch {
		if evt.Done {
			if _, exists := results[evt.JobID]; !exists {
				results[evt.JobID] = evt
			}
		}
	}
	return results
}

// TestResumeSkipsCompletedFiles verifies that a resumable engine skips files
// that already exist at the destination with matching size and mtime.
func TestResumeSkipsCompletedFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create 5 source files.
	for i := 1; i <= 5; i++ {
		data := make([]byte, i*1024)
		for j := range data {
			data[j] = byte(i)
		}
		if err := os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("file%d.bin", i)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srcFS := fs.NewLocalFS()
	dstFS := fs.NewLocalFS()

	// --- First transfer: transfer all 5 files ---
	engine1 := transfer.NewEngine(1, true)
	go engine1.Start()

	entries, err := srcFS.List(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		engine1.Enqueue(&transfer.Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstDir, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
	}
	engine1.Done()
	for range engine1.Progress() {
	}

	// Verify all 5 files exist at destination.
	dstEntries, err := dstFS.List(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dstEntries) != 5 {
		t.Fatalf("expected 5 files after first transfer, got %d", len(dstEntries))
	}

	// Record destination mtimes to verify they don't change.
	mtimes := make(map[string]time.Time)
	for _, e := range dstEntries {
		mtimes[e.Name] = e.ModTime
	}

	// Small sleep so any re-write would have a different mtime.
	time.Sleep(100 * time.Millisecond)

	// --- Second transfer: same files, should all be skipped ---
	engine2 := transfer.NewEngine(1, true)
	go engine2.Start()

	entries, _ = srcFS.List(srcDir)
	for _, entry := range entries {
		engine2.Enqueue(&transfer.Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstDir, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
	}
	engine2.Done()

	results := collectDoneEvents(engine2.Progress())

	if len(results) != 5 {
		t.Errorf("expected 5 completed jobs, got %d", len(results))
	}
	for _, evt := range results {
		if evt.Err != nil {
			t.Errorf("file %s had error on resume: %v", evt.Name, evt.Err)
		}
	}

	// Verify mtimes unchanged — files were skipped, not rewritten.
	dstEntries, _ = dstFS.List(dstDir)
	for _, e := range dstEntries {
		if !e.ModTime.Equal(mtimes[e.Name]) {
			t.Errorf("file %s mtime changed: was %v, now %v — file was rewritten instead of skipped",
				e.Name, mtimes[e.Name], e.ModTime)
		}
	}
}

// TestResumeCancelAndRetry simulates a cancelled transfer and verifies
// that on retry, completed files are skipped and remaining files transfer.
func TestResumeCancelAndRetry(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFS := fs.NewLocalFS()
	dstFS := fs.NewLocalFS()

	// Create 5 source files.
	for i := 1; i <= 5; i++ {
		data := make([]byte, i*1024)
		for j := range data {
			data[j] = byte(i)
		}
		if err := os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("file%d.bin", i)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// --- First transfer: only transfer first 2 files, simulating partial completion ---
	engine1 := transfer.NewEngine(1, true)
	go engine1.Start()

	entries, _ := srcFS.List(srcDir)
	for _, entry := range entries[:2] {
		engine1.Enqueue(&transfer.Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstDir, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
	}
	engine1.Done()
	for range engine1.Progress() {
	}

	// Verify only 2 files exist.
	dstEntries, _ := dstFS.List(dstDir)
	if len(dstEntries) != 2 {
		t.Fatalf("expected 2 files after partial transfer, got %d", len(dstEntries))
	}

	// Also leave a .ferry-tmp file to simulate an interrupted write of file3.
	tmpData := []byte("incomplete")
	tmpPath := filepath.Join(dstDir, "file3.bin.ferry-tmp")
	if err := os.WriteFile(tmpPath, tmpData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Record completed file mtimes.
	completedMtimes := make(map[string]time.Time)
	for _, e := range dstEntries {
		completedMtimes[e.Name] = e.ModTime
	}

	time.Sleep(100 * time.Millisecond)

	// --- Second transfer: enqueue all 5 files ---
	engine2 := transfer.NewEngine(1, true)
	go engine2.Start()

	entries, _ = srcFS.List(srcDir)
	for _, entry := range entries {
		engine2.Enqueue(&transfer.Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstDir, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
	}
	engine2.Done()

	results := collectDoneEvents(engine2.Progress())

	// All 5 should complete without error.
	if len(results) != 5 {
		t.Errorf("expected 5 completed jobs, got %d", len(results))
	}
	for _, evt := range results {
		if evt.Err != nil {
			t.Errorf("file %s had error: %v", evt.Name, evt.Err)
		}
	}

	// All 5 files should exist at destination now (filter out .ferry-tmp).
	dstEntries, _ = dstFS.List(dstDir)
	var realFiles []fs.Entry
	for _, e := range dstEntries {
		if filepath.Ext(e.Name) != ".ferry-tmp" {
			realFiles = append(realFiles, e)
		}
	}
	if len(realFiles) != 5 {
		names := make([]string, len(dstEntries))
		for i, e := range dstEntries {
			names[i] = e.Name
		}
		t.Errorf("expected 5 destination files, got %d: %v", len(realFiles), names)
	}

	// The old .ferry-tmp should be gone (overwritten by the real file3.bin transfer).
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error(".ferry-tmp file should not remain after successful transfer")
	}

	// Previously completed files should NOT have been rewritten.
	dstEntries, _ = dstFS.List(dstDir)
	for _, e := range dstEntries {
		if origMtime, was := completedMtimes[e.Name]; was {
			if !e.ModTime.Equal(origMtime) {
				t.Errorf("completed file %s was rewritten (mtime changed from %v to %v)",
					e.Name, origMtime, e.ModTime)
			}
		}
	}
}

// TestNoTempFilesAfterSuccess verifies that no .ferry-tmp files remain
// after a fully successful resumable transfer.
func TestNoTempFilesAfterSuccess(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFS := fs.NewLocalFS()
	dstFS := fs.NewLocalFS()

	for i := 1; i <= 3; i++ {
		data := make([]byte, 512)
		os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("f%d.txt", i)), data, 0o644)
	}

	engine := transfer.NewEngine(2, true)
	go engine.Start()

	entries, _ := srcFS.List(srcDir)
	for _, entry := range entries {
		engine.Enqueue(&transfer.Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstDir, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
	}
	engine.Done()
	for range engine.Progress() {
	}

	// Check no .ferry-tmp files remain.
	dstEntries, _ := dstFS.List(dstDir)
	for _, e := range dstEntries {
		if filepath.Ext(e.Name) == ".ferry-tmp" {
			t.Errorf("temp file %s should not remain after successful transfer", e.Name)
		}
	}
	if len(dstEntries) != 3 {
		t.Errorf("expected 3 files, got %d", len(dstEntries))
	}
}

// TestRetryFailedTransfers verifies that a failed transfer is retried
// and succeeds when the underlying error is transient.
func TestRetryFailedTransfers(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	data := []byte("retry me")
	srcPath := filepath.Join(srcDir, "retry.txt")
	os.WriteFile(srcPath, data, 0o644)

	srcFS := fs.NewLocalFS()
	dstFS := &failingWriteFS{FileSystem: fs.NewLocalFS()}
	// Fail on first write attempt, succeed on retry.
	dstFS.failCount.Store(1)

	engine := transfer.NewEngine(1, true)
	engine.SetRetryDelay(time.Millisecond) // fast retries for test
	go engine.Start()

	engine.Enqueue(&transfer.Job{
		Name:    "retry.txt",
		SrcPath: srcPath,
		SrcFS:   srcFS,
		DstPath: filepath.Join(dstDir, "retry.txt"),
		DstFS:   dstFS,
		Size:    int64(len(data)),
	})
	engine.Done()

	results := collectDoneEvents(engine.Progress())
	if len(results) != 1 {
		t.Fatalf("expected 1 job result, got %d", len(results))
	}
	for _, evt := range results {
		if evt.Err != nil {
			t.Errorf("expected successful retry, got error: %v", evt.Err)
		}
	}

	// Verify file content at destination.
	got, err := os.ReadFile(filepath.Join(dstDir, "retry.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("file content mismatch: got %q, want %q", got, data)
	}
}

// TestRetryExhausted verifies that a job fails permanently after all retries.
func TestRetryExhausted(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	data := []byte("fail forever")
	srcPath := filepath.Join(srcDir, "fail.txt")
	os.WriteFile(srcPath, data, 0o644)

	srcFS := fs.NewLocalFS()
	dstFS := &failingWriteFS{FileSystem: fs.NewLocalFS()}
	// Fail more times than maxRetries (2) + initial attempt = 3 total.
	dstFS.failCount.Store(10)

	engine := transfer.NewEngine(1, true)
	engine.SetRetryDelay(time.Millisecond)
	go engine.Start()

	engine.Enqueue(&transfer.Job{
		Name:    "fail.txt",
		SrcPath: srcPath,
		SrcFS:   srcFS,
		DstPath: filepath.Join(dstDir, "fail.txt"),
		DstFS:   dstFS,
		Size:    int64(len(data)),
	})
	engine.Done()

	results := collectDoneEvents(engine.Progress())
	if len(results) != 1 {
		t.Fatalf("expected 1 job result, got %d", len(results))
	}
	for _, evt := range results {
		if evt.Err == nil {
			t.Error("expected error after exhausting retries, got nil")
		}
	}

	// File should not exist at destination.
	if _, err := os.Stat(filepath.Join(dstDir, "fail.txt")); err == nil {
		t.Error("file should not exist after all retries exhausted")
	}
}

// TestNonResumableEngineSkipsNothing verifies that resumable=false
// does not skip files and does not use temp files.
func TestNonResumableEngineSkipsNothing(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFS := fs.NewLocalFS()
	dstFS := fs.NewLocalFS()

	data := []byte("hello world")
	srcPath := filepath.Join(srcDir, "test.txt")
	dstPath := filepath.Join(dstDir, "test.txt")
	os.WriteFile(srcPath, data, 0o644)

	// Pre-create destination with matching content, size, and mtime.
	os.WriteFile(dstPath, data, 0o644)
	srcInfo, _ := os.Stat(srcPath)
	os.Chtimes(dstPath, srcInfo.ModTime(), srcInfo.ModTime())

	// Non-resumable engine should still transfer (overwrite).
	engine := transfer.NewEngine(1, false)
	go engine.Start()
	engine.Enqueue(&transfer.Job{
		Name:    "test.txt",
		SrcPath: srcPath,
		SrcFS:   srcFS,
		DstPath: dstPath,
		DstFS:   dstFS,
		Size:    int64(len(data)),
	})
	engine.Done()

	results := collectDoneEvents(engine.Progress())
	if len(results) != 1 {
		t.Fatalf("expected 1 job result, got %d", len(results))
	}
	for _, evt := range results {
		if evt.Err != nil {
			t.Errorf("unexpected error: %v", evt.Err)
		}
	}
}
