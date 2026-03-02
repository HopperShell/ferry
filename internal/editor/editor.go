package editor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
	tea "github.com/charmbracelet/bubbletea"
)

// EditSession holds state for an in-progress remote file edit.
type EditSession struct {
	RemotePath  string
	TempPath    string    // local temp file for editing
	ShadowPath  string    // original copy for change detection
	RemoteMTime time.Time // mtime at download time
	RemoteFS    fs.FileSystem
}

// EditCompleteMsg is sent when the edit/upload cycle finishes.
type EditCompleteMsg struct {
	Session  *EditSession
	Modified bool // whether the file was changed
	Err      error
}

// ConflictMsg is sent when the remote file changed since download.
type ConflictMsg struct {
	Session *EditSession
}

// UploadCompleteMsg is sent after a force upload finishes.
type UploadCompleteMsg struct {
	Session *EditSession
	Err     error
}

// EditSessionReadyMsg is sent when a remote file has been downloaded and is ready to edit.
type EditSessionReadyMsg struct {
	Session *EditSession
	Err     error
}

// StartEdit downloads a remote file and prepares a local edit session.
func StartEdit(remoteFS fs.FileSystem, remotePath string) tea.Cmd {
	return func() tea.Msg {
		// Create a temp directory for this edit session.
		tmpDir, err := os.MkdirTemp("", "ferry-edit-*")
		if err != nil {
			return EditSessionReadyMsg{Err: err}
		}

		baseName := filepath.Base(remotePath)
		tempPath := filepath.Join(tmpDir, baseName)
		shadowPath := filepath.Join(tmpDir, ".shadow-"+baseName)

		// Download the remote file.
		var buf bytes.Buffer
		if err := remoteFS.Read(remotePath, &buf); err != nil {
			os.RemoveAll(tmpDir)
			return EditSessionReadyMsg{Err: err}
		}

		// Write the temp file for editing.
		if err := os.WriteFile(tempPath, buf.Bytes(), 0o644); err != nil {
			os.RemoveAll(tmpDir)
			return EditSessionReadyMsg{Err: err}
		}

		// Write the shadow file for change detection.
		if err := os.WriteFile(shadowPath, buf.Bytes(), 0o644); err != nil {
			os.RemoveAll(tmpDir)
			return EditSessionReadyMsg{Err: err}
		}

		// Stat the remote file to record mtime.
		entry, err := remoteFS.Stat(remotePath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return EditSessionReadyMsg{Err: err}
		}

		session := &EditSession{
			RemotePath:  remotePath,
			TempPath:    tempPath,
			ShadowPath:  shadowPath,
			RemoteMTime: entry.ModTime,
			RemoteFS:    remoteFS,
		}

		return EditSessionReadyMsg{Session: session}
	}
}

// OpenEditor returns a tea.Exec command that opens the temp file in the user's editor.
// This suspends Bubble Tea and gives terminal control to the editor process.
func OpenEditor(session *EditSession) tea.Cmd {
	editorName := GetEditor()
	c := exec.Command(editorName, session.TempPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return EditorExitMsg{Session: session, Err: err}
	})
}

// EditorExitMsg is sent when the editor process exits.
type EditorExitMsg struct {
	Session *EditSession
	Err     error
}

// CheckAndUpload compares the edited file against the shadow copy and uploads if changed.
func CheckAndUpload(session *EditSession) tea.Cmd {
	return func() tea.Msg {
		// Read temp and shadow files.
		tempData, err := os.ReadFile(session.TempPath)
		if err != nil {
			return EditCompleteMsg{Session: session, Err: err}
		}
		shadowData, err := os.ReadFile(session.ShadowPath)
		if err != nil {
			return EditCompleteMsg{Session: session, Err: err}
		}

		// If identical, no changes — clean up.
		if bytes.Equal(tempData, shadowData) {
			Cleanup(session)
			return EditCompleteMsg{Session: session, Modified: false}
		}

		// File was modified — check for remote conflicts.
		entry, err := session.RemoteFS.Stat(session.RemotePath)
		if err != nil {
			// Can't stat remote — keep temp file, report error.
			return EditCompleteMsg{Session: session, Err: err}
		}

		if !entry.ModTime.Equal(session.RemoteMTime) {
			// Remote file changed since we downloaded — conflict.
			return ConflictMsg{Session: session}
		}

		// No conflict — upload.
		if err := uploadFile(session, tempData); err != nil {
			return EditCompleteMsg{Session: session, Err: err}
		}

		Cleanup(session)
		return EditCompleteMsg{Session: session, Modified: true}
	}
}

// ForceUpload uploads the edited file regardless of conflicts.
func ForceUpload(session *EditSession) tea.Cmd {
	return func() tea.Msg {
		tempData, err := os.ReadFile(session.TempPath)
		if err != nil {
			return UploadCompleteMsg{Session: session, Err: err}
		}
		if err := uploadFile(session, tempData); err != nil {
			return UploadCompleteMsg{Session: session, Err: err}
		}
		Cleanup(session)
		return UploadCompleteMsg{Session: session}
	}
}

// Cleanup removes the temp directory and all files in it.
func Cleanup(session *EditSession) {
	if session == nil || session.TempPath == "" {
		return
	}
	os.RemoveAll(filepath.Dir(session.TempPath))
}

// GetEditor returns the user's preferred editor from environment variables.
func GetEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vi"
}

// EditLocal returns a tea.Exec command to open a local file directly in the editor.
func EditLocal(path string) tea.Cmd {
	editorName := GetEditor()
	c := exec.Command(editorName, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return EditCompleteMsg{Modified: err == nil, Err: err}
	})
}

// uploadFile writes data to the remote path via the session's filesystem.
func uploadFile(session *EditSession, data []byte) error {
	return session.RemoteFS.Write(session.RemotePath, bytes.NewReader(data), 0o644)
}
