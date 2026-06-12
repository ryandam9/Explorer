// Package findings is the shared findings ("linter") framework: a Finding
// describes one detected issue with a severity, a stable check ID, and an
// optional monthly cost estimate. Analysis functions are pure — they reason
// over snapshots of collected AWS data and never call AWS themselves — so
// every check is unit-testable with fixtures.
//
// The package generalizes the per-VPC linter in internal/vpctui to
// account-wide audits (cost/waste today; security and messaging checks are
// planned to join it — see docs/enhancement-roadmap.md).
package findings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Severity ranks a finding. Higher values sort first.
type Severity int

const (
	SevInfo Severity = iota
	SevWarning
	SevCritical
)

func (s Severity) String() string {
	switch s {
	case SevCritical:
		return "CRITICAL"
	case SevWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}

// Badge returns the severity with its color dot, as used in tables.
func (s Severity) Badge() string {
	switch s {
	case SevCritical:
		return "🔴 CRITICAL"
	case SevWarning:
		return "🟡 WARNING"
	default:
		return "🔵 INFO"
	}
}

// MarshalJSON renders the severity as its name, not a bare int.
func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON parses a severity name back, so serialized findings
// round-trip.
func (s *Severity) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	switch name {
	case "CRITICAL":
		*s = SevCritical
	case "WARNING":
		*s = SevWarning
	case "INFO":
		*s = SevInfo
	default:
		return fmt.Errorf("unknown severity %q", name)
	}
	return nil
}

// Finding is a single detected issue.
type Finding struct {
	// ID is the stable identifier of the check that fired (e.g.
	// "COST-EBS-001"). Stable IDs let findings be referenced, suppressed,
	// and mapped to SARIF rules.
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Service  string   `json:"service"`
	Region   string   `json:"region"`
	Resource string   `json:"resource"` // the offending resource ID/name (or "-")
	ARN      string   `json:"arn,omitempty"`
	Title    string   `json:"title"`  // short one-line summary
	Detail   string   `json:"detail"` // why this was flagged
	Fix      string   `json:"fix"`    // suggested remediation
	// EstMonthlyUSD is the approximate monthly cost of the flagged waste
	// (or the saving from fixing it). Zero means no estimate.
	EstMonthlyUSD float64 `json:"estMonthlyUSD"`
}

// Sort orders findings for display: most severe first, then largest estimated
// cost, then region and resource for stable output.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if a.EstMonthlyUSD != b.EstMonthlyUSD {
			return a.EstMonthlyUSD > b.EstMonthlyUSD
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		return a.Resource < b.Resource
	})
}

// TotalMonthlyUSD sums the cost estimates across findings.
func TotalMonthlyUSD(fs []Finding) float64 {
	var total float64
	for _, f := range fs {
		total += f.EstMonthlyUSD
	}
	return total
}

// CountBySeverity tallies findings per severity.
func CountBySeverity(fs []Finding) (crit, warn, info int) {
	for _, f := range fs {
		switch f.Severity {
		case SevCritical:
			crit++
		case SevWarning:
			warn++
		default:
			info++
		}
	}
	return
}

// Summary renders a one-line severity tally ("1 critical, 2 warning, 0 info").
func Summary(fs []Finding) string {
	crit, warn, info := CountBySeverity(fs)
	return fmt.Sprintf("%d critical, %d warning, %d info", crit, warn, info)
}

// ParseSeverity parses a severity name ("critical", "warning", "info"),
// case-insensitively. Used by the --fail-on flag.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return SevCritical, nil
	case "warning":
		return SevWarning, nil
	case "info":
		return SevInfo, nil
	default:
		return SevInfo, fmt.Errorf("unknown severity %q (use critical, warning or info)", s)
	}
}

// Drop removes findings whose check ID is in ignore, preserving order. A nil
// or empty ignore set returns the input unchanged.
func Drop(fs []Finding, ignore map[string]bool) []Finding {
	if len(ignore) == 0 {
		return fs
	}
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		if !ignore[f.ID] {
			out = append(out, f)
		}
	}
	return out
}

// AnyAtOrAbove reports whether any finding is at least as severe as min.
// Used by the --fail-on exit-code gate.
func AnyAtOrAbove(fs []Finding, min Severity) bool {
	for _, f := range fs {
		if f.Severity >= min {
			return true
		}
	}
	return false
}
