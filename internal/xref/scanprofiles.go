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
// It mirrors the keys used in collectRegion and the global phases. CloudWatch
// is split into "cloudwatch" (alarm actions) and "logs" (the per-log-group
// subscription-filter sweep) so the throttle-prone Logs collector can be
// excluded on its own (--scan exclude:logs) without also dropping alarms.
var validServices = map[string]bool{
	"iam": true, "s3": true, "networking": true, "lambda": true, "ec2": true,
	"rds": true, "secretsmanager": true, "sqs": true, "ecs": true, "eks": true,
	"elbv2": true, "efs": true, "sns": true, "events": true, "states": true,
	"kinesis": true, "apigateway": true, "ec2-endpoints": true, "kms": true,
	"dynamodb": true, "elasticache": true, "redshift": true,
	"cloudwatch": true, "logs": true,
}

// serviceAliases expand a friendly umbrella token to the concrete collector
// keys it covers. "observability" historically meant both CloudWatch alarms and
// Logs; keeping it as an alias preserves `--scan observability` while letting
// callers target the throttle-prone Logs sweep on its own (`--scan logs`,
// `--scan exclude:logs`).
var serviceAliases = map[string][]string{
	"observability": {"cloudwatch", "logs"},
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
// service set; an "exclude:a,b" value runs everything *except* the named
// services; otherwise the value is treated as a comma-separated explicit list
// of service keys (validated). Service names may be aliases (e.g.
// "observability" → cloudwatch + logs).
func ParseScan(s string) (map[string]bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "full" {
		return nil, nil
	}
	if rest, ok := strings.CutPrefix(s, "exclude:"); ok {
		excluded, err := resolveServiceTokens(rest)
		if err != nil {
			return nil, err
		}
		if len(excluded) == 0 {
			return nil, fmt.Errorf("exclude: needs at least one service to exclude (valid: %s)", validServicesList())
		}
		out := map[string]bool{}
		for svc := range validServices {
			if !excluded[svc] {
				out[svc] = true
			}
		}
		return out, nil
	}
	if p, ok := scanProfiles[s]; ok {
		return p, nil
	}
	out, err := resolveServiceTokens(s)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// resolveServiceTokens parses a comma-separated list of service names (or
// aliases) into a set of concrete collector keys, validating each token and
// expanding any aliases.
func resolveServiceTokens(list string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if alias, ok := serviceAliases[part]; ok {
			for _, svc := range alias {
				out[svc] = true
			}
			continue
		}
		if !validServices[part] {
			return nil, fmt.Errorf("unknown scan service %q (valid: %s, or a profile: full|fast|security|eventing|network)", part, validServicesList())
		}
		out[part] = true
	}
	return out, nil
}

func validServicesList() string {
	names := make([]string, 0, len(validServices)+len(serviceAliases))
	for n := range validServices {
		names = append(names, n)
	}
	for n := range serviceAliases {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
