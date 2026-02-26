# Ferry - Future Upgrades

## Transfers & Sync

- [x] Use rsync for sync transfers when available (infrastructure exists, not wired up)
- [x] Resume interrupted transfers
- [ ] Retry failed transfers automatically
- [x] Confirm before overwriting existing files on copy/paste
- [x] Atomic writes (write to temp file, rename on success)
- [x] Streaming progress for large files (pipe instead of buffer)

## Navigation & File Operations

- [ ] Resizable pane split (drag or keybind to adjust 50/50)
- [ ] Directory bookmarks / favorites
- [ ] Bulk rename with pattern support
- [ ] File preview pane for text files
- [ ] Text file diff before transferring modified files
- [ ] Archive / compress files (tar, zip)
- [ ] Symlink-aware mode (option to not follow symlinks)

## Connection & Auth

- [ ] SSH key passphrase prompt (currently only works via ssh-agent)
- [ ] Remember password for session (don't re-prompt on reconnect)
- [ ] Connection profiles (save user/host/port/key combos outside ssh config)
- [ ] Multiple simultaneous connections

## UI & UX

- [ ] Vi-style command mode (`:cd /path`, `:connect host`)
- [ ] Customizable keybindings
- [ ] Customizable color themes
- [ ] Search match count indicator
- [ ] Persistent error log (scrollable, not auto-dismiss)
- [ ] Sortable file list (by name, size, date)
- [ ] File size totals for selections

## Performance

- [ ] Directory listing cache with invalidation
- [ ] Search debouncing for large directories
- [ ] Background pane refresh after file operations
- [ ] Connection pooling for concurrent SFTP operations

## Testing

- [x] Integration tests for transfer engine
- [ ] Integration tests for sync/diff compare
- [ ] UI snapshot tests
- [ ] CI pipeline

## Polish

- [ ] Man page / built-in docs
- [ ] Shell completions (bash, zsh, fish)
- [ ] Homebrew formula
- [ ] Release binaries for linux/mac/windows
