// Package quotas implements the service-quota dashboard (AXE-017): a curated
// view of the ~20 AWS limits that actually page people — vCPUs, Elastic IPs,
// VPCs, ENIs, Lambda concurrency, RDS instances, EBS storage — with their
// real (account-specific) limits and current usage.
//
// It generalizes the VPC linter's hardcoded-default checks: limits come from
// servicequotas:GetServiceQuota (the applied value, so account-specific
// increases are reflected), falling back to the AWS default when the applied
// value is unavailable. Usage comes from the quota's own CloudWatch usage
// metric when AWS publishes one; otherwise the limit is shown without a
// percentage rather than guessed.
//
// The analysis (Evaluate/Filter/Sort) is pure and fixture-tested; collect.go
// does the AWS calls.
package quotas

import "sort"

// Quota is one collected limit-and-usage pair (filled by collect.go or
// hand-built in tests).
type Quota struct {
	Name        string  // human-readable quota name (from AWS)
	Service     string  // service code, e.g. "ec2", "vpc", "lambda"
	Region      string  // "global" for account-level quotas
	Limit       float64 // the applied (or default) limit value
	Unit        string  // unit reported by Service Quotas (often "None")
	Used        float64 // current usage; meaningful only when UsageKnown
	UsageKnown  bool    // false when AWS publishes no usage metric for the quota
	FromDefault bool    // true when Limit came from the AWS default, not the applied value
}

// Status is the headline severity of a quota row.
type Status string

const (
	StatusOK       Status = "ok"
	StatusWarn     Status = "warn"
	StatusCritical Status = "critical"
	StatusUnknown  Status = "unknown" // limit known, usage not
)

// Row is one evaluated dashboard line.
type Row struct {
	Name        string   `json:"name"`
	Service     string   `json:"service"`
	Region      string   `json:"region"`
	Used        *float64 `json:"used,omitempty"`
	Limit       float64  `json:"limit"`
	Unit        string   `json:"unit,omitempty"`
	Percent     *float64 `json:"percent,omitempty"`
	Status      Status   `json:"status"`
	UsageKnown  bool     `json:"usage_known"`
	FromDefault bool     `json:"from_default,omitempty"`
}

// Evaluate turns collected quotas into rows: it computes utilization and
// assigns a status against warnThreshold (a percentage, e.g. 80). A quota
// with no usage metric is StatusUnknown — its limit is informative but its
// headroom cannot be assessed. Rows are returned sorted (see Sort).
func Evaluate(quotas []Quota, warnThreshold float64) []Row {
	rows := make([]Row, 0, len(quotas))
	for _, q := range quotas {
		r := Row{
			Name:        q.Name,
			Service:     q.Service,
			Region:      q.Region,
			Limit:       q.Limit,
			Unit:        q.Unit,
			UsageKnown:  q.UsageKnown,
			FromDefault: q.FromDefault,
		}
		if !q.UsageKnown {
			r.Status = StatusUnknown
		} else {
			used := q.Used
			r.Used = &used
			var pct float64
			if q.Limit > 0 {
				pct = used / q.Limit * 100
			}
			p := pct
			r.Percent = &p
			switch {
			case pct >= 100:
				r.Status = StatusCritical
			case pct >= warnThreshold:
				r.Status = StatusWarn
			default:
				r.Status = StatusOK
			}
		}
		rows = append(rows, r)
	}
	Sort(rows)
	return rows
}

// Filter narrows rows to those worth attention: when minPercent > 0, only
// usage-known rows at or above that utilization are kept (everything critical
// is above 100% and so always kept). minPercent <= 0 keeps every row,
// including those whose usage is unknown. It returns the kept rows and the
// number of rows dropped, so the caller can note "N quotas hidden".
func Filter(rows []Row, minPercent float64) ([]Row, int) {
	if minPercent <= 0 {
		return rows, 0
	}
	kept := make([]Row, 0, len(rows))
	dropped := 0
	for _, r := range rows {
		if r.UsageKnown && r.Percent != nil && *r.Percent >= minPercent {
			kept = append(kept, r)
		} else {
			dropped++
		}
	}
	return kept, dropped
}

// statusRank orders statuses most-severe first for sorting.
func statusRank(s Status) int {
	switch s {
	case StatusCritical:
		return 0
	case StatusWarn:
		return 1
	case StatusOK:
		return 2
	default: // unknown
		return 3
	}
}

// Sort orders rows by severity, then utilization (highest first), then name —
// so the things closest to exhaustion lead and output is deterministic.
func Sort(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		pa, pb := pctOrNeg(a), pctOrNeg(b)
		if pa != pb {
			return pa > pb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Region < b.Region
	})
}

func pctOrNeg(r Row) float64 {
	if r.Percent == nil {
		return -1
	}
	return *r.Percent
}
