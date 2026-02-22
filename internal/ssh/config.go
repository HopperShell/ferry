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
