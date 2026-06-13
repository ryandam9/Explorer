package csvexport

import (
	"os"
	"reflect"
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

func TestSanitizeField_FormulaInjection(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"normal", "normal"},
		{"my-instance", "my-instance"}, // interior dash is fine; only the leading char matters
		{"=1+2", "'=1+2"},
		{"+44", "'+44"},
		{"-5", "'-5"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"=cmd|'/C calc'!A1", "'=cmd|'/C calc'!A1"},
		{"\tleadingtab", "'\tleadingtab"},
		{"\rleadingcr", "'\rleadingcr"},
		{"arn:aws:s3:::bucket", "arn:aws:s3:::bucket"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeRow(t *testing.T) {
	in := []string{"ok", "=danger", "-1"}
	want := []string{"ok", "'=danger", "'-1"}
	got := SanitizeRow(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SanitizeRow(%q) = %q, want %q", in, got, want)
	}
	if in[1] != "=danger" {
		t.Errorf("SanitizeRow mutated its input: %q", in)
	}
}
