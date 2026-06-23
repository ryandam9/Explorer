package xref

import "testing"

func TestParseScan(t *testing.T) {
	// full / empty → nil (scan everything).
	for _, in := range []string{"", "full", "FULL", "  "} {
		got, err := ParseScan(in)
		if err != nil || got != nil {
			t.Errorf("ParseScan(%q) = %v, %v; want nil,nil", in, got, err)
		}
	}
	// Named profile expands to a set.
	sec, err := ParseScan("security")
	if err != nil || !sec["iam"] || !sec["kms"] || sec["dynamodb"] {
		t.Errorf("security profile = %v, %v", sec, err)
	}
	// Explicit list.
	got, err := ParseScan("iam, kms,lambda")
	if err != nil || !got["iam"] || !got["kms"] || !got["lambda"] || len(got) != 3 {
		t.Errorf("explicit list = %v, %v", got, err)
	}
	// Unknown service errors.
	if _, err := ParseScan("iam,bogus"); err == nil {
		t.Errorf("unknown service should error")
	}
}

func TestCheckedTypesFor_NarrowsWithScan(t *testing.T) {
	full := CheckedTypes(KindKMSKey)
	if len(full) == 0 {
		t.Fatal("KMS should have a full checked-types list")
	}

	// A scan that excludes most services narrows the footer, and never lists a
	// reference type whose service wasn't scanned.
	services := set("ec2", "rds") // EBS + RDS encryption only
	got := CheckedTypesFor(KindKMSKey, services)
	if len(got) == 0 || len(got) >= len(full) {
		t.Errorf("narrowed list (%d) should be a non-empty subset of full (%d): %v", len(got), len(full), got)
	}
	for _, label := range got {
		if label != "EBS volume encryption" && label != "RDS instance encryption" {
			t.Errorf("unexpected checked type for ec2+rds scan: %q", label)
		}
	}

	// nil services → full list (unchanged behavior).
	if len(CheckedTypesFor(KindKMSKey, nil)) != len(full) {
		t.Errorf("nil services should yield the full list")
	}
}
