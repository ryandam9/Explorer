// Package authzmsg summarizes decoded IAM authorization failure messages
// (AXE-001). Services like EC2 return opaque "Encoded authorization failure
// message" blobs; sts:DecodeAuthorizationMessage turns them into a JSON
// document, and this package turns that document into the three facts an
// engineer needs: who was denied, doing what, on which resource — and whether
// it was an explicit deny or just a missing allow.
package authzmsg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Summary is the human-relevant core of a decoded authorization message.
type Summary struct {
	Allowed      bool
	ExplicitDeny bool
	Principal    string // ARN preferred, falls back to the principal ID
	Action       string
	Resource     string
	// MatchedStatements names the policies whose statements matched the
	// request (for an explicit deny, the policy that denied it).
	MatchedStatements []string
}

// decodedMessage mirrors the JSON shape sts:DecodeAuthorizationMessage
// returns. Only the fields the summary needs are declared.
type decodedMessage struct {
	Allowed           bool `json:"allowed"`
	ExplicitDeny      bool `json:"explicitDeny"`
	MatchedStatements struct {
		Items []struct {
			SourcePolicyID   string `json:"sourcePolicyId"`
			SourcePolicyType string `json:"sourcePolicyType"`
		} `json:"items"`
	} `json:"matchedStatements"`
	Context struct {
		Principal struct {
			ID  string `json:"id"`
			ARN string `json:"arn"`
		} `json:"principal"`
		Action   string `json:"action"`
		Resource string `json:"resource"`
	} `json:"context"`
}

// Summarize extracts a Summary from the decoded message JSON.
func Summarize(decoded []byte) (Summary, error) {
	var msg decodedMessage
	if err := json.Unmarshal(decoded, &msg); err != nil {
		return Summary{}, fmt.Errorf("decoded message is not the expected JSON document: %w", err)
	}

	s := Summary{
		Allowed:      msg.Allowed,
		ExplicitDeny: msg.ExplicitDeny,
		Principal:    msg.Context.Principal.ARN,
		Action:       msg.Context.Action,
		Resource:     msg.Context.Resource,
	}
	if s.Principal == "" {
		s.Principal = msg.Context.Principal.ID
	}
	for _, st := range msg.MatchedStatements.Items {
		name := st.SourcePolicyID
		if name == "" {
			continue
		}
		if st.SourcePolicyType != "" {
			name += " (" + st.SourcePolicyType + ")"
		}
		s.MatchedStatements = append(s.MatchedStatements, name)
	}
	return s, nil
}

// Render formats the summary as the few lines a human reads first.
func Render(s Summary) string {
	var b strings.Builder

	switch {
	case s.Allowed:
		b.WriteString("✓ Allowed\n")
	case s.ExplicitDeny:
		b.WriteString("❌ Explicit deny — a policy statement explicitly denies this request\n")
	default:
		b.WriteString("❌ Implicit deny — no policy allows this request (missing allow, not an explicit deny)\n")
	}

	row := func(label, v string) {
		if v == "" {
			v = "-"
		}
		fmt.Fprintf(&b, "  %-10s %s\n", label, v)
	}
	row("Principal", s.Principal)
	row("Action", s.Action)
	row("Resource", s.Resource)
	if len(s.MatchedStatements) > 0 {
		row("Matched", strings.Join(s.MatchedStatements, ", "))
	}

	if !s.Allowed && !s.ExplicitDeny {
		b.WriteString("\n  Fix: grant the principal an identity or resource policy that allows the action on the resource.\n")
	} else if s.ExplicitDeny {
		b.WriteString("\n  Fix: find the Deny statement in the matched policy (or an SCP/permission boundary) and scope it down.\n")
	}
	return b.String()
}

// StripPrefix removes the boilerplate services wrap around the blob when an
// engineer pastes the whole error message instead of just the encoded part.
func StripPrefix(in string) string {
	s := strings.TrimSpace(in)
	const marker = "Encoded authorization failure message:"
	if i := strings.Index(s, marker); i >= 0 {
		s = s[i+len(marker):]
	}
	// The blob itself never contains whitespace or quotes; trim anything an
	// error message might leave around it.
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	if i := strings.IndexAny(s, " \t\n"); i > 0 {
		s = s[:i]
	}
	return s
}
