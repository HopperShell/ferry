//go:build integration

package fs_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	s3util "github.com/HopperShell/ferry/internal/s3"

	"github.com/HopperShell/ferry/internal/fs"
)

// Run with: AWS_ENDPOINT_URL=http://localhost:9000 go test ./internal/fs/ -tags integration -v -run TestS3FS_Integration
// Requires MinIO running on localhost:9000 with test-bucket created.
func TestS3FS_Integration(t *testing.T) {
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("AWS_ENDPOINT_URL not set, skipping integration test")
	}

	ctx := context.Background()
	result, err := s3util.Connect(ctx, "test-bucket", "us-east-1", "")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	s3fs := fs.NewS3FS(result.Client, "test-bucket", "")

	// Test HomeDir
	home, err := s3fs.HomeDir()
	if err != nil || home != "/" {
		t.Errorf("HomeDir: got %q, %v", home, err)
	}

	// Test List root
	entries, err := s3fs.List("/")
	if err != nil {
		t.Fatalf("List /: %v", err)
	}
	t.Logf("Root entries: %d", len(entries))
	for _, e := range entries {
		t.Logf("  %s (dir=%v, size=%d)", e.Path, e.IsDir, e.Size)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries in test-bucket root")
	}

	// Test Stat file
	stat, err := s3fs.Stat("/hello.txt")
	if err != nil {
		t.Fatalf("Stat hello.txt: %v", err)
	}
	t.Logf("Stat hello.txt: size=%d, name=%s", stat.Size, stat.Name)

	// Test Read
	var buf bytes.Buffer
	err = s3fs.Read("/hello.txt", &buf)
	if err != nil {
		t.Fatalf("Read hello.txt: %v", err)
	}
	t.Logf("Read hello.txt: %q", buf.String())
	if buf.String() != "hello from ferry\n" {
		t.Errorf("unexpected content: %q", buf.String())
	}

	// Test Write
	err = s3fs.Write("/test-upload.txt", bytes.NewReader([]byte("uploaded by test")), 0644)
	if err != nil {
		t.Fatalf("Write test-upload.txt: %v", err)
	}

	// Verify the write
	buf.Reset()
	err = s3fs.Read("/test-upload.txt", &buf)
	if err != nil {
		t.Fatalf("Read test-upload.txt: %v", err)
	}
	if buf.String() != "uploaded by test" {
		t.Errorf("unexpected content after write: %q", buf.String())
	}

	// Test Mkdir
	err = s3fs.Mkdir("/testdir", 0755)
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	// Test Rename
	err = s3fs.Rename("/test-upload.txt", "/test-renamed.txt")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	buf.Reset()
	err = s3fs.Read("/test-renamed.txt", &buf)
	if err != nil {
		t.Fatalf("Read after rename: %v", err)
	}
	if buf.String() != "uploaded by test" {
		t.Errorf("content changed after rename: %q", buf.String())
	}

	// Test Chtimes
	mtime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	err = s3fs.Chtimes("/test-renamed.txt", mtime)
	if err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	stat, err = s3fs.Stat("/test-renamed.txt")
	if err != nil {
		t.Fatalf("Stat after chtimes: %v", err)
	}
	if stat.ModTime.Unix() != mtime.Unix() {
		t.Errorf("mtime not preserved: got %v, want %v", stat.ModTime, mtime)
	}

	// Test Chmod (no-op, should not error)
	err = s3fs.Chmod("/test-renamed.txt", 0600)
	if err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	// Test Remove
	err = s3fs.Remove("/test-renamed.txt")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err = s3fs.Stat("/test-renamed.txt")
	if err == nil {
		t.Error("expected error after remove, got nil")
	}

	// Test List subdirectory
	entries, err = s3fs.List("/subdir")
	if err != nil {
		t.Fatalf("List /subdir: %v", err)
	}
	t.Logf("Subdir entries: %d", len(entries))
	for _, e := range entries {
		t.Logf("  %s (dir=%v, size=%d)", e.Path, e.IsDir, e.Size)
	}

	// Cleanup
	_ = s3fs.Remove("/testdir")

	t.Log("All integration tests passed!")
}
