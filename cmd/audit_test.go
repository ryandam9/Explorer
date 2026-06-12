package cmd

import (
	"strings"
	"testing"
)

func TestValidateAuditCategories(t *testing.T) {
	if err := validateAuditCategories(nil); err != nil {
		t.Errorf("nil categories should be valid: %v", err)
	}
	if err := validateAuditCategories([]string{"cost"}); err != nil {
		t.Errorf("cost should be valid: %v", err)
	}
	if err := validateAuditCategories([]string{"COST"}); err != nil {
		t.Errorf("category matching should be case-insensitive: %v", err)
	}
	err := validateAuditCategories([]string{"cost", "bogus"})
	if err == nil {
		t.Fatal("unknown category should be rejected")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "cost") {
		t.Errorf("error should name the bad value and the available ones: %v", err)
	}
}
