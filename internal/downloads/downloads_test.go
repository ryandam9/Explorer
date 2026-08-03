package downloads

import (
	"os"
	"path/filepath"
	"testing"
)

// tempHome points os.UserHomeDir at a fresh temp dir (HOME for Unix,
// USERPROFILE for Windows) and clears any configured override.
func tempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	Init("")
	t.Cleanup(func() { Init("") })
	return dir
}

func TestDirDefault(t *testing.T) {
	home := tempHome(t)
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	want := filepath.Join(home, ".aws_explorer", "downloads")
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("default download dir was not created: %v", err)
	}
}

func TestDirOverride(t *testing.T) {
	home := tempHome(t)

	Init("~/my-exports")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if want := filepath.Join(home, "my-exports"); dir != want {
		t.Errorf("tilde override = %q, want %q", dir, want)
	}

	abs := filepath.Join(home, "elsewhere")
	Init(abs)
	dir, err = Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if dir != abs {
		t.Errorf("absolute override = %q, want %q", dir, abs)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		t.Errorf("override dir was not created: %v", err)
	}

	// Whitespace-only override falls back to the default.
	Init("   ")
	dir, _ = Dir()
	if want := filepath.Join(home, ".aws_explorer", "downloads"); dir != want {
		t.Errorf("blank override = %q, want default %q", dir, want)
	}
}
