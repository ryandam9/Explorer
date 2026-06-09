package vpctui

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Network ACL rule explanations
//
// Network ACLs differ from Security Groups in three ways that the plain-English
// rendering makes explicit:
//   - Rules can allow OR deny.
//   - Rules are evaluated in ascending rule-number order and the first match
//     wins (rule * / 32767 is the implicit catch-all).
//   - NACLs are stateless, so return traffic needs its own rule.
//
// The protocol/port/CIDR phrasing reuses the shared helpers in sgexplain.go.
// ---------------------------------------------------------------------------

// naclDefaultRuleNumber is AWS's catch-all rule, shown in the console as "*".
const naclDefaultRuleNumber = 32767

// explainNACLRule renders a single NACL entry as a plain-English sentence,
// including its rule number and allow/deny action, with a risk note when an
// allow rule exposes a sensitive port to the public internet.
func explainNACLRule(r NACLRule) string {
	action := "Allow"
	if strings.EqualFold(r.Action, "deny") {
		action = "Deny"
	}

	dir, prep := "inbound", "from"
	if strings.EqualFold(r.Direction, "Outbound") {
		dir, prep = "outbound", "to"
	}

	label := fmt.Sprintf("Rule %d", r.RuleNumber)
	if r.RuleNumber == naclDefaultRuleNumber {
		label = "Rule * (default)"
	}

	sentence := fmt.Sprintf("%s: %s %s %s %s %s",
		label, action, dir, describeProtoPorts(r.Protocol, r.PortRange), prep, describeSource(r.CIDR))

	// Only allow rules create exposure; a deny rule is protective.
	if action == "Allow" {
		if note := exposureRisk(r.Protocol, r.PortRange, r.CIDR); note != "" {
			sentence += "  " + note
		}
	}
	return sentence
}

// encodeNACLExplanations renders the "In plain English" section for a NACL,
// grouping inbound and outbound rules in evaluation order. The rules passed in
// are expected to already be sorted by rule number.
func encodeNACLExplanations(inbound, outbound []NACLRule) string {
	var sb strings.Builder
	sb.WriteString("\n\n  In plain English (rules evaluated low→high number, first match wins; NACLs are stateless):")

	sb.WriteString("\n  Inbound:")
	if len(inbound) == 0 {
		sb.WriteString("\n  • (no inbound rules)")
	}
	for _, r := range inbound {
		sb.WriteString("\n  • " + explainNACLRule(r))
	}

	sb.WriteString("\n  Outbound:")
	if len(outbound) == 0 {
		sb.WriteString("\n  • (no outbound rules)")
	}
	for _, r := range outbound {
		sb.WriteString("\n  • " + explainNACLRule(r))
	}
	return sb.String()
}
