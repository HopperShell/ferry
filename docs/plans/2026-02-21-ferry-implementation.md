# Ferry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a TUI secure file transfer tool with dual-pane browser, visual sync, remote editing, and SSH-first design.

**Architecture:** Monolithic Bubble Tea model with clean package boundaries. SSH/SFTP/transfer logic in separate packages, UI components as sub-models. FileSystem interface abstracts local vs remote.

**Tech Stack:** Go, Bubble Tea, lipgloss, bubbles, golang.org/x/crypto/ssh, github.com/pkg/sftp, github.com/sahilm/fuzzy

---

## Task 1: Project Scaffold & Go Module

**Files:**
- Create: `go.mod`
- Create: `cmd/ferry/main.go`
- Create: `internal/app/app.go`

**Step 1: Initialize Go module**

Run: `go mod init github.com/andrewstuart/ferry`

**Step 2: Create minimal main.go**

```go
// cmd/ferry/main.go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/andrewstuart/ferry/internal/app"
)

func main() {
	p := tea.NewProgram(app.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ferry: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 3: Create stub app model**

```go
// internal/app/app.go
package app

import tea "github.com/charmbracelet/bubbletea"

type Model struct{}

func New() Model { return Model{} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	return "ferry - press q to quit"
}
```

**Step 4: Install dependencies and verify it builds**

Run: `go mod tidy && go build ./cmd/ferry/`
Expected: clean build, no errors

**Step 5: Run it to verify**

Run: `./ferry`
Expected: shows "ferry - press q to quit", q exits cleanly

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: project scaffold with minimal Bubble Tea app"
```

---

## Task 2: Theme & Styling Package

**Files:**
- Create: `internal/ui/theme/theme.go`
- Create: `internal/ui/theme/logo.go`

**Step 1: Create theme with ferry color palette**

```go
// internal/ui/theme/theme.go
package theme

import "github.com/charmbracelet/lipgloss"

var (
	// Primary palette - nautical
	Navy    = lipgloss.Color("#1B2838")
	Teal    = lipgloss.Color("#2D9B99")
	Cyan    = lipgloss.Color("#5DE4E7")
	Amber   = lipgloss.Color("#FFAA33")
	Red     = lipgloss.Color("#FF5555")
	Green   = lipgloss.Color("#50FA7B")
	White   = lipgloss.Color("#F8F8F2")
	Dim     = lipgloss.Color("#6272A4")
	BgDark  = lipgloss.Color("#0E1621")
	BgPanel = lipgloss.Color("#1B2838")

	// Component styles
	ActiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Cyan)

	InactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Dim)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Cyan).
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Background(Navy).
			Foreground(White).
			Padding(0, 1)

	DirStyle  = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	FileStyle = lipgloss.NewStyle().Foreground(White)
	ExecStyle = lipgloss.NewStyle().Foreground(Green)
	LinkStyle = lipgloss.NewStyle().Foreground(Cyan).Italic(true)
	SizeStyle = lipgloss.NewStyle().Foreground(Dim).Align(lipgloss.Right)

	SelectedStyle = lipgloss.NewStyle().
			Background(Teal).
			Foreground(White)

	CursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2A3A5A")).
			Foreground(White)

	ProgressBar    = lipgloss.NewStyle().Foreground(Cyan)
	ProgressFilled = lipgloss.NewStyle().Foreground(Amber)

	ErrorStyle   = lipgloss.NewStyle().Foreground(Red).Bold(true)
	WarningStyle = lipgloss.NewStyle().Foreground(Amber)
	SuccessStyle = lipgloss.NewStyle().Foreground(Green)
)
```

**Step 2: Create ASCII logo**

```go
// internal/ui/theme/logo.go
package theme

const Logo = `
   ___
  / _/__ ____________  __
 / _/ -_) __/ __/ // /
/_/ \__/_/ /_/  \_, /
               /___/
`

const Tagline = "secure file transfer, terminal style"
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: clean build

**Step 4: Commit**

```bash
git add -A && git commit -m "feat: add theme package with nautical color palette and logo"
```

---

## Task 3: FileSystem Interface & LocalFS

**Files:**
- Create: `internal/fs/fs.go`
- Create: `internal/fs/local.go`
- Create: `internal/fs/local_test.go`

**Step 1: Define FileSystem interface and Entry type**

```go
// internal/fs/fs.go
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
```

**Step 2: Write failing tests for LocalFS**

```go
// internal/fs/local_test.go
package fs_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewstuart/ferry/internal/fs"
)

func TestLocalFS_List(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	lfs := fs.NewLocalFS()
	entries, err := lfs.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLocalFS_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	lfs := fs.NewLocalFS()
	err := lfs.Write(path, bytes.NewReader([]byte("ferry")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = lfs.Read(path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "ferry" {
		t.Fatalf("expected 'ferry', got %q", buf.String())
	}
}

func TestLocalFS_MkdirRemoveRename(t *testing.T) {
	dir := t.TempDir()
	lfs := fs.NewLocalFS()

	sub := filepath.Join(dir, "newdir")
	if err := lfs.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	renamed := filepath.Join(dir, "renamed")
	if err := lfs.Rename(sub, renamed); err != nil {
		t.Fatal(err)
	}

	if err := lfs.Remove(renamed); err != nil {
		t.Fatal(err)
	}

	_, err := lfs.Stat(renamed)
	if err == nil {
		t.Fatal("expected error after remove")
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/fs/ -v`
Expected: FAIL — `NewLocalFS` not defined

**Step 4: Implement LocalFS**

```go
// internal/fs/local.go
package fs

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
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
	_ = fmt.Sprintf // avoid unused import
	return e
}
```

**Step 5: Run tests**

Run: `go test ./internal/fs/ -v`
Expected: all PASS

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: add FileSystem interface and LocalFS implementation with tests"
```

---

## Task 4: SSH Config Parsing & Connection

**Files:**
- Create: `internal/ssh/config.go`
- Create: `internal/ssh/config_test.go`
- Create: `internal/ssh/conn.go`

**Step 1: Write tests for SSH config parsing**

```go
// internal/ssh/config_test.go
package ssh_test

import (
	"os"
	"path/filepath"
	"testing"

	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
)

func TestParseSSHConfigHosts(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	os.WriteFile(configPath, []byte(`
Host myserver
    HostName 192.168.1.100
    User admin
    Port 2222

Host devbox
    HostName dev.example.com
    User developer

Host *
    ServerAliveInterval 60
`), 0644)

	hosts, err := ferrySSH.ParseConfigHosts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	// Should not include wildcard entries
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Name != "myserver" {
		t.Fatalf("expected 'myserver', got %q", hosts[0].Name)
	}
	if hosts[0].HostName != "192.168.1.100" {
		t.Fatalf("expected '192.168.1.100', got %q", hosts[0].HostName)
	}
	if hosts[0].User != "admin" {
		t.Fatalf("expected 'admin', got %q", hosts[0].User)
	}
	if hosts[0].Port != "2222" {
		t.Fatalf("expected '2222', got %q", hosts[0].Port)
	}
}
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/ssh/ -v`
Expected: FAIL

**Step 3: Implement SSH config parser**

```go
// internal/ssh/config.go
package ssh

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type HostEntry struct {
	Name     string
	HostName string
	User     string
	Port     string
}

func ParseConfigHosts(configPath string) ([]HostEntry, error) {
	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []HostEntry
	var current *HostEntry

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			// Try tab separator
			parts = strings.SplitN(line, "\t", 2)
			if len(parts) != 2 {
				continue
			}
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if strings.EqualFold(key, "Host") {
			if current != nil {
				hosts = append(hosts, *current)
			}
			// Skip wildcard and pattern entries
			if strings.ContainsAny(value, "*?!") {
				current = nil
				continue
			}
			current = &HostEntry{Name: value}
		} else if current != nil {
			switch strings.ToLower(key) {
			case "hostname":
				current.HostName = value
			case "user":
				current.User = value
			case "port":
				current.Port = value
			}
		}
	}
	if current != nil {
		hosts = append(hosts, *current)
	}
	return hosts, scanner.Err()
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}
```

**Step 4: Run tests**

Run: `go test ./internal/ssh/ -v`
Expected: PASS

**Step 5: Implement SSH connection manager**

```go
// internal/ssh/conn.go
package ssh

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type ResolvedConfig struct {
	HostName     string
	User         string
	Port         string
	IdentityFile []string
	ProxyJump    string
	ProxyCommand string
}

// Resolve uses `ssh -G` to get the fully resolved SSH config for a host.
func Resolve(host string) (*ResolvedConfig, error) {
	cmd := exec.Command("ssh", "-G", host)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ssh -G %s: %w", host, err)
	}

	cfg := &ResolvedConfig{Port: "22"}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "hostname":
			cfg.HostName = val
		case "user":
			cfg.User = val
		case "port":
			cfg.Port = val
		case "identityfile":
			cfg.IdentityFile = append(cfg.IdentityFile, val)
		case "proxyjump":
			if val != "none" {
				cfg.ProxyJump = val
			}
		case "proxycommand":
			if val != "none" {
				cfg.ProxyCommand = val
			}
		}
	}
	return cfg, nil
}

type ConnectOptions struct {
	Host              string
	PasswordCallback  func() (string, error)
	PassphraseCallback func(file string) (string, error)
}

type Connection struct {
	Client     *ssh.Client
	Config     *ResolvedConfig
	host       string
	keepaliveStop chan struct{}
}

func Connect(opts ConnectOptions) (*Connection, error) {
	cfg, err := Resolve(opts.Host)
	if err != nil {
		return nil, err
	}

	authMethods := buildAuthMethods(cfg, opts)

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: proper host key checking
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(cfg.HostName, cfg.Port)

	var client *ssh.Client

	if cfg.ProxyJump != "" {
		client, err = connectViaProxyJump(cfg.ProxyJump, addr, sshConfig)
	} else if cfg.ProxyCommand != "" {
		client, err = connectViaProxyCommand(cfg.ProxyCommand, addr, sshConfig)
	} else {
		client, err = ssh.Dial("tcp", addr, sshConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", opts.Host, err)
	}

	conn := &Connection{
		Client:        client,
		Config:        cfg,
		host:          opts.Host,
		keepaliveStop: make(chan struct{}),
	}
	go conn.keepalive()
	return conn, nil
}

func (c *Connection) Close() error {
	close(c.keepaliveStop)
	return c.Client.Close()
}

func (c *Connection) keepalive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _, err := c.Client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				return
			}
		case <-c.keepaliveStop:
			return
		}
	}
}

func buildAuthMethods(cfg *ResolvedConfig, opts ConnectOptions) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	// Try ssh-agent first
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	// Try key files
	for _, keyPath := range cfg.IdentityFile {
		expanded := expandPath(keyPath)
		key, err := os.ReadFile(expanded)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			// Might be encrypted — try passphrase
			if opts.PassphraseCallback != nil {
				if passphrase, cbErr := opts.PassphraseCallback(expanded); cbErr == nil {
					if signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase)); err == nil {
						methods = append(methods, ssh.PublicKeys(signer))
					}
				}
			}
			continue
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	// Password auth as fallback
	if opts.PasswordCallback != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return opts.PasswordCallback()
		}))
	}

	return methods
}

func connectViaProxyJump(jumpHost, target string, config *ssh.ClientConfig) (*ssh.Client, error) {
	jumpCfg, err := Resolve(jumpHost)
	if err != nil {
		return nil, err
	}
	jumpAddr := net.JoinHostPort(jumpCfg.HostName, jumpCfg.Port)

	jumpConfig := &ssh.ClientConfig{
		User:            jumpCfg.User,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	// Reuse agent auth for jump host
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			jumpConfig.Auth = []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}
		}
	}

	jumpClient, err := ssh.Dial("tcp", jumpAddr, jumpConfig)
	if err != nil {
		return nil, fmt.Errorf("connect to jump host %s: %w", jumpHost, err)
	}

	conn, err := jumpClient.Dial("tcp", target)
	if err != nil {
		jumpClient.Close()
		return nil, fmt.Errorf("tunnel through %s to %s: %w", jumpHost, target, err)
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, target, config)
	if err != nil {
		conn.Close()
		jumpClient.Close()
		return nil, err
	}
	return ssh.NewClient(ncc, chans, reqs), nil
}

func connectViaProxyCommand(proxyCmd, target string, config *ssh.ClientConfig) (*ssh.Client, error) {
	cmd := exec.Command("sh", "-c", proxyCmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proxy command: %w", err)
	}

	rwc := &readWriteCloser{stdout, stdin}
	c, chans, reqs, err := ssh.NewClientConn(rwc, target, config)
	if err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

type readWriteCloser struct {
	r interface{ Read([]byte) (int, error) }
	w interface{ Write([]byte) (int, error); Close() error }
}

func (rwc *readWriteCloser) Read(p []byte) (int, error)  { return rwc.r.Read(p) }
func (rwc *readWriteCloser) Write(p []byte) (int, error) { return rwc.w.Write(p) }
func (rwc *readWriteCloser) Close() error                { return rwc.w.Close() }

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
```

Note: need to add `"path/filepath"` import. The `expandPath` uses it.

**Step 6: Run tests and build**

Run: `go mod tidy && go test ./internal/ssh/ -v && go build ./...`
Expected: tests pass, build succeeds

**Step 7: Commit**

```bash
git add -A && git commit -m "feat: add SSH config parsing, connection with agent/ProxyJump/ProxyCommand support"
```

---

## Task 5: RemoteFS (SFTP Implementation)

**Files:**
- Create: `internal/fs/remote.go`

**Step 1: Implement RemoteFS wrapping SFTP**

```go
// internal/fs/remote.go
package fs

import (
	"io"
	"os"
	"strconv"
	"syscall"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

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
	info, err := r.client.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return remoteEntryFromFileInfo(path, info), nil
}

func (r *RemoteFS) Read(path string, w io.Writer) error {
	f, err := r.client.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func (r *RemoteFS) Write(path string, rd io.Reader, perm os.FileMode) error {
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
	return r.client.MkdirAll(path)
}

func (r *RemoteFS) Remove(path string) error {
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
	// SFTP rename is a posix rename
	return r.client.Rename(old, new)
}

func (r *RemoteFS) Chmod(path string, perm os.FileMode) error {
	return r.client.Chmod(path, perm)
}

func (r *RemoteFS) HomeDir() (string, error) {
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
```

**Step 2: Build**

Run: `go mod tidy && go build ./...`
Expected: clean build

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: add RemoteFS with SFTP implementation"
```

---

## Task 6: Connection Picker UI

**Files:**
- Create: `internal/ui/picker/picker.go`
- Modify: `internal/app/app.go` — integrate picker as initial view

**Step 1: Build fuzzy connection picker**

```go
// internal/ui/picker/picker.go
package picker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

type HostSelected struct {
	Host string
}

type Model struct {
	hosts    []ferrySSH.HostEntry
	filtered []ferrySSH.HostEntry
	input    textinput.Model
	cursor   int
	width    int
	height   int
}

func New(hosts []ferrySSH.HostEntry) Model {
	ti := textinput.New()
	ti.Placeholder = "Search hosts or enter user@host:port..."
	ti.Focus()
	ti.CharLimit = 256

	return Model{
		hosts:    hosts,
		filtered: hosts,
		input:    ti,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				return m, selectHost(m.filtered[m.cursor].Name)
			}
			// Manual entry
			if m.input.Value() != "" {
				return m, selectHost(m.input.Value())
			}
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "esc":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Filter hosts
	query := m.input.Value()
	if query == "" {
		m.filtered = m.hosts
	} else {
		names := make([]string, len(m.hosts))
		for i, h := range m.hosts {
			names[i] = fmt.Sprintf("%s %s %s", h.Name, h.HostName, h.User)
		}
		matches := fuzzy.Find(query, names)
		m.filtered = make([]ferrySSH.HostEntry, len(matches))
		for i, match := range matches {
			m.filtered[i] = m.hosts[match.Index]
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}

	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder

	// Logo
	logo := lipgloss.NewStyle().Foreground(theme.Cyan).Bold(true).Render(theme.Logo)
	tagline := lipgloss.NewStyle().Foreground(theme.Dim).Render(theme.Tagline)
	b.WriteString(logo)
	b.WriteString(tagline + "\n\n")

	// Search input
	b.WriteString(m.input.View() + "\n\n")

	// Host list
	maxVisible := m.height - 14 // leave room for logo, input, footer
	if maxVisible < 3 {
		maxVisible = 3
	}

	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	for i := start; i < len(m.filtered) && i < start+maxVisible; i++ {
		h := m.filtered[i]
		host := h.HostName
		if host == "" {
			host = h.Name
		}
		user := h.User
		if user == "" {
			user = "~"
		}
		port := h.Port
		if port == "" || port == "22" {
			port = ""
		} else {
			port = ":" + port
		}

		line := fmt.Sprintf("  %s  %s@%s%s", h.Name, user, host, port)

		if i == m.cursor {
			line = theme.CursorStyle.Render("> " + line[2:])
		} else {
			line = lipgloss.NewStyle().Foreground(theme.White).Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	footer := lipgloss.NewStyle().Foreground(theme.Dim).Render("  enter:connect  esc:quit")
	b.WriteString(footer)

	return b.String()
}

func selectHost(host string) tea.Cmd {
	return func() tea.Msg {
		return HostSelected{Host: host}
	}
}
```

**Step 2: Build and verify**

Run: `go mod tidy && go build ./...`
Expected: clean build

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: add fuzzy connection picker with SSH config host search"
```

---

## Task 7: File Browser Pane Component

**Files:**
- Create: `internal/ui/pane/pane.go`

**Step 1: Build the file browser pane**

This is the core UI component — a scrollable file list that works identically for local and remote. Supports cursor movement, selection, sorting (dirs first), hidden file toggle, and search.

The pane takes a `fs.FileSystem` and manages its own state (current path, cursor, scroll offset, selected files). It exposes methods for the parent model to read state and trigger operations.

Key behaviors:
- Columns: icon/name, size, mtime
- Dirs first, then alphabetical
- `Space` toggles selection, `V` for range select
- `/` opens inline search filter
- Maintains breadcrumb path in header
- Reports cursor entry for file info panel

**Step 2: Build and verify**

Run: `go build ./...`
Expected: clean build

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: add file browser pane component with vim navigation and selection"
```

---

## Task 8: App Model — Wire Dual Pane Layout

**Files:**
- Modify: `internal/app/app.go` — full rewrite with state machine (picker → dual pane)
- Create: `internal/ui/statusbar/statusbar.go`

**Step 1: Create status bar**

Simple bottom bar showing connection info, selected file count, and keybinding hints.

**Step 2: Rewrite app model**

State machine:
- `statePicker` — show connection picker
- `stateConnecting` — show connection spinner
- `stateBrowser` — dual-pane file browser (main view)
- `stateSync` — sync/diff view

The model owns: left pane (local), right pane (remote), active pane index, SSH connection, overlays (transfer, help, info).

Tab switches active pane. All key events route to the active pane unless an overlay is open.

**Step 3: Build and run**

Run: `go build ./cmd/ferry/ && ./ferry`
Expected: shows picker, selecting a host connects and shows dual pane

**Step 4: Commit**

```bash
git add -A && git commit -m "feat: wire dual-pane layout with picker → connecting → browser state machine"
```

---

## Task 9: File Operations (Copy, Move, Delete, Rename, Mkdir)

**Files:**
- Create: `internal/transfer/engine.go`
- Create: `internal/transfer/progress.go`
- Modify: `internal/app/app.go` — handle file operation keybindings

**Step 1: Build transfer engine**

The engine manages a queue of transfer jobs. Each job is a source path + source FS + dest path + dest FS. Runs N concurrent workers. Each worker reads from source, writes to dest, reporting progress via a channel.

```go
type Job struct {
    ID       string
    SrcPath  string
    SrcFS    fs.FileSystem
    DstPath  string
    DstFS    fs.FileSystem
    Size     int64
    Status   JobStatus // pending, active, completed, failed
}

type ProgressEvent struct {
    JobID      string
    BytesSent  int64
    TotalBytes int64
    Speed      float64 // bytes/sec
    Done       bool
    Err        error
}
```

**Step 2: Build progress tracking**

A `ProgressReader` wraps `io.Reader` to track bytes read and emit events. Speed calculated via sliding window (last 3 seconds of samples). ETA = remaining bytes / speed.

**Step 3: Wire keybindings in app**

- `yy` on selected files → mark for copy (store source paths + FS)
- `p` → create transfer jobs from marked files to current pane's directory
- `dd` → delete with confirmation modal
- `r` → rename prompt (inline text input)
- `m` → move (copy + delete source on success)
- `D` → mkdir prompt

**Step 4: Build and test manually**

Run: `go build ./cmd/ferry/ && ./ferry`
Expected: can copy files between local and remote panes, see progress

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: add transfer engine with progress tracking and file operation keybindings"
```

---

## Task 10: Transfer Progress Overlay

**Files:**
- Create: `internal/ui/transfer/overlay.go`
- Modify: `internal/app/app.go` — show overlay during active transfers

**Step 1: Build transfer overlay component**

Subscribes to progress events from the engine. Renders a centered overlay showing:
- Per-file progress bar with percentage and speed
- Queue status (N/M files)
- Aggregate ETA
- Esc to cancel

Uses `tea.Sub` (or channel → Msg pattern) to receive progress updates.

**Step 2: Wire into app**

`t` toggles persistent queue view. Overlay auto-appears when transfers start, can be dismissed with Esc (transfers continue in background).

**Step 3: Build and test**

Run: `go build ./cmd/ferry/`
Expected: transfer overlay appears during file copies

**Step 4: Commit**

```bash
git add -A && git commit -m "feat: add transfer progress overlay with speed and ETA display"
```

---

## Task 11: Sync/Diff View

**Files:**
- Create: `internal/ui/diff/diff.go`
- Create: `internal/transfer/sync.go`
- Modify: `internal/app/app.go` — add sync state

**Step 1: Build tree comparison logic**

Walk both FileSystems, build a unified tree of entries with status:
- `Same` — exists on both sides, same size + mtime
- `LocalOnly` — exists only locally
- `RemoteOnly` — exists only on remote
- `Modified` — exists on both but size or mtime differs (show which is newer)

**Step 2: Build diff view component**

Replaces dual-pane when activated via `S`. Unified tree with status icons `[+]` `[-]` `[M]` `[=]`. Space to toggle selection, `a` to select all modified/missing. `→` to push selected to remote, `←` to pull to local. Esc to return to browser.

**Step 3: Build rsync integration**

Check if rsync exists on remote (`which rsync`). If yes, sync operations use `rsync -avz --progress -e "ssh ..."`. Parse rsync's `--progress` output for file-level progress. If rsync not available, fall back to SFTP-based file-by-file sync.

**Step 4: Wire into app**

`S` enters sync mode. Esc returns to browser. Transfers go through the same engine.

**Step 5: Build and test**

Run: `go build ./cmd/ferry/`
Expected: `S` shows diff view, can push/pull files

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: add visual sync/diff view with rsync integration"
```

---

## Task 12: Remote File Editing

**Files:**
- Create: `internal/editor/editor.go`
- Modify: `internal/app/app.go` — handle `e` keybinding

**Step 1: Implement editor flow**

```go
type EditSession struct {
    RemotePath  string
    TempPath    string    // local temp file
    ShadowPath  string    // original copy for conflict detection
    RemoteMTime time.Time // mtime at download time
    FS          fs.FileSystem
}
```

Flow:
1. `e` on remote file → download to temp dir, save shadow copy, record mtime
2. `tea.Exec` to open `$EDITOR` (or `$VISUAL`, fallback `vi`)
3. On return, compare temp file to shadow. If unchanged, clean up.
4. If changed, check remote mtime. If same, upload. If different, show conflict modal.
5. On upload failure, keep temp file, offer retry on reconnect.

**Step 2: Wire into app**

`e` on local file → just open in editor directly.
`e` on remote file → full edit session flow.

**Step 3: Build and test**

Run: `go build ./cmd/ferry/`
Expected: `e` opens file in editor, saves back to remote

**Step 4: Commit**

```bash
git add -A && git commit -m "feat: add remote file editing with shadow copy and conflict detection"
```

---

## Task 13: Toggleable Panels (Info, Help)

**Files:**
- Create: `internal/ui/modal/info.go`
- Create: `internal/ui/modal/help.go`
- Modify: `internal/app/app.go` — toggle overlays

**Step 1: File info panel**

`i` toggles a panel showing: full path, permissions (rwxrwxrwx), owner:group, size (human readable), mtime, type. Renders as a right-side split or bottom panel.

**Step 2: Help overlay**

`?` shows a keybinding reference as a centered overlay. All bindings listed in categories (Navigation, File Ops, Views). Esc dismisses.

**Step 3: Wire into app**

Overlays take priority in `Update` — Esc always closes the topmost overlay.

**Step 4: Build and test**

Run: `go build ./cmd/ferry/`

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: add file info panel and help overlay"
```

---

## Task 14: CLI Argument Parsing

**Files:**
- Modify: `cmd/ferry/main.go` — parse args

**Step 1: Handle CLI args**

- `ferry` → launch picker
- `ferry myhost` → connect directly to host (skip picker)
- `ferry user@host` → connect with explicit user
- `ferry user@host:port` → connect with explicit user and port
- `ferry --help` / `ferry -h` → usage text

Use `flag` package — no need for cobra for this simple interface.

**Step 2: Build and test**

Run: `go build ./cmd/ferry/ && ./ferry --help`
Expected: shows usage

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: add CLI argument parsing for direct host connection"
```

---

## Task 15: Polish & Error Handling

**Files:**
- Modify: various — error handling, edge cases, visual polish

**Step 1: Connection error handling**

- Auth failure → modal with retry option
- Connection drop → "Reconnecting..." modal with spinner and auto-retry (exponential backoff, max 3 attempts)
- SFTP errors → status bar message, auto-dismiss after 5s

**Step 2: Visual polish**

- File type icons/colors (dirs bold cyan + `/`, executables green, symlinks cyan italic)
- Smooth scrolling in panes
- Proper terminal resize handling throughout
- Loading spinners for async operations (connecting, listing large dirs)

**Step 3: Edge cases**

- Handle symlinks gracefully (show target, follow on open)
- Handle permission denied (show error, don't crash)
- Handle very long filenames (truncate with ellipsis)
- Handle empty directories
- Handle binary files (don't try to edit, show warning)

**Step 4: Build and full manual test**

Run: `go build ./cmd/ferry/`
Test: connect to a real host, browse, copy files, sync, edit, disconnect/reconnect

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: polish error handling, visual styling, and edge cases"
```

---

## Task 16: README & Release Prep

**Files:**
- Create: `README.md`
- Create: `Makefile`
- Create: `.goreleaser.yml`

**Step 1: README**

Install instructions, screenshots placeholder, feature list, keybinding reference, comparison with termscp.

**Step 2: Makefile**

`make build`, `make install`, `make test`, `make lint`.

**Step 3: GoReleaser config**

Cross-compile for linux/darwin/windows, amd64/arm64. Homebrew tap formula.

**Step 4: Commit**

```bash
git add -A && git commit -m "docs: add README, Makefile, and GoReleaser config"
```
