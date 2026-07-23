package ui

import "testing"

func TestSmartCaseRegexp(t *testing.T) {
	cases := []struct {
		pattern string
		input   string
		want    bool
	}{
		// All-lowercase pattern: case-insensitive.
		{"error", "ERROR db timeout", true},
		{"error", "Error two", true},
		{"error", "error three", true},
		{"err.r", "ERROR", true},
		// Any literal uppercase: case-sensitive as typed.
		{"Error", "Error two", true},
		{"Error", "ERROR one", false},
		{"ERROR", "error three", false},
		// An uppercase letter in a regex escape is not a literal and keeps
		// the pattern case-insensitive.
		{`\Serror`, "XERROR", true},
		{`e\Dror`, "EXROR", true},
	}
	for _, c := range cases {
		re, err := SmartCaseRegexp(c.pattern)
		if err != nil {
			t.Fatalf("SmartCaseRegexp(%q) error: %v", c.pattern, err)
		}
		if got := re.MatchString(c.input); got != c.want {
			t.Errorf("SmartCaseRegexp(%q).MatchString(%q) = %v, want %v", c.pattern, c.input, got, c.want)
		}
	}
	if _, err := SmartCaseRegexp("a("); err == nil {
		t.Error("invalid pattern must return an error")
	}
}
