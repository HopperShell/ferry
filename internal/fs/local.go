package fs

import (
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type LocalFS struct{}

func NewLocalFS() *LocalFS { return &LocalFS{} }

func (l *LocalFS) List(path string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, entryFromFileInfo(filepath.Join(path, de.Name()), info))
	}
	return entries, nil
}

func (l *LocalFS) Stat(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return entryFromFileInfo(path, info), nil
}

func (l *LocalFS) Read(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func (l *LocalFS) Write(path string, r io.Reader, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l *LocalFS) Mkdir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (l *LocalFS) Remove(path string) error {
	return os.RemoveAll(path)
}

func (l *LocalFS) Rename(old, new string) error {
	return os.Rename(old, new)
}

func (l *LocalFS) Chmod(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

func (l *LocalFS) Chtimes(path string, mtime time.Time) error {
	return os.Chtimes(path, mtime, mtime)
}

func (l *LocalFS) HomeDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

func entryFromFileInfo(path string, info os.FileInfo) Entry {
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
		if u, err := user.LookupId(e.Owner); err == nil {
			e.Owner = u.Username
		}
		if g, err := user.LookupGroupId(e.Group); err == nil {
			e.Group = g.Name
		}
	}
	return e
}
