// Package expiry implements the expiry & deprecation watchlist (AXE-007):
// one report, sorted by days remaining, of everything in the account that
// breaks on a calendar date — certificate expiry, Lambda runtime
// deprecations, EKS version end-of-support, RDS maintenance and CA
// certificates, and overdue secret rotations. Calendar-driven breakage is
// fully predictable; this turns it into a weekly ritual instead of a page.
//
// All analysis is pure: collectors (collect.go) fetch snapshots, the check
// builders here turn them into Items.
package expiry

import (
	"fmt"
	"sort"
	"time"
)

// Item is one upcoming (or already-passed) deadline.
type Item struct {
	Date     time.Time `json:"date"`     // when it breaks
	Days     int       `json:"days"`     // until Date; negative = already passed
	Kind     string    `json:"kind"`     // what kind of deadline
	Resource string    `json:"resource"` // the affected resource
	Region   string    `json:"region"`
	Detail   string    `json:"detail"` // what breaks and the action to take
}

// daysUntil counts whole calendar days (UTC) from now to t, negative when t
// is past. Counting on calendar-day boundaries rather than raw 24h spans means
// "expires today" is 0, "expired yesterday" is -1, and a deadline 47 hours out
// reads 2 — independent of the time of day. (Truncating the raw difference
// toward zero, as a naive hours/24 does, would report an item that lapsed
// earlier today as 0 instead of a negative "already passed", and would
// under-count every non-midnight deadline.)
func daysUntil(now, t time.Time) int {
	now, t = now.UTC(), t.UTC()
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	tDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(tDay.Sub(nowDay) / (24 * time.Hour))
}

// ---------------------------------------------------------------------------
// Snapshot input types (filled by collect.go, hand-built in tests).
// ---------------------------------------------------------------------------

// Certificate is an ACM certificate or a legacy IAM server certificate.
type Certificate struct {
	Name     string // domain name (ACM) or certificate name (IAM)
	ARN      string
	NotAfter time.Time
	InUse    bool   // ACM only; IAM server certs report false
	Source   string // "acm" or "iam"
}

// Function is a Lambda function and its runtime.
type Function struct {
	Name    string
	Runtime string
}

// Cluster is an EKS cluster and its Kubernetes version.
type Cluster struct {
	Name    string
	Version string
}

// DBInstance is an RDS instance and its CA certificate.
type DBInstance struct {
	ID       string
	CACertID string
}

// Maintenance is one pending RDS maintenance action.
type Maintenance struct {
	Resource         string // resource identifier (ARN tail)
	Action           string
	Description      string
	AutoAppliedAfter time.Time // zero when unset
	ForcedApply      time.Time // zero when unset
	CurrentApply     time.Time // zero when unset
}

// Secret is a Secrets Manager secret's rotation state.
type Secret struct {
	Name            string
	RotationEnabled bool
	NextRotation    time.Time // zero when AWS did not report one
	LastRotated     time.Time // zero when never rotated
	RotateAfterDays int64     // rotation rule, 0 when unset
}

// ---------------------------------------------------------------------------
// Check builders — pure functions from snapshots to Items.
// ---------------------------------------------------------------------------

// CertItems flags certificates by their NotAfter date.
func CertItems(now time.Time, region string, certs []Certificate) []Item {
	var out []Item
	for _, c := range certs {
		if c.NotAfter.IsZero() {
			continue
		}
		kind := "ACM certificate expires"
		detail := "renew or re-issue the certificate before it expires"
		if c.Source == "iam" {
			kind = "IAM server certificate expires"
			detail = "legacy IAM server certificate — replace it (ideally migrate to ACM)"
		} else if c.InUse {
			detail = "certificate is in use — renew or re-issue before it expires"
		} else {
			detail = "certificate is not attached to anything — renew it or let it lapse deliberately"
		}
		out = append(out, Item{
			Date:     c.NotAfter,
			Days:     daysUntil(now, c.NotAfter),
			Kind:     kind,
			Resource: c.Name,
			Region:   region,
			Detail:   fmt.Sprintf("%s (expires %s)", detail, c.NotAfter.Format("2006-01-02")),
		})
	}
	return out
}

// LambdaItems flags functions whose runtime has an announced deprecation
// date (past dates included — a deprecated runtime blocks updates).
func LambdaItems(now time.Time, region string, fns []Function) []Item {
	var out []Item
	for _, f := range fns {
		date, known := lambdaRuntimeDeprecation[f.Runtime]
		if !known {
			continue
		}
		verb := "is deprecated on"
		if !date.After(now) {
			verb = "was deprecated on"
		}
		out = append(out, Item{
			Date:     date,
			Days:     daysUntil(now, date),
			Kind:     "Lambda runtime deprecated",
			Resource: fmt.Sprintf("%s (%s)", f.Name, f.Runtime),
			Region:   region,
			Detail:   fmt.Sprintf("runtime %s %s %s — update the function's runtime", f.Runtime, verb, date.Format("2006-01-02")),
		})
	}
	return out
}

// EKSItems flags clusters whose Kubernetes version has a known end of
// standard support.
func EKSItems(now time.Time, region string, clusters []Cluster) []Item {
	var out []Item
	for _, c := range clusters {
		date, known := eksEndOfStandardSupport[c.Version]
		if !known {
			continue
		}
		out = append(out, Item{
			Date:     date,
			Days:     daysUntil(now, date),
			Kind:     "EKS version end of standard support",
			Resource: fmt.Sprintf("%s (%s)", c.Name, c.Version),
			Region:   region,
			Detail:   fmt.Sprintf("standard support for %s ends %s — upgrade the cluster (extended support bills extra)", c.Version, date.Format("2006-01-02")),
		})
	}
	return out
}

// RDSItems flags expired CA certificates and pending maintenance actions.
func RDSItems(now time.Time, region string, instances []DBInstance, maint []Maintenance) []Item {
	var out []Item
	for _, db := range instances {
		date, expired := rdsCAExpiry[db.CACertID]
		if !expired {
			continue
		}
		out = append(out, Item{
			Date:     date,
			Days:     daysUntil(now, date),
			Kind:     "RDS CA certificate expired",
			Resource: db.ID,
			Region:   region,
			Detail:   fmt.Sprintf("instance still uses %s (expired %s) — rotate to a current CA (rds-ca-rsa2048-g1 or newer)", db.CACertID, date.Format("2006-01-02")),
		})
	}
	for _, m := range maint {
		date := m.ForcedApply
		urgency := "will be force-applied"
		if date.IsZero() {
			date = m.AutoAppliedAfter
			urgency = "auto-applies after"
		}
		if date.IsZero() {
			date = m.CurrentApply
			urgency = "applies"
		}
		if date.IsZero() {
			continue // no scheduled date yet — nothing to count down to
		}
		desc := m.Description
		if desc == "" {
			desc = m.Action
		}
		out = append(out, Item{
			Date:     date,
			Days:     daysUntil(now, date),
			Kind:     "RDS pending maintenance",
			Resource: m.Resource,
			Region:   region,
			Detail:   fmt.Sprintf("%s %s %s — schedule a maintenance window before AWS picks one", desc, urgency, date.Format("2006-01-02")),
		})
	}
	return out
}

// SecretItems flags rotation-enabled secrets whose next rotation is overdue.
func SecretItems(now time.Time, region string, secrets []Secret) []Item {
	var out []Item
	for _, s := range secrets {
		if !s.RotationEnabled {
			continue
		}
		due := s.NextRotation
		if due.IsZero() && !s.LastRotated.IsZero() && s.RotateAfterDays > 0 {
			due = s.LastRotated.Add(time.Duration(s.RotateAfterDays) * 24 * time.Hour)
		}
		if due.IsZero() || due.After(now) {
			continue // not due yet (upcoming rotations are routine, not findings)
		}
		out = append(out, Item{
			Date:     due,
			Days:     daysUntil(now, due),
			Kind:     "Secret rotation overdue",
			Resource: s.Name,
			Region:   region,
			Detail:   fmt.Sprintf("rotation was due %s and has not happened — check the rotation Lambda and its errors", due.Format("2006-01-02")),
		})
	}
	return out
}

// Filter keeps items within the horizon: everything already past, plus
// upcoming items with at most `within` days remaining.
func Filter(items []Item, within int) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Days <= within {
			out = append(out, it)
		}
	}
	return out
}

// Sort orders items soonest-first (already-passed items lead), with stable
// region/resource tie-breaks.
func Sort(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Days != b.Days {
			return a.Days < b.Days
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		return a.Resource < b.Resource
	})
}
