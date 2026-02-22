package fs

import (
	"io"
	"os"
	"time"
)

type Entry struct {
	Name    string
	Path    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	IsDir   bool
	Owner   string
	Group   string
}

type FileSystem interface {
	List(path string) ([]Entry, error)
	Stat(path string) (Entry, error)
	Read(path string, w io.Writer) error
	Write(path string, r io.Reader, perm os.FileMode) error
	Mkdir(path string, perm os.FileMode) error
	Remove(path string) error
	Rename(old, new string) error
	Chmod(path string, perm os.FileMode) error
	HomeDir() (string, error)
}
