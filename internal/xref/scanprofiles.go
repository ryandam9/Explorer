package xref

import (
	"fmt"
	"sort"
	"strings"
)

// Scan profiles (#393) trade coverage for speed: the related scan is broad by
// design, but a focused question rarely needs every service. A profile (or an
// explicit service list) restricts which collectors run — and, via
// CheckedTypesFor, narrows the "reference types checked" footer so the honesty
// contract still holds.

// validServices is the set of collector service keys a --scan list may name.
// It mirrors the keys used in collectRegion and the global phases.
var validServices = map[string]bool{
	"iam": true, "s3": true, "networking": true, "lambda": true, "ec2": true,
	"rds": true, "secretsmanager": true, "sqs": true, "ecs": true, "eks": true,
	"elbv2": true, "efs": true, "sns": true, "events": true, "states": true,
	"kinesis": true, "apigateway": true, "ec2-endpoints": true, "kms": true,
	"dynamodb": true, "elasticache": true, "redshift": true, "observability": true,
}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var scanProfiles = map[string]map[string]bool{
	"fast":     set("iam", "lambda", "ec2", "s3", "rds", "kms", "sqs", "sns"),
	"security": set("iam", "kms", "ec2", "secretsmanager", "lambda", "ecs", "rds"),
	"eventing": set("s3", "events", "sns", "sqs", "lambda", "states", "kinesis"),
	"network":  set("ec2", "elbv2", "apigateway", "networking", "ec2-endpoints"),
}

// ParseScan resolves a --scan value to the set of collector services to run.
// "" or "full" means everything (nil set); a known profile name expands to its
// service set; otherwise the value is treated as a comma-separated explicit
// list of service keys (validated).
func ParseScan(s string) (map[string]bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "full" {
		return nil, nil
	}
	if p, ok := scanProfiles[s]; ok {
		return p, nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !validServices[part] {
			return nil, fmt.Errorf("unknown scan service %q (valid: %s, or a profile: full|fast|security|eventing|network)", part, validServicesList())
		}
		out[part] = true
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validServicesList() string {
	names := make([]string, 0, len(validServices))
	for n := range validServices {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
