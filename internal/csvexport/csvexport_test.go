package csvexport

import (
	"os"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path, err := Write(dir, "inventory-All", []string{"A", "B"}, [][]string{{"1", "with,comma"}, {"2", "plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, dir) || !strings.HasSuffix(path, ".csv") {
		t.Fatalf("unexpected path %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "A,B\n") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, `"with,comma"`) {
		t.Errorf("comma value should be quoted: %q", got)
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("s3-my/bucket name"); got != "s3-my-bucket-name" {
		t.Errorf("sanitize = %q", got)
	}
	if got := sanitize(""); got != "export" {
		t.Errorf("sanitize empty = %q", got)
	}
}
