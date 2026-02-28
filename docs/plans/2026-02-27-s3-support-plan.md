# S3 Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add S3 as a remote backend with full feature parity to SFTP, letting users browse buckets, transfer files, sync directories, and edit remote files through the dual-pane TUI.

**Architecture:** Implement `S3FS` behind the existing `FileSystem` interface (`internal/fs/fs.go`). Add an `internal/s3/` package for AWS client setup and bucket discovery. Extend the picker and CLI to support `s3://` connection targets. The transfer engine and sync/diff view need no changes — they already operate on the `FileSystem` interface.

**Tech Stack:** Go 1.25, AWS SDK v2 (`aws-sdk-go-v2`), Bubble Tea TUI framework

**Design doc:** `docs/plans/2026-02-27-s3-support-design.md`

---

### Task 1: Add AWS SDK Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add AWS SDK modules**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/service/s3
go get github.com/aws/aws-sdk-go-v2/feature/s3/manager
go get github.com/aws/aws-sdk-go-v2/aws
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: Clean build, no errors.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add AWS SDK v2 dependencies for S3 support"
```

---

### Task 2: Implement S3 Client Setup (`internal/s3/`)

**Files:**
- Create: `internal/s3/client.go`
- Create: `internal/s3/buckets.go`

**Step 1: Write client.go**

```go
// internal/s3/client.go
package s3util

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ConnectResult holds the S3 client and parsed connection details.
type ConnectResult struct {
	Client *s3.Client
	Bucket string
	Prefix string // optional prefix within the bucket (e.g., "uploads/")
	Region string
}

// ParseS3URI parses an "s3://bucket/prefix" string into bucket and prefix.
// Returns bucket, prefix. Prefix may be empty.
func ParseS3URI(uri string) (bucket, prefix string, err error) {
	uri = strings.TrimPrefix(uri, "s3://")
	if uri == "" {
		return "", "", fmt.Errorf("empty S3 URI")
	}
	parts := strings.SplitN(uri, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
		// Ensure prefix ends with "/" if non-empty (it's a path prefix).
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}
	return bucket, prefix, nil
}

// Connect creates an S3 client using the default AWS credential chain.
// Region is auto-detected from AWS config if empty.
func Connect(ctx context.Context, bucket, region string) (*ConnectResult, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)

	// If no region was specified, detect the bucket's region.
	if region == "" {
		region = cfg.Region
		if region == "" {
			region = "us-east-1" // fallback
		}
		loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
			Bucket: aws.String(bucket),
		})
		if err == nil && loc.LocationConstraint != "" {
			region = string(loc.LocationConstraint)
			// Re-create client with correct region.
			cfg.Region = region
			client = s3.NewFromConfig(cfg)
		}
	}

	return &ConnectResult{
		Client: client,
		Bucket: bucket,
		Region: region,
	}, nil
}

// HasCredentials returns true if AWS credentials are available.
func HasCredentials(ctx context.Context) bool {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return false
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return false
	}
	return creds.HasKeys()
}
```

**Step 2: Write buckets.go**

```go
// internal/s3/buckets.go
package s3util

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BucketEntry represents an S3 bucket for the picker.
type BucketEntry struct {
	Name   string
	Region string
}

// ListBuckets returns available S3 buckets. Returns nil, nil if no credentials.
func ListBuckets(ctx context.Context) ([]BucketEntry, error) {
	if !HasCredentials(ctx) {
		return nil, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, nil
	}

	client := s3.NewFromConfig(cfg)
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, nil // fail silently
	}

	entries := make([]BucketEntry, 0, len(result.Buckets))
	for _, b := range result.Buckets {
		name := ""
		if b.Name != nil {
			name = *b.Name
		}
		entries = append(entries, BucketEntry{
			Name: name,
		})
	}
	return entries, nil
}
```

**Step 3: Verify build**

Run: `go build ./internal/s3/...`
Expected: Clean build.

**Step 4: Commit**

```bash
git add internal/s3/
git commit -m "feat: add S3 client setup and bucket discovery"
```

---

### Task 3: Implement S3FS (`internal/fs/s3.go`)

This is the core task. Implement all 10 `FileSystem` interface methods.

**Files:**
- Create: `internal/fs/s3.go`

**Step 1: Write the S3FS implementation**

```go
// internal/fs/s3.go
package fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const mtimeMetadataKey = "ferry-mtime"

// S3FS implements FileSystem for Amazon S3 buckets.
type S3FS struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string // optional root prefix (e.g., "uploads/")
}

// NewS3FS creates a new S3-backed FileSystem.
// prefix is an optional path prefix to use as the virtual root.
func NewS3FS(client *s3.Client, bucket, prefix string) *S3FS {
	return &S3FS{
		client:   client,
		uploader: manager.NewUploader(client),
		bucket:   bucket,
		prefix:   prefix,
	}
}

func (s *S3FS) List(p string) ([]Entry, error) {
	fullPrefix := s.toKey(p)
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}

	var entries []Entry
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Prefix:    aws.String(fullPrefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}

		// CommonPrefixes = virtual directories
		for _, cp := range page.CommonPrefixes {
			dirName := path.Base(strings.TrimSuffix(*cp.Prefix, "/"))
			entries = append(entries, Entry{
				Name:  dirName,
				Path:  s.fromKey(strings.TrimSuffix(*cp.Prefix, "/")),
				IsDir: true,
				Mode:  os.ModeDir | 0o755,
			})
		}

		// Contents = files
		for _, obj := range page.Contents {
			key := *obj.Key
			// Skip the prefix directory marker itself.
			if key == fullPrefix {
				continue
			}
			// Skip directory markers (zero-byte objects ending in "/").
			if strings.HasSuffix(key, "/") && obj.Size != nil && *obj.Size == 0 {
				continue
			}

			name := path.Base(key)
			mtime := s.objectMtime(obj.LastModified, nil)

			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}

			entries = append(entries, Entry{
				Name:    name,
				Path:    s.fromKey(key),
				Size:    size,
				ModTime: mtime,
				Mode:    0o644,
				IsDir:   false,
			})
		}
	}

	return entries, nil
}

func (s *S3FS) Stat(p string) (Entry, error) {
	key := s.toKey(p)

	// Try as file first.
	head, err := s.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		mtime := s.objectMtime(head.LastModified, head.Metadata)
		var size int64
		if head.ContentLength != nil {
			size = *head.ContentLength
		}
		return Entry{
			Name:    path.Base(key),
			Path:    p,
			Size:    size,
			ModTime: mtime,
			Mode:    0o644,
			IsDir:   false,
		}, nil
	}

	// Try as directory prefix.
	listPrefix := key
	if !strings.HasSuffix(listPrefix, "/") {
		listPrefix += "/"
	}
	result, err := s.client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(listPrefix),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return Entry{}, fmt.Errorf("s3 stat: %w", err)
	}
	if result.KeyCount != nil && *result.KeyCount > 0 {
		return Entry{
			Name:  path.Base(key),
			Path:  p,
			IsDir: true,
			Mode:  os.ModeDir | 0o755,
		}, nil
	}

	return Entry{}, fmt.Errorf("s3 stat: %s not found", p)
}

func (s *S3FS) Read(p string, w io.Writer) error {
	key := s.toKey(p)
	result, err := s.client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 read: %w", err)
	}
	defer result.Body.Close()
	_, err = io.Copy(w, result.Body)
	return err
}

func (s *S3FS) Write(p string, r io.Reader, perm os.FileMode) error {
	key := s.toKey(p)

	// Use the upload manager which handles multipart for large files.
	_, err := s.uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	if err != nil {
		return fmt.Errorf("s3 write: %w", err)
	}
	return nil
}

func (s *S3FS) Mkdir(p string, perm os.FileMode) error {
	key := s.toKey(p)
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}

	_, err := s.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(nil),
		ContentLength: aws.Int64(0),
	})
	if err != nil {
		return fmt.Errorf("s3 mkdir: %w", err)
	}
	return nil
}

func (s *S3FS) Remove(p string) error {
	key := s.toKey(p)

	// Try deleting as a single object first.
	_, err := s.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	// Also delete all objects under this prefix (recursive directory delete).
	prefix := key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, pageErr := paginator.NextPage(context.TODO())
		if pageErr != nil {
			break
		}
		if len(page.Contents) == 0 {
			break
		}

		// Batch delete up to 1000 objects at a time.
		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: obj.Key})
		}
		_, err = s.client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("s3 remove batch: %w", err)
		}
	}

	return nil
}

func (s *S3FS) Rename(oldPath, newPath string) error {
	oldKey := s.toKey(oldPath)
	newKey := s.toKey(newPath)

	// S3 has no native rename. Copy then delete.
	_, err := s.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		CopySource: aws.String(s.bucket + "/" + oldKey),
		Key:        aws.String(newKey),
	})
	if err != nil {
		return fmt.Errorf("s3 rename (copy): %w", err)
	}

	_, err = s.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		return fmt.Errorf("s3 rename (delete old): %w", err)
	}
	return nil
}

func (s *S3FS) Chmod(path string, perm os.FileMode) error {
	// S3 has no concept of Unix permissions. No-op.
	return nil
}

func (s *S3FS) Chtimes(p string, mtime time.Time) error {
	key := s.toKey(p)

	// S3 doesn't support setting mtime directly.
	// Store it as custom metadata by copying the object to itself.
	_, err := s.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		CopySource:        aws.String(s.bucket + "/" + key),
		Key:               aws.String(key),
		MetadataDirective: types.MetadataDirectiveReplace,
		Metadata: map[string]string{
			mtimeMetadataKey: strconv.FormatInt(mtime.Unix(), 10),
		},
	})
	if err != nil {
		return fmt.Errorf("s3 chtimes: %w", err)
	}
	return nil
}

func (s *S3FS) HomeDir() (string, error) {
	return "/", nil
}

// toKey converts a FileSystem path to an S3 object key, prepending the prefix.
func (s *S3FS) toKey(p string) string {
	// Normalize: remove leading "/" since S3 keys don't start with "/".
	p = strings.TrimPrefix(p, "/")
	if s.prefix == "" {
		return p
	}
	if p == "" {
		return strings.TrimSuffix(s.prefix, "/")
	}
	return s.prefix + p
}

// fromKey converts an S3 object key back to a FileSystem path, stripping the prefix.
func (s *S3FS) fromKey(key string) string {
	key = strings.TrimPrefix(key, s.prefix)
	if key == "" {
		return "/"
	}
	return "/" + key
}

// objectMtime returns the best mtime for an object: custom metadata first, then LastModified.
func (s *S3FS) objectMtime(lastModified *time.Time, metadata map[string]string) time.Time {
	if metadata != nil {
		if raw, ok := metadata[mtimeMetadataKey]; ok {
			if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return time.Unix(unix, 0)
			}
		}
	}
	if lastModified != nil {
		return *lastModified
	}
	return time.Time{}
}
```

**Step 2: Verify build**

Run: `go build ./internal/fs/...`
Expected: Clean build.

**Step 3: Commit**

```bash
git add internal/fs/s3.go
git commit -m "feat: implement S3FS filesystem backend"
```

---

### Task 4: Write S3FS Unit Tests

**Files:**
- Create: `internal/fs/s3_test.go`

Write tests using a mock/stub S3 client to verify path conversion logic and the interface contract. Focus on the path normalization (`toKey`/`fromKey`) and mtime parsing since those are the trickiest parts. Integration tests against real S3 can come later.

**Step 1: Write tests**

```go
// internal/fs/s3_test.go
package fs

import (
	"testing"
	"time"
)

func TestS3FS_toKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{"root no prefix", "", "/", ""},
		{"file no prefix", "", "/docs/readme.md", "docs/readme.md"},
		{"root with prefix", "data/", "/", "data"},
		{"file with prefix", "data/", "/docs/readme.md", "data/docs/readme.md"},
		{"no leading slash", "", "docs/readme.md", "docs/readme.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S3FS{prefix: tt.prefix}
			got := s.toKey(tt.path)
			if got != tt.want {
				t.Errorf("toKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestS3FS_fromKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"file no prefix", "", "docs/readme.md", "/docs/readme.md"},
		{"file with prefix", "data/", "data/docs/readme.md", "/docs/readme.md"},
		{"root", "", "", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S3FS{prefix: tt.prefix}
			got := s.fromKey(tt.key)
			if got != tt.want {
				t.Errorf("fromKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestS3FS_objectMtime(t *testing.T) {
	s := &S3FS{}
	now := time.Now()

	// Custom metadata takes precedence.
	meta := map[string]string{mtimeMetadataKey: "1700000000"}
	got := s.objectMtime(&now, meta)
	if got.Unix() != 1700000000 {
		t.Errorf("expected unix 1700000000, got %d", got.Unix())
	}

	// Falls back to LastModified.
	got = s.objectMtime(&now, nil)
	if !got.Equal(now) {
		t.Errorf("expected %v, got %v", now, got)
	}

	// Returns zero time when no info.
	got = s.objectMtime(nil, nil)
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/fs/ -v -run TestS3FS`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/fs/s3_test.go
git commit -m "test: add S3FS path conversion and mtime unit tests"
```

---

### Task 5: Generalize Picker for Multi-Backend Support

The picker currently only shows SSH hosts. Generalize it to show both SSH hosts and S3 buckets with a unified `ConnectionTarget` type.

**Files:**
- Modify: `internal/ui/picker/picker.go`

**Step 1: Add ConnectionTarget type and update picker**

Add a `ConnectionTarget` struct and a new `TargetSelected` message type. Keep the existing `HostSelected` temporarily for backwards compat, but the picker will now emit `TargetSelected`.

Key changes:
- Add `ConnectionTarget` with `Type` field (`"ssh"` or `"s3"`)
- Add `s3Buckets []s3util.BucketEntry` field to `Model`
- New constructor: `NewWithBuckets(hosts, buckets)` alongside existing `New(hosts)`
- Replace `HostSelected` message with `TargetSelected{Target ConnectionTarget}`
- Render S3 buckets below SSH hosts with a divider
- Fuzzy search covers both host names and bucket names
- Typing `s3://bucket` in the input creates an S3 target directly

The `ConnectionTarget` type:

```go
type ConnectionTarget struct {
	Type   string // "ssh" or "s3"
	Host   string // SSH host (for ssh type)
	Bucket string // S3 bucket name (for s3 type)
	Prefix string // S3 prefix (for s3 type)
}
```

The `TargetSelected` message replaces `HostSelected`:

```go
type TargetSelected struct {
	Target ConnectionTarget
}
```

Keep `HostSelected` as a type alias or remove it — the app will be updated in Task 7 to use `TargetSelected`.

**Step 2: Verify build**

Run: `go build ./internal/ui/picker/...`
Expected: Clean build.

**Step 3: Commit**

```bash
git add internal/ui/picker/picker.go
git commit -m "feat: generalize picker for SSH and S3 connection targets"
```

---

### Task 6: Update CLI to Parse s3:// URIs

**Files:**
- Modify: `cmd/ferry/main.go`
- Modify: `internal/app/app.go` (Options struct only)

**Step 1: Update Options to support S3**

In `internal/app/app.go`, extend `Options`:

```go
type Options struct {
	Host   string // If set, skip picker and connect to SSH host
	S3URI  string // If set, skip picker and connect to S3 (e.g., "s3://bucket/prefix")
}
```

**Step 2: Update main.go to detect s3:// argument**

```go
func main() {
	// ... existing flag parsing ...

	var host string
	if flag.NArg() > 0 {
		host = flag.Arg(0)
	}

	opts := app.Options{}
	if strings.HasPrefix(host, "s3://") {
		opts.S3URI = host
	} else {
		opts.Host = host
	}

	// ... rest unchanged ...
}
```

Add `"strings"` to main.go imports.

**Step 3: Verify build**

Run: `go build ./cmd/ferry/...`
Expected: Clean build.

**Step 4: Commit**

```bash
git add cmd/ferry/main.go internal/app/app.go
git commit -m "feat: parse s3:// URIs from CLI arguments"
```

---

### Task 7: Integrate S3 Backend into App State Machine

This is the largest integration task. Wire S3 connections through the app's state machine.

**Files:**
- Modify: `internal/app/app.go`

**Step 1: Add S3 connection state**

Add fields to `Model`:

```go
type Model struct {
	// ... existing fields ...

	// S3 backend (nil when using SSH)
	s3Client *s3.Client
	s3Bucket string
	s3Prefix string

	// Backend type for the current connection
	backendType string // "ssh" or "s3"
}
```

Add imports for `s3util` and `context`.

**Step 2: Update NewWithOptions for S3**

In `NewWithOptions`, handle `opts.S3URI`:

```go
if opts.S3URI != "" {
	m.state = stateConnecting
	m.connectHost = opts.S3URI // reuse for display
	m.backendType = "s3"
} else if opts.Host != "" {
	m.state = stateConnecting
	m.connectHost = opts.Host
	m.backendType = "ssh"
}
```

Also pass S3 buckets to the picker:

```go
buckets, _ := s3util.ListBuckets(context.Background())
m.picker = picker.NewWithBuckets(hosts, buckets)
```

**Step 3: Add S3 connection messages**

```go
type s3ConnectSuccessMsg struct {
	client *s3.Client
	bucket string
	prefix string
}

type s3ConnectErrorMsg struct {
	err error
}
```

**Step 4: Add doS3Connect command**

```go
func (m Model) doS3Connect(uri string) tea.Cmd {
	return func() tea.Msg {
		bucket, prefix, err := s3util.ParseS3URI(uri)
		if err != nil {
			return s3ConnectErrorMsg{err: err}
		}
		result, err := s3util.Connect(context.Background(), bucket, "")
		if err != nil {
			return s3ConnectErrorMsg{err: err}
		}
		return s3ConnectSuccessMsg{
			client: result.Client,
			bucket: result.Bucket,
			prefix: prefix,
		}
	}
}
```

**Step 5: Update Init() to handle S3**

```go
func (m Model) Init() tea.Cmd {
	if m.state == stateConnecting {
		if m.backendType == "s3" {
			return tea.Batch(m.spinner.Tick, m.doS3Connect(m.connectHost))
		}
		return tea.Batch(m.spinner.Tick, m.doConnect(m.connectHost))
	}
	return tea.Batch(m.picker.Init(), m.spinner.Tick)
}
```

**Step 6: Handle S3 connect results in Update()**

Add cases in the top-level `Update` switch:

```go
case s3ConnectSuccessMsg:
	m.s3Client = msg.client
	m.s3Bucket = msg.bucket
	m.s3Prefix = msg.prefix
	m.backendType = "s3"
	m.state = stateBrowser

	localFS := fs.NewLocalFS()
	remoteFS := fs.NewS3FS(msg.client, msg.bucket, msg.prefix)

	m.localPane = pane.New(localFS, "Local")
	m.remotePane = pane.New(remoteFS, "S3")
	m.activePane = 0
	m.localPane.SetActive(true)
	m.remotePane.SetActive(false)

	connInfo := fmt.Sprintf("s3://%s", msg.bucket)
	if msg.prefix != "" {
		connInfo += "/" + strings.TrimSuffix(msg.prefix, "/")
	}
	m.statusBar.SetConnection(connInfo)
	m.setPaneSizes()

	return m, tea.Batch(m.localPane.Init(), m.remotePane.Init())

case s3ConnectErrorMsg:
	m.state = statePicker
	m.err = msg.err
	m.picker.SetError(fmt.Sprintf("S3 connection failed: %v", msg.err))
	return m, nil
```

**Step 7: Update picker handling for TargetSelected**

In `updatePicker`, handle `picker.TargetSelected`:

```go
case picker.TargetSelected:
	m.state = stateConnecting
	m.err = nil
	m.picker.SetError("")
	if msg.Target.Type == "s3" {
		uri := "s3://" + msg.Target.Bucket
		if msg.Target.Prefix != "" {
			uri += "/" + msg.Target.Prefix
		}
		m.connectHost = uri
		m.backendType = "s3"
		return m, tea.Batch(m.spinner.Tick, m.doS3Connect(uri))
	}
	m.connectHost = msg.Target.Host
	m.backendType = "ssh"
	return m, tea.Batch(m.spinner.Tick, m.doConnect(msg.Target.Host))
```

**Step 8: Guard SSH-only features**

Update the reconnect handler and sync rsync path to check `m.backendType`:

- In the `R` key handler: only reconnect if `m.backendType == "ssh"`
- In `startSync`: skip rsync check if `m.backendType != "ssh"` (set `hasRsync = false`)
- In quit handler: only call `m.conn.Close()` if `m.conn != nil`

**Step 9: Verify build**

Run: `go build ./...`
Expected: Clean build.

**Step 10: Commit**

```bash
git add internal/app/app.go
git commit -m "feat: integrate S3 backend into app state machine"
```

---

### Task 8: End-to-End Manual Testing

No automated integration tests here — this requires real AWS credentials and an S3 bucket. Test manually.

**Test matrix:**

1. `ferry s3://your-test-bucket` — should connect and show bucket contents
2. Browse directories in S3 pane (Enter to navigate, Backspace to go up)
3. `yy` on local files, `p` in S3 pane — upload files
4. `yy` on S3 files, `p` in local pane — download files
5. `D` in S3 pane — create directory (marker object)
6. `dd` on S3 file — delete with confirmation
7. `r` on S3 file — rename (copy + delete)
8. `e` on S3 file — edit (download, edit, upload)
9. `S` — sync view between local and S3
10. Picker shows S3 buckets when AWS credentials are present
11. Picker hides S3 section when no credentials
12. `ferry` with no args — picker shows both SSH and S3

**Step 1: Test and fix issues**

Run through the test matrix. Fix any bugs found.

**Step 2: Commit fixes**

```bash
git add -u
git commit -m "fix: address S3 integration issues from manual testing"
```

---

### Task 9: Update Help Overlay and README

**Files:**
- Modify: `internal/ui/modal/help.go` (if S3-specific hints needed)
- Modify: `README.md`

**Step 1: Update README**

Add S3 to the feature list and usage examples:

```markdown
## S3 Support

Ferry can browse and transfer files to/from Amazon S3 buckets:

```
ferry s3://my-bucket           # Connect to S3 bucket
ferry s3://my-bucket/prefix    # Connect to specific prefix
```

Uses the standard AWS credential chain (env vars, ~/.aws/credentials, IAM roles).
S3 buckets also appear in the connection picker when AWS credentials are detected.
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add S3 support to README"
```
