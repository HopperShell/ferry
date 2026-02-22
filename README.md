```
   ___
  / _/__ ____________  __
 / _/ -_) __/ __/ // /
/_/ \__/_/ /_/  \_, /
               /___/
```

**Secure file transfer, terminal style.**

<!-- badges -->
<!-- [![Go Report Card](https://goreportcard.com/badge/github.com/andrewstuart/ferry)](https://goreportcard.com/report/github.com/andrewstuart/ferry) -->
<!-- [![Release](https://img.shields.io/github/v/release/andrewstuart/ferry)](https://github.com/andrewstuart/ferry/releases) -->
<!-- [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) -->

---

## What is ferry

Ferry is a terminal-based file transfer tool built for engineers who live in the terminal. It provides a dual-pane file browser (local on the left, remote on the right) over SSH, with vim-style keybindings, visual directory sync, and remote file editing -- all without leaving the command line.

## Features

- Dual-pane file browser (local and remote side by side)
- Full SSH config support (ssh-agent forwarding, ProxyJump, encrypted keys)
- Visual sync/diff mode (compare directories, selective transfer)
- Remote file editing ($EDITOR with shadow copy and conflict detection)
- Transfer progress with speed and ETA
- Fuzzy connection picker from ~/.ssh/config
- Vim-style keybindings with arrow key support
- rsync integration for fast delta transfers

## Install

### From source

```sh
go install github.com/andrewstuart/ferry/cmd/ferry@latest
```

### Binary releases

Download a prebuilt binary from the [Releases](https://github.com/andrewstuart/ferry/releases) page.

<!-- ### Homebrew -->
<!-- ```sh -->
<!-- brew install andrewstuart/tap/ferry -->
<!-- ``` -->

## Usage

```
ferry                     # Launch connection picker
ferry myhost              # Connect to SSH host
ferry user@host           # Connect with explicit user
ferry user@host:port      # Connect with user and port
```

Flags:

```
ferry -v                  # Show version
ferry --version           # Show version
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
| `/` | Search / filter in current directory |
| `H` | Toggle hidden files |

### File Operations

| Key | Action |
|-----|--------|
| `Space` | Toggle file selection |
| `V` | Range select |
| `yy` | Yank (mark for copy) |
| `dd` | Delete selected files (with confirmation) |
| `p` | Paste (transfer yanked files to current pane) |
| `r` | Rename |
| `m` | Move selected to current pane |
| `e` | Edit file (remote editing if remote pane) |
| `D` | Create directory |

### Views and Panels

| Key | Action |
|-----|--------|
| `i` | Toggle file info panel |
| `t` | Toggle transfer queue |
| `?` | Toggle help overlay |
| `S` | Enter sync/diff view |
| `Esc` | Close overlay / cancel / back |
| `q` / `Ctrl+c` | Quit |

## ferry vs termscp

| | ferry | termscp |
|---|---|---|
| Interface | Dual-pane (local + remote) | Single-pane with tab switching |
| Navigation | Vim keybindings + arrows | Arrow keys only |
| SSH config | Full support (ProxyJump, agent, includes) | Basic support |
| Directory sync | Visual diff with selective transfer | No |
| Remote editing | $EDITOR with conflict detection | Basic remote edit |
| Connection picker | Fuzzy search over ~/.ssh/config | Manual entry or bookmarks |
| Transfer engine | rsync integration for delta transfers | SCP/SFTP only |

## Built With

- [Go](https://go.dev)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) -- TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) -- Terminal styling
- [Bubbles](https://github.com/charmbracelet/bubbles) -- TUI components
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) -- SSH client
- [pkg/sftp](https://github.com/pkg/sftp) -- SFTP client
- [sahilm/fuzzy](https://github.com/sahilm/fuzzy) -- Fuzzy matching

## License

MIT
