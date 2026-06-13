package cmd

import "testing"

// Every TUI-capable command must accept --tui, so invocation is uniform:
// summary/audit/bill toggle a TUI off their CLI default, while vpc/s3/cw are
// always interactive but still accept the flag for consistency (#138).
func TestTUIFlagConsistency(t *testing.T) {
	want := map[string]bool{
		"summary": false,
		"audit":   false,
		"bill":    false,
		"vpc":     false,
		"s3":      false,
		"cw":      false,
	}
	for _, c := range rootCmd.Commands() {
		if _, ok := want[c.Name()]; !ok {
			continue
		}
		if c.Flags().Lookup("tui") == nil {
			t.Errorf("%q is missing the --tui flag", c.Name())
		}
		want[c.Name()] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("command %q not found under root", name)
		}
	}
}
