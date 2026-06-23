package xref

import "fmt"

// Deletion-risk scoring (#398): a deterministic, no-AI estimate of how risky it
// is to delete/change the queried resource, derived purely from how many things
// directly depend on it ("used by" at depth 1). Like every other signal here it
// is scoped — it can only reflect the relationship types that were collected
// successfully, so a partial scan is flagged as possibly understating risk (§8:
// under-warn, never mis-warn).

type RiskLevel string

const (
	RiskLow    RiskLevel = "LOW"
	RiskMedium RiskLevel = "MEDIUM"
	RiskHigh   RiskLevel = "HIGH"
)

// RiskAssessment is a level plus a one-line rationale.
type RiskAssessment struct {
	Level  RiskLevel `json:"level"`
	Reason string    `json:"reason"`
}

// AssessRisk scores the blast radius of deleting the queried resource from its
// direct (depth-1) dependents. The thresholds are deliberately simple and
// explainable: none → LOW, a few → MEDIUM, many → HIGH.
func AssessRisk(res RelatedResult) RiskAssessment {
	direct := 0
	for _, l := range res.UsedBy {
		if l.Depth == 1 {
			direct++
		}
	}

	var a RiskAssessment
	switch {
	case direct == 0:
		a = RiskAssessment{RiskLow, "nothing collected references this resource"}
	case direct <= 2:
		a = RiskAssessment{RiskMedium, fmt.Sprintf("%d resource(s) directly depend on this", direct)}
	default:
		a = RiskAssessment{RiskHigh, fmt.Sprintf("%d resources directly depend on this", direct)}
	}
	if res.Partial {
		a.Reason += "; scan was partial, so risk may be understated"
	}
	return a
}
