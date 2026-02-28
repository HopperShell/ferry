package fs

import (
	"testing"
	"time"
)

func TestS3FS_toKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{"root no prefix", "", "/", ""},
		{"file no prefix", "", "/docs/readme.md", "docs/readme.md"},
		{"root with prefix", "data/", "/", "data"},
		{"file with prefix", "data/", "/docs/readme.md", "data/docs/readme.md"},
		{"no leading slash", "", "docs/readme.md", "docs/readme.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S3FS{prefix: tt.prefix}
			got := s.toKey(tt.path)
			if got != tt.want {
				t.Errorf("toKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestS3FS_fromKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"file no prefix", "", "docs/readme.md", "/docs/readme.md"},
		{"file with prefix", "data/", "data/docs/readme.md", "/docs/readme.md"},
		{"root", "", "", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S3FS{prefix: tt.prefix}
			got := s.fromKey(tt.key)
			if got != tt.want {
				t.Errorf("fromKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestS3FS_objectMtime(t *testing.T) {
	s := &S3FS{}
	now := time.Now()

	// Custom metadata takes precedence.
	meta := map[string]string{mtimeMetadataKey: "1700000000"}
	got := s.objectMtime(&now, meta)
	if got.Unix() != 1700000000 {
		t.Errorf("expected unix 1700000000, got %d", got.Unix())
	}

	// Falls back to LastModified.
	got = s.objectMtime(&now, nil)
	if !got.Equal(now) {
		t.Errorf("expected %v, got %v", now, got)
	}

	// Returns zero time when no info.
	got = s.objectMtime(nil, nil)
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}
