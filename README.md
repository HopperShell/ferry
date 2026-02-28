<p align="center">
  <img src="logo-concepts/ferry-logo.png" alt="ferry" width="480" />
</p>

<p align="center">
  <strong>Secure file transfer, terminal style.</strong>
</p>

<!-- <p align="center">
  <a href="https://goreportcard.com/report/github.com/andrewstuart/ferry"><img src="https://goreportcard.com/badge/github.com/andrewstuart/ferry" alt="Go Report Card"></a>
  <a href="https://github.com/andrewstuart/ferry/releases"><img src="https://img.shields.io/github/v/release/andrewstuart/ferry" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p> -->

---

Ferry is a terminal-based file transfer tool built for engineers who live in the terminal. Dual-pane file browser (local + remote) over SSH or S3, with vim-style keybindings, visual directory sync, and remote file editing — all without leaving the command line.

## Features

- **Dual-pane browser** — Local and remote side by side
- **Full SSH config support** — ssh-agent, ProxyJump, encrypted keys, `~/.ssh/config` includes
- **Visual sync/diff** — Compare directories, selective push/pull
- **Remote file editing** — `$EDITOR` with shadow copy and conflict detection
- **Resumable transfers** — Interrupted transfers pick up where they left off, failed transfers retry automatically
- **Transfer progress** — Speed, ETA, concurrent workers
- **Overwrite protection** — Confirms before overwriting existing files
- **Sortable file list** — Sort by name, size, or date; selection size totals
- **S3 support** — Browse and transfer files to/from Amazon S3 buckets
- **Fuzzy connection picker** — Search your `~/.ssh/config` hosts and S3 buckets
- **Vim keybindings** — With arrow key support for the rest of us
- **rsync integration** — Fast delta transfers when available

## Install

### From source

```sh
go install github.com/andrewstuart/ferry/cmd/ferry@latest
```

### Binary releases

Download a prebuilt binary from the [Releases](https://github.com/andrewstuart/ferry/releases) page.

<!-- ### Homebrew
```sh
brew install andrewstuart/tap/ferry
``` -->

## Quick Start

```sh
# Install
go install github.com/andrewstuart/ferry/cmd/ferry@latest

# Connect to a host (or just run `ferry` to pick from ~/.ssh/config)
ferry myhost
```

Once connected you'll see two panes — local files on the left, remote on the right.

1. **Navigate** — `j`/`k` to move, `l` to open a directory, `h` to go back
2. **Select files** — `Space` to select, `V` for range select
3. **Copy across** — `yy` to yank, `Tab` to switch panes, `p` to paste
4. **Sync directories** — `S` to compare and selectively push/pull
5. **Edit remote files** — `e` opens in `$EDITOR` with conflict detection

Press `?` for the full keybinding reference.

## Usage

```
ferry                     # Launch connection picker
ferry myhost              # Connect to SSH host
ferry user@host           # Connect with explicit user
ferry user@host:port      # Connect with user and port
ferry s3://my-bucket      # Connect to S3 bucket
ferry s3://my-bucket/path # Connect to S3 bucket at prefix
```

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `j` / `Down` | Move cursor down |
| `k` / `Up` | Move cursor up |
| `h` / `Left` / `Backspace` | Go to parent directory |
| `l` / `Right` / `Enter` | Open directory or file |
| `gg` | Go to top |
| `G` | Go to bottom |
| `Ctrl+d` | Page down |
| `Ctrl+u` | Page up |
| `Tab` | Switch active pane |
| `/` | Search / filter |
| `H` | Toggle hidden files |
| `s` | Cycle sort (name → size → date) |

### File Operations

| Key | Action |
|-----|--------|
| `Space` | Toggle file selection |
| `V` | Range select |
| `yy` | Yank (mark for copy) |
| `p` | Paste yanked files to current pane |
| `m` | Move selected to current pane |
| `dd` | Delete (with confirmation) |
| `r` | Rename |
| `e` | Edit file (remote editing if remote pane) |
| `D` | Create directory |

### Views

| Key | Action |
|-----|--------|
| `S` | Enter sync/diff view |
| `i` | Toggle file info panel |
| `t` | Toggle transfer queue |
| `?` | Help overlay |
| `R` | Reconnect |
| `Esc` | Close overlay / cancel |
| `q` / `Ctrl+c` | Quit |

## S3 Support

Ferry can browse and transfer files to/from Amazon S3 buckets:

```sh
ferry s3://my-bucket           # Connect to S3 bucket
ferry s3://my-bucket/prefix    # Connect to specific prefix
```

Uses the standard AWS credential chain (environment variables, `~/.aws/credentials`, IAM roles). S3 buckets also appear in the connection picker when AWS credentials are detected.

All features work with S3: browse, upload/download, sync/diff, rename, delete, mkdir, and remote editing.

## How It Works

Ferry connects over SSH/SFTP or S3 using your existing config. Files transfer through a concurrent engine with progress tracking. For directory sync, it compares file trees by size and modification time, then lets you selectively push or pull changes — or use rsync for SSH connections.

Interrupted transfers are resumable: completed files are detected by matching size and mtime and skipped on retry. Failed transfers retry automatically (up to 2 times) to handle transient network issues. Writes use atomic temp files (`.ferry-tmp` → rename) so partial transfers are never mistaken for complete ones. Pasting into a directory with existing files prompts for overwrite confirmation.

## ferry vs termscp

| | ferry | termscp |
|---|---|---|
| Interface | Dual-pane (local + remote) | Single-pane with tab switching |
| Navigation | Vim keybindings + arrows | Arrow keys only |
| SSH config | Full support (ProxyJump, agent, includes) | Basic support |
| Directory sync | Visual diff with selective transfer | No |
| Remote editing | `$EDITOR` with conflict detection | Basic remote edit |
| Resume transfers | Yes (skip completed files, auto-retry) | No |
| Overwrite protection | Yes (confirms before overwriting) | No |
| rsync integration | Yes | No |

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [pkg/sftp](https://github.com/pkg/sftp) — SFTP client
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) — SSH client
- [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2) — S3 client

## License

MIT
