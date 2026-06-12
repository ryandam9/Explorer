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

func TestParseIgnoreIDs(t *testing.T) {
	got, err := parseIgnoreIDs(nil)
	if err != nil || got != nil {
		t.Errorf("empty ignore = %v, %v", got, err)
	}

	got, err = parseIgnoreIDs([]string{"cost-ebs-002", " COST-EIP-001 "})
	if err != nil {
		t.Fatalf("valid IDs rejected: %v", err)
	}
	if !got["COST-EBS-002"] || !got["COST-EIP-001"] {
		t.Errorf("ignore set = %v (IDs should be normalized to upper case)", got)
	}

	_, err = parseIgnoreIDs([]string{"COST-TYPO-999"})
	if err == nil {
		t.Fatal("unknown check ID should be rejected")
	}
	if !strings.Contains(err.Error(), "COST-TYPO-999") || !strings.Contains(err.Error(), "COST-EBS-001") {
		t.Errorf("error should name the bad ID and the known ones: %v", err)
	}
}
