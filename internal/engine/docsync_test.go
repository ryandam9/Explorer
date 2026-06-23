package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDocsMentionCollectorCount guards against the service-count drift the
// external review flagged (#360): the docs claimed "15 service" collectors while
// the registry had 29. The registry is the source of truth; if the count changes
// the docs must move with it.
func TestDocsMentionCollectorCount(t *testing.T) {
	count := len(defaultRegistry().GetAll())
	want := fmt.Sprintf("%d service", count)
	for _, p := range []string{"../../README.md", "../../docs/architecture.md"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not mention %q — it has drifted from the registry's %d collectors", p, want, count)
		}
	}
}
