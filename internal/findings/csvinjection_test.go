package findings

import (
	"bytes"
	"strings"
	"testing"
)

// CSV cells that begin with =, +, -, @ (or tab/CR) are executed as formulas by
// spreadsheet apps. renderCSV must neutralize them like the other writers
// (#377, CLAUDE.md §13).
func TestRenderCSV_NeutralizesFormulaInjection(t *testing.T) {
	f := Finding{
		ID:       "TEST-001",
		Resource: "=cmd|'/C calc'!A1",
		Title:    "@SUM(A1)",
		Detail:   "+danger",
	}
	var buf bytes.Buffer
	if err := renderCSV(&buf, []Finding{f}, false); err != nil {
		t.Fatalf("renderCSV: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"'=cmd", "'@SUM(A1)", "'+danger"} {
		if !strings.Contains(out, want) {
			t.Errorf("dangerous cell not neutralized (missing %q):\n%s", want, out)
		}
	}
}
