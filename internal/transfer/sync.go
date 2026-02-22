// internal/transfer/sync.go
package transfer

import (
	"bufio"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"time"

	"github.com/andrewstuart/ferry/internal/fs"
	"golang.org/x/crypto/ssh"
)

// DiffStatus describes the comparison result for a single entry.
type DiffStatus int

const (
	DiffSame       DiffStatus = iota // identical on both sides
	DiffLocalOnly                    // exists only locally
	DiffRemoteOnly                   // exists only on remote
	DiffModified                     // exists on both but differs
)

// DiffEntry represents one item in a unified diff listing.
type DiffEntry struct {
	Name        string     // base name
	RelPath     string     // relative path from sync root
	LocalEntry  *fs.Entry  // nil if remote only
	RemoteEntry *fs.Entry  // nil if local only
	Status      DiffStatus //
	IsDir       bool       //
	NewerSide   string     // "local" or "remote" for modified entries
}

// Compare walks both FileSystems from the given root paths and returns a flat
// list of DiffEntries representing the unified view.
func Compare(localFS fs.FileSystem, localRoot string, remoteFS fs.FileSystem, remoteRoot string) ([]DiffEntry, error) {
	localMap, err := walkFS(localFS, localRoot, "")
	if err != nil {
		return nil, fmt.Errorf("walk local: %w", err)
	}

	remoteMap, err := walkFS(remoteFS, remoteRoot, "")
	if err != nil {
		return nil, fmt.Errorf("walk remote: %w", err)
	}

	// Collect all unique relative paths.
	allPaths := make(map[string]struct{})
	for k := range localMap {
		allPaths[k] = struct{}{}
	}
	for k := range remoteMap {
		allPaths[k] = struct{}{}
	}

	var entries []DiffEntry
	for rel := range allPaths {
		local, hasLocal := localMap[rel]
		remote, hasRemote := remoteMap[rel]

		de := DiffEntry{
			Name:    path.Base(rel),
			RelPath: rel,
		}

		switch {
		case hasLocal && !hasRemote:
			de.Status = DiffLocalOnly
			de.IsDir = local.IsDir
			de.LocalEntry = &local

		case !hasLocal && hasRemote:
			de.Status = DiffRemoteOnly
			de.IsDir = remote.IsDir
			de.RemoteEntry = &remote

		default: // both exist
			de.LocalEntry = &local
			de.RemoteEntry = &remote
			de.IsDir = local.IsDir || remote.IsDir

			if local.IsDir && remote.IsDir {
				// Directories are considered the same.
				de.Status = DiffSame
			} else if entriesMatch(local, remote) {
				de.Status = DiffSame
			} else {
				de.Status = DiffModified
				if local.ModTime.After(remote.ModTime) {
					de.NewerSide = "local"
				} else {
					de.NewerSide = "remote"
				}
			}
		}

		entries = append(entries, de)
	}

	// Sort: directories first, then alphabetical by relative path.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].RelPath < entries[j].RelPath
	})

	return entries, nil
}

// entriesMatch returns true if two entries are considered identical (same size
// and modification time within a 2-second tolerance).
func entriesMatch(a, b fs.Entry) bool {
	if a.Size != b.Size {
		return false
	}
	diff := a.ModTime.Sub(b.ModTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 2*time.Second
}

// walkFS recursively lists a filesystem tree, building a map from relative
// path to Entry.
func walkFS(filesystem fs.FileSystem, root string, prefix string) (map[string]fs.Entry, error) {
	result := make(map[string]fs.Entry)

	entries, err := filesystem.List(root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		rel := path.Join(prefix, entry.Name)
		result[rel] = entry

		if entry.IsDir {
			subMap, err := walkFS(filesystem, entry.Path, rel)
			if err != nil {
				// Skip directories we can't read; don't fail entirely.
				continue
			}
			for k, v := range subMap {
				result[k] = v
			}
		}
	}

	return result, nil
}

// HasRsync checks if rsync is available on the remote host via SSH.
func HasRsync(sshClient *ssh.Client) bool {
	session, err := sshClient.NewSession()
	if err != nil {
		return false
	}
	defer session.Close()

	err = session.Run("which rsync")
	return err == nil
}

// RsyncPush syncs local->remote using rsync over SSH.
// Progress lines from rsync stdout are sent to the progress channel.
// The channel is closed when rsync completes.
func RsyncPush(localPath, remotePath, host string, progress chan<- string) error {
	defer close(progress)

	cmd := exec.Command("rsync", "-avz", "--progress",
		"-e", "ssh",
		localPath+"/",
		fmt.Sprintf("%s:%s/", host, remotePath),
	)

	return runRsync(cmd, progress)
}

// RsyncPull syncs remote->local using rsync over SSH.
// Progress lines from rsync stdout are sent to the progress channel.
// The channel is closed when rsync completes.
func RsyncPull(remotePath, localPath, host string, progress chan<- string) error {
	defer close(progress)

	cmd := exec.Command("rsync", "-avz", "--progress",
		"-e", "ssh",
		fmt.Sprintf("%s:%s/", host, remotePath),
		localPath+"/",
	)

	return runRsync(cmd, progress)
}

// runRsync executes an rsync command, streaming stdout lines to the progress channel.
func runRsync(cmd *exec.Cmd, progress chan<- string) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("rsync stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rsync start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		progress <- scanner.Text()
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rsync: %w", err)
	}
	return nil
}
