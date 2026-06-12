// Package iamsim renders iam:SimulatePrincipalPolicy results as a
// step-by-step verdict in the path-tracer style: which policy allowed or
// denied the action, whether a permissions boundary was the limiting factor,
// and — always — the caveats about what the simulator does not evaluate.
package iamsim

import (
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// Decisions, as the simulator reports them.
const (
	DecisionAllowed      = "allowed"
	DecisionImplicitDeny = "implicitDeny"
	DecisionExplicitDeny = "explicitDeny"
)

// Statement names one policy statement the simulator matched.
type Statement struct {
	PolicyID   string `json:"policyId"`   // e.g. "app-s3-read" or "AdministratorAccess"
	PolicyType string `json:"policyType"` // "user" | "group" | "role" | "aws-managed" | "user-managed" | "resource" | "none"
}

// Verdict is the simulator's evaluation of one action.
type Verdict struct {
	Action          string      `json:"action"`
	Resource        string      `json:"resource,omitempty"`
	Decision        string      `json:"decision"` // allowed | implicitDeny | explicitDeny
	Matched         []Statement `json:"matchedStatements,omitempty"`
	MissingContext  []string    `json:"missingContextKeys,omitempty"`
	BoundaryAllowed *bool       `json:"allowedByPermissionsBoundary,omitempty"` // nil when no boundary applies
	OrgsAllowed     *bool       `json:"allowedByOrganizations,omitempty"`       // nil when no Organizations detail
}

// FromSDK reduces the simulator's evaluation results to Verdicts. Pure over
// its input — fixture-testable with hand-built results.
func FromSDK(results []types.EvaluationResult) []Verdict {
	out := make([]Verdict, 0, len(results))
	for _, r := range results {
		v := Verdict{
			Action:         aws.ToString(r.EvalActionName),
			Resource:       aws.ToString(r.EvalResourceName),
			Decision:       string(r.EvalDecision),
			MissingContext: r.MissingContextValues,
		}
		if v.Resource == "*" {
			v.Resource = ""
		}
		for _, s := range r.MatchedStatements {
			v.Matched = append(v.Matched, Statement{
				PolicyID:   aws.ToString(s.SourcePolicyId),
				PolicyType: string(s.SourcePolicyType),
			})
		}
		if d := r.PermissionsBoundaryDecisionDetail; d != nil {
			v.BoundaryAllowed = aws.Bool(d.AllowedByPermissionsBoundary)
		}
		if d := r.OrganizationsDecisionDetail; d != nil {
			v.OrgsAllowed = aws.Bool(d.AllowedByOrganizations)
		}
		out = append(out, v)
	}
	return out
}

// Render writes the human verdict for a principal: one block per action,
// each rendering allowed / implicit deny / explicit deny distinctly with the
// matched statements named, followed by the standing caveats.
func Render(w io.Writer, principal string, verdicts []Verdict) {
	for i, v := range verdicts {
		if i > 0 {
			fmt.Fprintln(w)
		}
		renderVerdict(w, principal, v)
	}
	fmt.Fprint(w, Caveats())
}

func renderVerdict(w io.Writer, principal string, v Verdict) {
	target := v.Action
	if v.Resource != "" {
		target += " on " + v.Resource
	}

	switch v.Decision {
	case DecisionAllowed:
		fmt.Fprintf(w, "✅ Allowed: %s for %s\n", target, principal)
		if len(v.Matched) > 0 {
			fmt.Fprintf(w, "  ✓ Identity policies      allowed by %s\n", statementList(v.Matched))
		} else {
			fmt.Fprintf(w, "  ✓ Identity policies      allowed (no statement detail returned)\n")
		}
	case DecisionExplicitDeny:
		fmt.Fprintf(w, "❌ Denied: %s for %s — EXPLICIT deny\n", target, principal)
		if len(v.Matched) > 0 {
			fmt.Fprintf(w, "  ✗ Deny statement         %s forbids this action — removing an allow elsewhere will not help\n", statementList(v.Matched))
		} else {
			fmt.Fprintf(w, "  ✗ Deny statement         a policy explicitly forbids this action — removing an allow elsewhere will not help\n")
		}
	default: // implicit deny
		fmt.Fprintf(w, "❌ Denied: %s for %s — implicit deny (no policy allows it)\n", target, principal)
		fmt.Fprintf(w, "  ✗ Identity policies      no attached or inline policy allows this action\n")
		fmt.Fprintf(w, "    Fix: grant an identity policy that allows %s", v.Action)
		if v.Resource != "" {
			fmt.Fprintf(w, " on %s", v.Resource)
		}
		fmt.Fprintln(w)
	}

	switch {
	case v.BoundaryAllowed == nil:
		// No boundary on the principal — say nothing rather than guess.
	case *v.BoundaryAllowed:
		fmt.Fprintf(w, "  ✓ Permissions boundary   does not limit this action\n")
	default:
		fmt.Fprintf(w, "  ✗ Permissions boundary   the boundary does not include this action — the boundary, not the identity policies, is the blocker\n")
	}

	if v.OrgsAllowed != nil && !*v.OrgsAllowed {
		fmt.Fprintf(w, "  ✗ Organizations          an SCP blocks this action for the account\n")
	}

	if len(v.MissingContext) > 0 {
		fmt.Fprintf(w, "  ⚠ Condition keys not supplied (treated as absent): %s\n",
			strings.Join(v.MissingContext, ", "))
	}
}

func statementList(stmts []Statement) string {
	parts := make([]string, 0, len(stmts))
	for _, s := range stmts {
		p := s.PolicyID
		if p == "" {
			p = "(unnamed policy)"
		}
		if s.PolicyType != "" {
			p += " (" + s.PolicyType + ")"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// Caveats returns the standing disclaimer; it is printed with every verdict
// because the simulator's blind spots are exactly what makes "but the
// simulator said allowed!" tickets.
func Caveats() string {
	return `
Caveats — the simulator does not evaluate:
  • Resource-based policies (bucket/queue/key/secret policies) — a real request
    may be allowed or denied by them regardless of this verdict
  • Service control policies are only reflected when AWS returns an
    Organizations decision (shown above when present)
  • Session policies and VPC endpoint policies
  • Condition keys you did not supply are treated as absent
`
}
