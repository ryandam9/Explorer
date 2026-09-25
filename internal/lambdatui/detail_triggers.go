package lambdatui

import (
	"fmt"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/findings"
)

// The Triggers panel answers "what calls this function?" from what AWS
// records: the event-source mappings that poll a queue/stream for it (from the
// loaded inventory), the principals its resource-based policy allows to invoke
// it (S3 buckets, EventBridge rules, SNS topics, API Gateway, other accounts…),
// and its function URLs. Callers that use their own IAM permissions in the
// same account (Step Functions, SDK calls, other functions) need no policy
// entry, so the panel says it can't list those rather than implying "none".

// servicePrincipalLabels names the common service principals.
var servicePrincipalLabels = map[string]string{
	"s3.amazonaws.com":                   "S3 bucket",
	"events.amazonaws.com":               "EventBridge rule",
	"sns.amazonaws.com":                  "SNS topic",
	"apigateway.amazonaws.com":           "API Gateway",
	"logs.amazonaws.com":                 "CloudWatch Logs subscription",
	"elasticloadbalancing.amazonaws.com": "Load balancer target group",
	"iot.amazonaws.com":                  "IoT rule",
	"cognito-idp.amazonaws.com":          "Cognito user pool",
	"secretsmanager.amazonaws.com":       "Secrets Manager rotation",
	"scheduler.amazonaws.com":            "EventBridge Scheduler",
	"lex.amazonaws.com":                  "Lex bot",
	"config.amazonaws.com":               "AWS Config rule",
	"alexa-appkit.amazon.com":            "Alexa skill",
}

// grantLabel renders one policy grant: who, and the source it is scoped to.
func grantLabel(g findings.LambdaPolicyGrant, fnAccount string) string {
	var who string
	switch g.Kind {
	case "any":
		who = "⚠ anyone (Principal \"*\")"
		if g.URLAuthType != "" {
			who = "anyone, through the function URL (auth " + g.URLAuthType + ")"
		}
	case "service":
		who = g.Principal
		if l, ok := servicePrincipalLabels[g.Principal]; ok {
			who = l
		}
	default:
		acct := accountOf(g.Principal)
		who = "account " + acct
		if acct == "" {
			who = g.Principal
		} else if fnAccount != "" && acct != fnAccount {
			who += " (cross-account)"
		}
	}
	var scope []string
	if g.SourceArn != "" {
		scope = append(scope, lastSegment(g.SourceArn)+"  ("+g.SourceArn+")")
	}
	if g.SourceAccount != "" {
		scope = append(scope, "source account "+g.SourceAccount)
	}
	if g.PrincipalOrgID != "" {
		scope = append(scope, "org "+g.PrincipalOrgID)
	}
	if len(scope) == 0 && g.Kind == "service" {
		scope = append(scope, "any source — no SourceArn/SourceAccount condition")
	}
	if len(scope) == 0 {
		return who
	}
	return who + " — " + strings.Join(scope, " · ")
}

// accountOf pulls the 12-digit account out of an account ID or IAM ARN.
func accountOf(p string) string {
	if len(p) == 12 && strings.Trim(p, "0123456789") == "" {
		return p
	}
	if parts := strings.Split(p, ":"); len(parts) >= 5 && strings.HasPrefix(p, "arn:") {
		return parts[4]
	}
	return ""
}

func triggersBody(d FunctionDetail) string {
	var b strings.Builder
	n := 0
	for _, es := range d.Triggers {
		state := es.State
		if state == "" {
			state = "state unknown"
		}
		b.WriteString(fmt.Sprintf("  polls %s · %s · batch %d\n", es.SourceLabel, strings.ToLower(state), es.BatchSize))
		n++
	}
	switch {
	case d.ResourcePolicyErr != "":
		b.WriteString("  " + d.ResourcePolicyErr + "\n")
	case strings.TrimSpace(d.ResourcePolicy) != "":
		grants, err := findings.ParseLambdaPolicy(d.ResourcePolicy)
		if err != nil {
			b.WriteString("  Could not parse the resource policy: " + err.Error() + "\n")
			break
		}
		for _, g := range grants {
			if !g.Invokes() {
				continue
			}
			b.WriteString("  " + grantLabel(g, accountOf(d.ARN)) + "\n")
			n++
		}
	}
	for _, u := range d.URLs {
		label := "  HTTPS function URL (auth " + u.AuthType + ")"
		if u.Qualifier != "" {
			label += " on " + u.Qualifier
		}
		b.WriteString(label + "\n")
		n++
	}
	if n == 0 && d.ResourcePolicyErr == "" {
		b.WriteString("  No event-source mapping, resource-policy grant or function URL.\n")
	}
	b.WriteString("  (Callers using their own IAM permissions in this account — Step Functions, SDK calls, other functions — need no policy entry and aren't listed.)")
	return b.String()
}

// triggersFor returns the event-source mappings that feed the function.
func triggersFor(inv Inventory, region, name string) []EventSource {
	var out []EventSource
	for _, es := range inv.EventSources {
		if es.Region == region && es.FunctionName == name {
			out = append(out, es)
		}
	}
	return out
}
