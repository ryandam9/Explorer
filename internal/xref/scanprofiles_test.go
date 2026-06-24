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

func TestParseScan_Exclude(t *testing.T) {
	// exclude:logs runs everything except the CloudWatch Logs sweep, but keeps
	// CloudWatch alarms and the rest.
	got, err := ParseScan("exclude:logs")
	if err != nil {
		t.Fatalf("exclude:logs errored: %v", err)
	}
	if got["logs"] {
		t.Errorf("exclude:logs should drop logs, got %v", got)
	}
	if !got["cloudwatch"] || !got["iam"] || !got["lambda"] {
		t.Errorf("exclude:logs should keep every other service, got %v", got)
	}
	if len(got) != len(validServices)-1 {
		t.Errorf("exclude:logs should keep all but one service: got %d, want %d", len(got), len(validServices)-1)
	}

	// exclude:observability expands the alias and drops both cloudwatch + logs.
	got, err = ParseScan("exclude:observability")
	if err != nil {
		t.Fatalf("exclude:observability errored: %v", err)
	}
	if got["logs"] || got["cloudwatch"] {
		t.Errorf("exclude:observability should drop both cloudwatch and logs, got %v", got)
	}

	// An exclude naming an unknown service errors rather than silently scanning all.
	if _, err := ParseScan("exclude:bogus"); err == nil {
		t.Errorf("exclude with unknown service should error")
	}
	// Empty exclude list errors.
	if _, err := ParseScan("exclude:"); err == nil {
		t.Errorf("empty exclude list should error")
	}
}

func TestParseScan_ObservabilityAlias(t *testing.T) {
	got, err := ParseScan("observability")
	if err != nil {
		t.Fatalf("observability alias errored: %v", err)
	}
	if !got["cloudwatch"] || !got["logs"] || len(got) != 2 {
		t.Errorf("observability alias should expand to {cloudwatch, logs}, got %v", got)
	}
	// Alias mixed into an explicit list.
	got, err = ParseScan("iam,observability")
	if err != nil || !got["iam"] || !got["cloudwatch"] || !got["logs"] {
		t.Errorf("iam,observability = %v, %v", got, err)
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
