package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigArgPath(t *testing.T) {
	dir := t.TempDir()

	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("ui: {}\n"), 0o644); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	missing := filepath.Join(dir, "absent.yaml")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"existing file passes through", existing, existing},
		{"missing file is dropped", missing, ""},
		{"directory is dropped", dir, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConfigArgPath(tt.in); got != tt.want {
				t.Errorf("ConfigArgPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
