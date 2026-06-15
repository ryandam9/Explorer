package ui

import (
	"strings"
	"testing"
)

func TestAboutViewContainsTitleAndBody(t *testing.T) {
	out := AboutView("Summary", "This page lists every resource in the account.", 60)
	if !strings.Contains(out, "Summary") {
		t.Errorf("AboutView output missing title:\n%s", out)
	}
	if !strings.Contains(out, "resource") {
		t.Errorf("AboutView output missing body text:\n%s", out)
	}
	if !strings.Contains(out, "close") {
		t.Errorf("AboutView output missing close hint:\n%s", out)
	}
}

func TestAboutWidthClamps(t *testing.T) {
	if got := AboutWidth(200); got != 76 {
		t.Errorf("AboutWidth(200) = %d, want 76 (capped)", got)
	}
	if got := AboutWidth(20); got != 32 {
		t.Errorf("AboutWidth(20) = %d, want 32 (floor)", got)
	}
	if got := AboutWidth(60); got != 48 {
		t.Errorf("AboutWidth(60) = %d, want 48", got)
	}
}
