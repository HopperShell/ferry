package fs

import (
	"errors"
	"io"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var errNoClient = errors.New("sftp: not connected")

type RemoteFS struct {
	client *sftp.Client
}

func NewRemoteFS(sshClient *ssh.Client) (*RemoteFS, error) {
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, err
	}
	return &RemoteFS{client: client}, nil
}

func (r *RemoteFS) List(path string) ([]Entry, error) {
	if r.client == nil {
		return nil, errNoClient
	}
	infos, err := r.client.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, remoteEntryFromFileInfo(path+"/"+info.Name(), info))
	}
	return entries, nil
}

func (r *RemoteFS) Stat(path string) (Entry, error) {
	if r.client == nil {
		return Entry{}, errNoClient
	}
	info, err := r.client.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return remoteEntryFromFileInfo(path, info), nil
}

func (r *RemoteFS) Read(path string, w io.Writer) error {
	if r.client == nil {
		return errNoClient
	}
	f, err := r.client.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func (r *RemoteFS) Write(path string, rd io.Reader, perm os.FileMode) error {
	if r.client == nil {
		return errNoClient
	}
	f, err := r.client.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, rd); err != nil {
		return err
	}
	return r.client.Chmod(path, perm)
}

func (r *RemoteFS) Mkdir(path string, perm os.FileMode) error {
	if r.client == nil {
		return errNoClient
	}
	return r.client.MkdirAll(path)
}

func (r *RemoteFS) Remove(path string) error {
	if r.client == nil {
		return errNoClient
	}
	info, err := r.client.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return r.removeAll(path)
	}
	return r.client.Remove(path)
}

func (r *RemoteFS) removeAll(path string) error {
	entries, err := r.client.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := path + "/" + entry.Name()
		if entry.IsDir() {
			if err := r.removeAll(child); err != nil {
				return err
			}
		} else {
			if err := r.client.Remove(child); err != nil {
				return err
			}
		}
	}
	return r.client.RemoveDirectory(path)
}

func (r *RemoteFS) Rename(old, new string) error {
	if r.client == nil {
		return errNoClient
	}
	return r.client.Rename(old, new)
}

func (r *RemoteFS) Chmod(path string, perm os.FileMode) error {
	if r.client == nil {
		return errNoClient
	}
	return r.client.Chmod(path, perm)
}

func (r *RemoteFS) Chtimes(path string, mtime time.Time) error {
	if r.client == nil {
		return errNoClient
	}
	return r.client.Chtimes(path, mtime, mtime)
}

func (r *RemoteFS) HomeDir() (string, error) {
	if r.client == nil {
		return "", errNoClient
	}
	return r.client.Getwd()
}

func (r *RemoteFS) Close() error {
	return r.client.Close()
}

func remoteEntryFromFileInfo(path string, info os.FileInfo) Entry {
	e := Entry{
		Name:    info.Name(),
		Path:    path,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		e.Owner = strconv.FormatUint(uint64(stat.Uid), 10)
		e.Group = strconv.FormatUint(uint64(stat.Gid), 10)
	}
	return e
}
