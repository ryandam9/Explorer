package auth

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListProfiles(t *testing.T) {
	dir := t.TempDir()
	creds := filepath.Join(dir, "credentials")
	cfg := filepath.Join(dir, "config")
	os.WriteFile(creds, []byte("[default]\naws_access_key_id=x\n\n[prod]\naws_access_key_id=y\n"), 0o600)
	os.WriteFile(cfg, []byte("[profile staging]\nregion=us-east-1\n\n[sso-session corp]\nsso_region=us-east-1\n\n[default]\nregion=us-west-2\n"), 0o600)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)
	t.Setenv("AWS_CONFIG_FILE", cfg)

	got := ListProfiles()
	want := []string{"default", "prod", "staging"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListProfiles() = %v, want %v (sso-session sections must be excluded)", got, want)
	}
}

func TestListProfilesMissingFiles(t *testing.T) {
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nope"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "nope"))
	if got := ListProfiles(); len(got) != 0 {
		t.Fatalf("expected no profiles for missing files, got %v", got)
	}
}
