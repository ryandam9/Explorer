package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestAuditDocCoversAllCategories guards against the audit-category drift the
// external review flagged (#360): the docs said "four categories" while the
// command implements eight. auditCategories (cmd/audit.go) is the source of
// truth — every category must be documented, and the stale phrasings must stay
// gone.
func TestAuditDocCoversAllCategories(t *testing.T) {
	b, err := os.ReadFile("../docs/audit.md")
	if err != nil {
		t.Fatalf("read docs/audit.md: %v", err)
	}
	doc := strings.ToLower(string(b))

	for _, c := range auditCategories {
		if !strings.Contains(doc, c) {
			t.Errorf("docs/audit.md does not document the %q audit category", c)
		}
	}
	for _, stale := range []string{"four categories", "in two categories"} {
		if strings.Contains(doc, stale) {
			t.Errorf("stale phrasing %q is still in docs/audit.md", stale)
		}
	}
}

// TestAuditLongMentionsCategories keeps the command's help text in sync with the
// implemented category list.
func TestAuditLongMentionsCategories(t *testing.T) {
	long := strings.ToLower(auditCmd.Long)
	for _, c := range auditCategories {
		if !strings.Contains(long, c) {
			t.Errorf("audit --help (Long) does not mention the %q category", c)
		}
	}
}
