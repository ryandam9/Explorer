package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docServiceRow matches a collector-matrix table row, capturing the service
// name in the first column's backticks: "| `ec2` | regional | …".
var docServiceRow = regexp.MustCompile("(?m)^\\|\\s*`([a-z0-9]+)`\\s*\\|")

// TestCollectorMatrixMatchesRegistry keeps docs/collectors.md honest: every
// registered collector must have a row, and every documented service must be a
// real registered collector. This is what lets the doc claim it cannot silently
// drift (the "29 services ≠ full coverage" guarantee).
func TestCollectorMatrixMatchesRegistry(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range defaultRegistry().GetAll() {
		registered[c.Name()] = true
	}

	path := filepath.Join("..", "..", "docs", "collectors.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	documented := map[string]bool{}
	for _, m := range docServiceRow.FindAllStringSubmatch(string(data), -1) {
		documented[m[1]] = true
	}

	for name := range registered {
		if !documented[name] {
			t.Errorf("collector %q is registered but missing from docs/collectors.md", name)
		}
	}
	for name := range documented {
		if !registered[name] {
			t.Errorf("docs/collectors.md lists %q, which is not a registered collector", name)
		}
	}

	if len(documented) != len(registered) {
		t.Errorf("documented %d services, registered %d: %s",
			len(documented), len(registered), strings.Join(diff(registered, documented), ", "))
	}
}

// diff returns the symmetric difference of two name sets, sorted, for a readable
// failure message.
func diff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, "-"+k)
		}
	}
	for k := range b {
		if !a[k] {
			out = append(out, "+"+k)
		}
	}
	sort.Strings(out)
	return out
}
