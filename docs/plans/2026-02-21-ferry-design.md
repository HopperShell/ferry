# Ferry — Design Document

> A TUI secure file transfer tool built with Go and Bubble Tea.
> SSH-first dual-pane file browser with visual sync, remote editing, and modern UX.

## Core Identity

Ferry is a supercharged scp/rsync with a lazygit-level TUI. If `ssh myhost` works, `ferry myhost` works. It replaces termscp by doing SSH properly (ssh-agent, ProxyJump, encrypted keys) and adding features termscp lacks (visual sync/diff, remote editing).

## Architecture

### Approach: Monolithic Bubble Tea Model

Single top-level `tea.Model` owns all state. Sub-components are plain structs with render/update methods, not independent Bubble Tea programs. Cross-component operations (copy between panes, sync) are straightforward. Clean package boundaries keep it manageable.

### Package Structure

```
ferry/
├── cmd/ferry/              # CLI entrypoint, arg parsing (cobra or plain flag)
├── internal/
│   ├── app/                # Top-level Bubble Tea model, message routing, glue
│   ├── ui/
│   │   ├── pane/           # File browser pane (shared by local & remote)
│   │   ├── picker/         # Fuzzy connection picker (SSH config hosts + manual entry)
│   │   ├── transfer/       # Transfer progress overlay & queue panel
│   │   ├── diff/           # Sync/diff unified tree view
│   │   ├── modal/          # Confirmations, prompts, passphrase input, file info
│   │   ├── statusbar/      # Bottom bar: connection info, keybinding hints, errors
│   │   └── theme/          # Colors, lipgloss styles, ASCII logo
│   ├── ssh/                # SSH config, connection, agent, ProxyJump
│   ├── fs/                 # FileSystem interface + LocalFS + RemoteFS (SFTP)
│   ├── transfer/           # Transfer engine: queue, progress, rsync integration
│   └── editor/             # Remote file editing: shadow copy, conflict detection
├── docs/plans/
├── go.mod
└── go.sum
```

## SSH Layer (`internal/ssh/`)

### Config Parsing

Use `ssh -G <host>` to resolve SSH config. This delegates all parsing (wildcards, Match blocks, Include directives) to OpenSSH itself. Parse the output for: HostName, User, Port, IdentityFile, ProxyJump, ProxyCommand.

For the fuzzy picker, parse `~/.ssh/config` directly to extract Host entries and any comments/aliases. This is a simpler parse — just host names for display.

### Connection

Using `golang.org/x/crypto/ssh`:

1. Try ssh-agent first via `SSH_AUTH_SOCK`
2. Try key files from config. If encrypted, prompt for passphrase via Bubble Tea modal
3. Fall back to password auth as last resort (prompt via modal)
4. ProxyJump: dial intermediate hosts in chain, tunnel through
5. ProxyCommand: pipe through command's stdin/stdout via `os/exec`
6. Keepalive: periodic `SendRequest("keepalive@openssh.com")` to detect dropped connections

### Session Management

- Maintain persistent SSH connection
- Open SFTP subsystem for remote filesystem operations
- Execute remote commands (rsync detection, rsync execution)
- Auto-reconnect on drop with UI notification
- Connection state exposed to status bar

## Filesystem Abstraction (`internal/fs/`)

```go
type FileSystem interface {
    List(path string) ([]Entry, error)
    Stat(path string) (Entry, error)
    Read(path string, w io.Writer) error
    Write(path string, r io.Reader, perm os.FileMode) error
    Mkdir(path string, perm os.FileMode) error
    Remove(path string) error
    Rename(old, new string) error
    Chmod(path string, perm os.FileMode) error
}

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
```

- **LocalFS**: thin wrapper around `os` package
- **RemoteFS**: wraps `github.com/pkg/sftp` client
- Pane component is generic — takes a `FileSystem` + label, renders identically for both sides
- Enables easy testing via mock implementations

## UI Components

### Layout

Classic side-by-side dual-pane. Always visible:

```
┌─ Local: ~/projects ──────────┬─ Remote: /var/www ────────────┐
│ ..                           │ ..                            │
│ > src/                       │   css/                        │
│   docs/                      │   js/                         │
│   README.md          4.2K    │   index.html          1.8K   │
│   go.mod             892B    │   config.yml          340B   │
│                              │                               │
├──────────────────────────────┴───────────────────────────────┤
│ ferry@myhost:22 | 3 files selected | Tab:switch  ?:help      │
└──────────────────────────────────────────────────────────────┘
```

### Connection Picker

Launched on `ferry` with no args. Shows:

- ASCII ferry logo at top
- Fuzzy-searchable list of SSH config hosts
- Each entry shows Host, HostName, User
- Bottom option: "Connect manually..." for `user@host:port` input
- `/` or just start typing to filter

### Pane Component (`ui/pane/`)

- Scrollable file list with columns: name, size, mtime
- Directory-first sorting, then alphabetical
- `.` files toggle via `H` (show/hide hidden)
- Cursor movement: `j/k` or arrows, `gg/G` for top/bottom, `Ctrl+d/u` for page jumps
- `Enter` or `l` to open directory / preview file
- `Backspace` or `h` to go up
- `/` for fuzzy search within current directory
- `Space` to toggle select, `V` for range select
- `Tab` to switch active pane

### Toggleable Panels

All contextual — hidden by default:

- **`i` — File info**: permissions, owner, group, size, mtime, full path. Rendered as a side panel or bottom split depending on terminal width.
- **`t` — Transfer queue**: list of pending/active/completed transfers with progress bars. Bottom split panel.
- **`?` — Help**: keybinding reference overlay. Dismissible.

### Diff/Sync View

Activated via `:sync` or a keybinding (e.g., `S`). Replaces the dual-pane with a unified tree:

```
┌─ Sync: ~/projects ↔ /var/www ────────────────────────────────┐
│ [=] css/                                                      │
│ [+] css/new-theme.css               local only    4.2K       │
│ [=] js/                                                       │
│ [M] js/app.js                       local newer   12K → 14K  │
│ [-] old-config.yml                  remote only   340B       │
│ [=] index.html                                               │
├──────────────────────────────────────────────────────────────┤
│ Space:toggle  a:select all  →:push to remote  ←:pull local   │
└──────────────────────────────────────────────────────────────┘
```

Status icons: `[+]` local only, `[-]` remote only, `[M]` modified (size/mtime differ), `[=]` same.

Comparison is by existence + size + mtime. User selects files, chooses direction (push/pull), confirms. Transfers execute via the transfer engine.

If rsync is available on the remote, sync operations use `rsync -avz --progress` over SSH for delta transfers. Otherwise falls back to SFTP-based file-by-file transfer.

### Transfer Progress

Overlay/popup for active transfers:

```
┌─ Transferring ───────────────────────────────┐
│ app.js          ████████████░░░░  74%  2.1MB/s │
│ styles.css      ██████████████████ 100% done   │
│ bundle.min.js   ░░░░░░░░░░░░░░░░  queued      │
│                                                │
│ 2/3 files  |  ETA: 12s  |  Esc: cancel        │
└────────────────────────────────────────────────┘
```

Progress events flow from the transfer engine via channels, converted to Bubble Tea messages via `tea.Sub`.

## Transfer Engine (`internal/transfer/`)

### Modes

1. **Single file**: SFTP read → write with progress tracking. Context-cancellable.
2. **Batch**: queue of single transfers. N concurrent (default 3). Individual + aggregate progress.
3. **Sync**: if remote has rsync, use `rsync -avz -e "ssh ..." --progress`. Parse rsync output for UI. Fallback: walk both trees via `FileSystem`, compare entries, transfer missing/newer files via SFTP.

### Progress Reporting

Each transfer emits `ProgressEvent{File, BytesSent, TotalBytes, Speed}` on a channel. The UI subscribes via `tea.Sub` and updates the transfer overlay.

### Resume on Reconnect

Connection drop pauses the queue. On SSH reconnect, the engine offers to resume. SFTP transfers restart from the beginning (SFTP doesn't support resume). rsync transfers re-run (rsync handles partial files natively).

## Remote File Editing (`internal/editor/`)

### Flow

1. Download file to temp dir via SFTP. Save a shadow copy of the original.
2. Record remote mtime at download time.
3. Open temp file in `$EDITOR` via `tea.Exec` (suspends Bubble Tea, gives terminal to editor).
4. On editor exit, check if file was modified locally.
5. If modified: check remote mtime. If unchanged, upload. If changed, show conflict modal (overwrite / re-download / abort).
6. If upload fails (connection dropped), keep the edited temp file. On reconnect, offer to retry upload.
7. Clean up temp files on exit.

## Keybindings

### Navigation
| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `h` / `←` / `Backspace` | Go to parent directory |
| `l` / `→` / `Enter` | Open directory / file |
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

### Views & Panels
| Key | Action |
|-----|--------|
| `i` | Toggle file info panel |
| `t` | Toggle transfer queue |
| `?` | Toggle help overlay |
| `S` | Enter sync/diff view |
| `Esc` | Close overlay / cancel / back |
| `q` / `Ctrl+c` | Quit |

## Theme & Visual Identity

- Custom color palette: deep navy/teal primary, bright cyan accents, warm amber for warnings/progress. Nautical but modern.
- ASCII "ferry" logo on connection picker screen.
- Styled with lipgloss throughout.
- Subtle box-drawing characters for borders.
- File type indicators: directories bold + trailing `/`, executables green, symlinks cyan + italic.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | Styling |
| `github.com/charmbracelet/bubbles` | Common components (textinput, list, viewport, progress) |
| `golang.org/x/crypto/ssh` | SSH client, agent |
| `github.com/pkg/sftp` | SFTP client |
| `github.com/sahilm/fuzzy` | Fuzzy matching for picker |

## Error Handling

- **Non-fatal** (permission denied, file not found): show in status bar, auto-dismiss after 5s.
- **Connection lost**: modal with "Reconnecting..." spinner. Auto-retry with backoff. Option to cancel and quit.
- **Transfer failure**: mark failed in queue, offer retry. Don't block other transfers.
- **Auth failure**: modal with option to retry passphrase or abort.
