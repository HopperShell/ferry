# S3 Support Design

## Goal

Add S3 as a remote backend with full feature parity to SFTP. Users can browse S3 buckets in the right pane, transfer files between local and S3, sync directories, and edit remote files — all through the existing dual-pane TUI.

## Approach

Direct AWS SDK v2 implementation behind the existing `FileSystem` interface. No abstraction layers, no FUSE. Consistent with how SFTP is implemented.

## Architecture

### 1. S3FS (`internal/fs/s3.go`)

New `FileSystem` implementation wrapping `*s3.Client`.

```go
type S3FS struct {
    client *s3.Client
    bucket string
}

func NewS3FS(client *s3.Client, bucket string) *S3FS
```

**Interface method mapping:**

| Method | S3 Implementation | Notes |
|--------|-------------------|-------|
| `List(path)` | `ListObjectsV2` with `Delimiter: "/"` | CommonPrefixes = dirs, Contents = files |
| `Stat(path)` | `HeadObject` | Synthesize Entry for directory prefixes |
| `Read(path, w)` | `GetObject` → `io.Copy` | Streaming |
| `Write(path, r, perm)` | Upload manager (`manager.Uploader`) | Handles multipart for large files |
| `Mkdir(path, perm)` | Put zero-byte object at `path/` | Standard S3 directory marker convention |
| `Remove(path)` | `DeleteObject` / `DeleteObjects` batch | Recursive delete for prefixes |
| `Rename(old, new)` | `CopyObject` + `DeleteObject` | S3 has no native rename |
| `Chmod(path, perm)` | No-op | S3 ACLs don't map to Unix permissions |
| `Chtimes(path, mtime)` | `CopyObject` to self with `x-amz-meta-mtime` | Standard metadata update pattern |
| `HomeDir()` | Returns `"/"` | Bucket root |

**S3 semantics handled internally:**
- Virtual directories via prefix + delimiter listing
- Pagination for large listings (>1000 objects)
- Multipart uploads for large files via upload manager
- Custom metadata `x-amz-meta-mtime` for modification time preservation, falling back to `LastModified`
- Batch delete for recursive directory removal

### 2. AWS Connection Layer (`internal/s3/`)

**`client.go`** — Client setup:
- `Connect(bucket, region string) (*s3.Client, error)`
- Uses default AWS credential chain: env vars → `~/.aws/credentials` → `~/.aws/config` → IAM role
- Region auto-detected from AWS config if not specified

**`buckets.go`** — Bucket discovery:
- `ListBuckets(ctx) ([]BucketEntry, error)`
- Called by picker to show available buckets
- Fails silently if no AWS credentials detected

No persistent connection management needed — S3 is stateless HTTP.

### 3. CLI Integration (`cmd/ferry/main.go`)

New connection syntax via `s3://` prefix:

```
ferry                         Launch picker (SSH hosts + S3 buckets)
ferry <host>                  Connect to SSH host
ferry s3://<bucket>           Connect to S3 bucket root
ferry s3://<bucket>/prefix    Connect to S3 bucket at prefix
```

No new flags. The `s3://` prefix distinguishes the backend.

### 4. Picker Integration (`internal/ui/picker/`)

- Generalize picker to accept connection targets (SSH hosts + S3 buckets)
- S3 buckets listed below SSH hosts with a visual divider
- Each bucket shown as `s3://bucket-name (us-east-1)`
- Fuzzy search works across both types
- Typing `s3://my-bucket` directly works like typing a custom SSH host
- S3 section hidden when no AWS credentials detected

### 5. App State Machine (`internal/app/app.go`)

- Picker returns a `ConnectionTarget` type instead of a host string
- `stateConnecting` handles both SSH and S3 — creates appropriate `FileSystem`
- Model stores backend-agnostic connection info; remote pane receives whichever `FileSystem` is created
- Status bar shows `s3://bucket-name` for S3 connections

### 6. Transfer & Sync

**No changes needed to the transfer engine or sync/diff view.** They already operate on the `FileSystem` interface.

- Resumable transfers work via size + mtime matching (mtime from custom metadata)
- rsync fast-path not available for S3; always uses file-by-file comparison (existing fallback)
- Large uploads handled transparently by the upload manager inside `S3FS.Write`

### 7. Edge Cases

- **Empty directories:** `Mkdir` creates marker objects to preserve structure
- **Permissions:** `Chmod` is no-op; files transferred from S3 get default 0644/0755
- **Symlinks:** Not applicable on S3, skipped
- **Rename:** Copy + delete pattern (can be slow for large files)
- **Remote editing:** Works via download/edit/upload. No shadow-copy conflict detection; could use ETags for optimistic concurrency in the future
- **Reconnect:** Not needed (stateless HTTP), but could recreate client on auth errors

## New Dependencies

- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/service/s3`
- `github.com/aws/aws-sdk-go-v2/feature/s3/manager` (multipart uploads)

## Files to Create/Modify

**New:**
- `internal/fs/s3.go` — S3FS implementation
- `internal/fs/s3_test.go` — Unit tests
- `internal/s3/client.go` — AWS client setup
- `internal/s3/buckets.go` — Bucket discovery

**Modify:**
- `cmd/ferry/main.go` — Parse `s3://` argument
- `internal/app/app.go` — Backend-agnostic connection flow, ConnectionTarget type
- `internal/ui/picker/picker.go` — Generalize to show SSH + S3 targets
- `internal/ui/statusbar/statusbar.go` — Display S3 connection info
- `go.mod` — Add AWS SDK dependencies
