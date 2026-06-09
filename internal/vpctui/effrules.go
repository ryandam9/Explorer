package vpctui

import (
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Effective security rules
//
// An ENI can carry several security groups, and AWS evaluates the *union* of
// their rules. Reading them group-by-group is error-prone, so computeEffective
// Rules merges and de-duplicates every group's inbound/outbound rules for an
// ENI and records which groups contribute each one. The applicable NACL is
// noted too, since it is evaluated separately and statelessly.
//
// Pure over a vpcSnapshot and fully unit-testable.
// ---------------------------------------------------------------------------

// mergedRule is one distinct rule and the security groups that define it.
type mergedRule struct {
	Rule SGRule
	SGs  []string
}

// effectiveRuleSet is the merged view of an ENI's security posture.
type effectiveRuleSet struct {
	ENIID    string
	SGIDs    []string
	Inbound  []mergedRule
	Outbound []mergedRule
	NACLID   string // NACL applied to the ENI's subnet, if known
	Found    bool   // false when the ENI is not in the snapshot
}

// computeEffectiveRules merges the rules of every security group attached to the
// given ENI.
func computeEffectiveRules(snap vpcSnapshot, eniID string) effectiveRuleSet {
	eni := findENI(snap, eniID)
	if eni == nil {
		return effectiveRuleSet{ENIID: eniID}
	}
	res := effectiveRuleSet{ENIID: eniID, SGIDs: append([]string(nil), eni.SecurityGroups...), Found: true}

	inbound := map[string]*mergedRule{}
	outbound := map[string]*mergedRule{}
	for _, sgID := range eni.SecurityGroups {
		sg := findSG(snap, sgID)
		if sg == nil {
			continue
		}
		for _, r := range sg.Rules {
			target := outbound
			if strings.EqualFold(r.Direction, "Inbound") {
				target = inbound
			}
			key := ruleKey(r)
			if mr, ok := target[key]; ok {
				if !contains(mr.SGs, sgID) {
					mr.SGs = append(mr.SGs, sgID)
				}
			} else {
				target[key] = &mergedRule{Rule: r, SGs: []string{sgID}}
			}
		}
	}
	res.Inbound = sortedRules(inbound)
	res.Outbound = sortedRules(outbound)

	if nacl := naclForSubnet(snap, eni.SubnetID); nacl != nil {
		res.NACLID = nacl.ID
	}
	return res
}

// ruleKey identifies a rule independent of which group defines it.
func ruleKey(r SGRule) string {
	return strings.ToLower(r.Direction + "|" + r.Protocol + "|" + r.PortRange + "|" + r.Source)
}

func sortedRules(m map[string]*mergedRule) []mergedRule {
	out := make([]mergedRule, 0, len(m))
	for _, mr := range m {
		sort.Strings(mr.SGs)
		out = append(out, *mr)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ruleKey(out[i].Rule) < ruleKey(out[j].Rule)
	})
	return out
}
