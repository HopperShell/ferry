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
