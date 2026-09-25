package findings

import (
	"encoding/json"
	"strings"
)

// LambdaPolicyGrant is one principal allowed by a function's resource-based
// policy (lambda:GetPolicy), reduced to what answers "who can invoke this?":
// the principal, and the conditions that scope it to a source.
type LambdaPolicyGrant struct {
	Sid       string
	Principal string // "*", a service ("s3.amazonaws.com") or an account / IAM ARN
	Kind      string // "any", "service" or "aws"
	Actions   []string

	SourceArn      string // AWS:SourceArn condition
	SourceAccount  string // AWS:SourceAccount condition
	PrincipalOrgID string // aws:PrincipalOrgID condition
	URLAuthType    string // lambda:FunctionUrlAuthType condition
	HasCondition   bool   // any Condition block at all
}

// Invokes reports whether the grant allows invoking the function (directly or
// through its URL).
func (g LambdaPolicyGrant) Invokes() bool {
	for _, a := range g.Actions {
		switch strings.ToLower(a) {
		case "lambda:invokefunction", "lambda:invokefunctionurl", "lambda:*", "*":
			return true
		}
	}
	return false
}

// Unrestricted reports whether anyone may invoke the function through this
// grant: Principal "*" with no condition at all to scope it.
func (g LambdaPolicyGrant) Unrestricted() bool {
	return g.Kind == "any" && !g.HasCondition && g.Invokes()
}

// ParseLambdaPolicy reads a resource-based policy document into one grant per
// (Allow statement, principal). Deny statements are skipped: they only narrow
// what the Allow statements grant.
func ParseLambdaPolicy(doc string) ([]LambdaPolicyGrant, error) {
	var p struct {
		Statement json.RawMessage `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(doc), &p); err != nil {
		return nil, err
	}
	var stmts []map[string]json.RawMessage
	if len(p.Statement) > 0 && p.Statement[0] == '{' {
		var one map[string]json.RawMessage
		if err := json.Unmarshal(p.Statement, &one); err != nil {
			return nil, err
		}
		stmts = append(stmts, one)
	} else if len(p.Statement) > 0 {
		if err := json.Unmarshal(p.Statement, &stmts); err != nil {
			return nil, err
		}
	}

	var out []LambdaPolicyGrant
	for _, st := range stmts {
		var effect, sid string
		_ = json.Unmarshal(st["Effect"], &effect) // absent → "" (not Allow)
		_ = json.Unmarshal(st["Sid"], &sid)       // optional
		if !strings.EqualFold(effect, "Allow") {
			continue
		}
		base := LambdaPolicyGrant{Sid: sid, Actions: stringOrList(st["Action"])}
		applyConditions(&base, st["Condition"])
		for _, pr := range principals(st["Principal"]) {
			g := base
			g.Kind, g.Principal = pr[0], pr[1]
			out = append(out, g)
		}
	}
	return out, nil
}

// principals flattens a Principal element into (kind, value) pairs.
func principals(raw json.RawMessage) [][2]string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "*" {
			return [][2]string{{"any", "*"}}
		}
		return [][2]string{{"aws", s}}
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out [][2]string
	for k, v := range m {
		for _, val := range stringOrList(v) {
			switch {
			case val == "*":
				out = append(out, [2]string{"any", "*"})
			case strings.EqualFold(k, "Service"):
				out = append(out, [2]string{"service", val})
			default:
				out = append(out, [2]string{"aws", val})
			}
		}
	}
	return out
}

// applyConditions records the condition keys that scope an invoke grant.
// Keys are matched case-insensitively ("AWS:SourceArn" / "aws:SourceArn").
func applyConditions(g *LambdaPolicyGrant, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var ops map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &ops) != nil || len(ops) == 0 {
		// A condition block we can't read still conditions the grant; treat it
		// as present so an unreadable policy never reads as "open to anyone".
		g.HasCondition = len(raw) > 2
		return
	}
	g.HasCondition = true
	for _, kv := range ops {
		for k, v := range kv {
			vals := strings.Join(stringOrList(v), ", ")
			switch strings.ToLower(k) {
			case "aws:sourcearn":
				g.SourceArn = vals
			case "aws:sourceaccount":
				g.SourceAccount = vals
			case "aws:principalorgid":
				g.PrincipalOrgID = vals
			case "lambda:functionurlauthtype":
				g.URLAuthType = vals
			}
		}
	}
}

func stringOrList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var l []string
	_ = json.Unmarshal(raw, &l) // not a list either → none
	return l
}
