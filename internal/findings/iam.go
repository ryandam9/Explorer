package findings

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// IAM hygiene check IDs (stable; see README "The checks").
const (
	CheckOldAccessKey       = "IAM-KEY-001"
	CheckUnusedAccessKey    = "IAM-KEY-002"
	CheckUserNoMFA          = "IAM-USER-001"
	CheckRootAccessKey      = "IAM-ROOT-001"
	CheckUnusedRole         = "IAM-ROLE-001"
	CheckWildcardPolicy     = "IAM-POLICY-001"
	CheckUserAttachedPolicy = "IAM-POLICY-002"
	CheckOpenTrustPolicy    = "IAM-TRUST-001"
)

// staleAge is the age/idleness threshold for keys and roles, per the
// 90-day rotation convention the spec uses.
const staleAge = 90 * 24 * time.Hour

// IAMSnapshot is the input to AnalyzeIAM. IAM is account-global, so one
// snapshot covers the account (collected once, in the first scanned region).
type IAMSnapshot struct {
	Now time.Time

	// Users come from the credential report (which includes the root row).
	Users []IAMUser

	Roles    []IAMRole
	Policies []IAMPolicy
}

// IAMUser is one credential-report row, parsed.
type IAMUser struct {
	Name            string
	IsRoot          bool
	PasswordEnabled bool
	MFAActive       bool
	Keys            []IAMAccessKey
}

// IAMAccessKey is one of a user's (up to two) access keys.
type IAMAccessKey struct {
	Slot        int // 1 or 2
	Active      bool
	LastRotated time.Time // zero = unknown
	LastUsed    time.Time // zero = never used / unknown
}

// IAMRole is a role's usage and trust posture.
type IAMRole struct {
	Name          string
	ARN           string
	ServiceRole   bool      // path starts with /aws-service-role/ — AWS-managed, skip usage checks
	LastUsed      time.Time // RoleLastUsed; zero = never (or unknown)
	LastUsedKnown bool      // false when GetRole was denied — never flag then
	Created       time.Time
	TrustPolicy   string // URL-decoded JSON document
}

// IAMPolicy is a customer-managed policy's document and attachment posture.
type IAMPolicy struct {
	Name          string
	ARN           string
	Document      string   // default version, URL-decoded JSON
	AttachedUsers []string // user names the policy is attached to directly
}

// AnalyzeIAM runs every IAM hygiene check over the snapshot. Pure.
func AnalyzeIAM(snap IAMSnapshot) []Finding {
	var out []Finding
	checkUsers(snap, &out)
	checkRoles(snap, &out)
	checkPolicies(snap, &out)
	return out
}

func checkUsers(snap IAMSnapshot, out *[]Finding) {
	for _, u := range snap.Users {
		if u.IsRoot {
			for _, k := range u.Keys {
				if k.Active {
					*out = append(*out, Finding{
						ID: CheckRootAccessKey, Severity: SevCritical, Service: "iam", Region: "global",
						Resource: "root",
						Title:    "Root account has an active access key",
						Detail:   fmt.Sprintf("Root access key %d is active; root credentials cannot be permission-scoped.", k.Slot),
						Fix:      "Delete the root access keys and use IAM roles/users instead.",
					})
				}
			}
			// Root MFA matters too, but the password/MFA checks below are
			// user-oriented; keep root scoped to the access-key check.
			continue
		}

		if u.PasswordEnabled && !u.MFAActive {
			*out = append(*out, Finding{
				ID: CheckUserNoMFA, Severity: SevCritical, Service: "iam", Region: "global",
				Resource: u.Name,
				Title:    "Console user has no MFA",
				Detail:   "The user can sign in to the console with a password alone.",
				Fix:      "Require an MFA device for the user (or remove console access).",
			})
		}

		for _, k := range u.Keys {
			if !k.Active {
				continue
			}
			age := time.Duration(0)
			if !k.LastRotated.IsZero() {
				age = snap.Now.Sub(k.LastRotated)
			}
			idle := time.Duration(0)
			neverUsed := k.LastUsed.IsZero()
			if !neverUsed {
				idle = snap.Now.Sub(k.LastUsed)
			}

			switch {
			// Active and unused for 90+ days (or never used despite being
			// 90+ days old): standing credential nobody needs.
			case (neverUsed && age > staleAge) || (!neverUsed && idle > staleAge):
				detail := fmt.Sprintf("Access key %d is active but was last used %s.", k.Slot, agoOrNever(k.LastUsed, snap.Now))
				*out = append(*out, Finding{
					ID: CheckUnusedAccessKey, Severity: SevCritical, Service: "iam", Region: "global",
					Resource: u.Name,
					Title:    "Active access key unused for 90+ days",
					Detail:   detail,
					Fix:      "Deactivate, then delete the key once nothing breaks.",
				})
			case age > staleAge:
				*out = append(*out, Finding{
					ID: CheckOldAccessKey, Severity: SevWarning, Service: "iam", Region: "global",
					Resource: u.Name,
					Title:    "Access key older than 90 days",
					Detail:   fmt.Sprintf("Access key %d was last rotated %d days ago.", k.Slot, int(age.Hours()/24)),
					Fix:      "Rotate the key: create a new one, migrate, then delete the old.",
				})
			}
		}
	}
}

func agoOrNever(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%d days ago", int(now.Sub(t).Hours()/24))
}

func checkRoles(snap IAMSnapshot, out *[]Finding) {
	for _, r := range snap.Roles {
		if PolicyAllowsEveryone(r.TrustPolicy) {
			*out = append(*out, Finding{
				ID: CheckOpenTrustPolicy, Severity: SevCritical, Service: "iam", Region: "global",
				Resource: r.Name, ARN: r.ARN,
				Title:  `Role trust policy allows "AWS": "*"`,
				Detail: "Any AWS principal in any account can attempt to assume this role.",
				Fix:    "Restrict the trust policy to specific accounts/roles, or add conditions (e.g. sts:ExternalId).",
			})
		}

		if r.ServiceRole || !r.LastUsedKnown {
			continue
		}
		// New roles legitimately have no usage yet; only flag roles older
		// than the threshold.
		if !r.Created.IsZero() && snap.Now.Sub(r.Created) <= staleAge {
			continue
		}
		if r.LastUsed.IsZero() || snap.Now.Sub(r.LastUsed) > staleAge {
			*out = append(*out, Finding{
				ID: CheckUnusedRole, Severity: SevInfo, Service: "iam", Region: "global",
				Resource: r.Name, ARN: r.ARN,
				Title:  "Role unused for 90+ days",
				Detail: fmt.Sprintf("RoleLastUsed: %s.", agoOrNever(r.LastUsed, snap.Now)),
				Fix:    "Confirm nothing seasonal needs it, then delete the role.",
			})
		}
	}
}

func checkPolicies(snap IAMSnapshot, out *[]Finding) {
	for _, p := range snap.Policies {
		if PolicyGrantsFullAdmin(p.Document) {
			*out = append(*out, Finding{
				ID: CheckWildcardPolicy, Severity: SevCritical, Service: "iam", Region: "global",
				Resource: p.Name, ARN: p.ARN,
				Title:  "Customer policy grants */* (full admin)",
				Detail: `The policy has an Allow statement with Action "*" on Resource "*".`,
				Fix:    "Scope the policy to the actions and resources actually needed (or attach AdministratorAccess deliberately and audit who has it).",
			})
		}
		if len(p.AttachedUsers) > 0 {
			*out = append(*out, Finding{
				ID: CheckUserAttachedPolicy, Severity: SevInfo, Service: "iam", Region: "global",
				Resource: p.Name, ARN: p.ARN,
				Title:  "Policy attached directly to users",
				Detail: fmt.Sprintf("Attached to user(s): %s. Direct attachments bypass group/role-based management.", strings.Join(p.AttachedUsers, ", ")),
				Fix:    "Attach the policy to a group or role and add the users to it instead.",
			})
		}
	}
}

// fullStatement carries the fields the wildcard check needs; the list
// tolerates Statement being a single object as well as an array.
type fullStatement struct {
	Effect   string          `json:"Effect"`
	Action   json.RawMessage `json:"Action"`
	Resource json.RawMessage `json:"Resource"`
}

type fullStatements []fullStatement

func (s *fullStatements) UnmarshalJSON(b []byte) error {
	var arr []fullStatement
	if err := json.Unmarshal(b, &arr); err == nil {
		*s = arr
		return nil
	}
	var one fullStatement
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*s = []fullStatement{one}
	return nil
}

// PolicyGrantsFullAdmin reports whether the policy document has an Allow
// statement whose Action is "*" (or "*:*") and whose Resource is "*". Pure.
func PolicyGrantsFullAdmin(policyJSON string) bool {
	if strings.TrimSpace(policyJSON) == "" {
		return false
	}
	var doc struct {
		Statement fullStatements `json:"Statement"`
	}
	if json.Unmarshal([]byte(policyJSON), &doc) != nil {
		return false
	}
	for _, st := range doc.Statement {
		if !strings.EqualFold(st.Effect, "Allow") {
			continue
		}
		if rawContainsWildcard(st.Action, true) && rawContainsWildcard(st.Resource, false) {
			return true
		}
	}
	return false
}

// rawContainsWildcard reports whether a string-or-list JSON value contains
// "*" (or, for actions, "*:*").
func rawContainsWildcard(raw json.RawMessage, action bool) bool {
	matches := func(s string) bool {
		return s == "*" || (action && s == "*:*")
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return matches(s)
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		for _, v := range list {
			if matches(v) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Credential report parsing
// ---------------------------------------------------------------------------

// ParseCredentialReport parses the IAM credential report CSV into users.
// Unknown or "N/A"/"no_information" date fields parse as zero times. Pure —
// fixture-testable with canned CSV.
func ParseCredentialReport(csvData []byte) ([]IAMUser, error) {
	r := csv.NewReader(strings.NewReader(string(csvData)))
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing credential report: %w", err)
	}
	if len(records) < 1 {
		return nil, fmt.Errorf("credential report is empty")
	}
	col := map[string]int{}
	for i, name := range records[0] {
		col[name] = i
	}
	field := func(row []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(row) {
			return ""
		}
		return row[i]
	}

	var users []IAMUser
	for _, row := range records[1:] {
		u := IAMUser{
			Name:            field(row, "user"),
			PasswordEnabled: field(row, "password_enabled") == "true",
			MFAActive:       field(row, "mfa_active") == "true",
		}
		u.IsRoot = u.Name == "<root_account>"
		for _, slot := range []int{1, 2} {
			prefix := fmt.Sprintf("access_key_%d_", slot)
			u.Keys = append(u.Keys, IAMAccessKey{
				Slot:        slot,
				Active:      field(row, prefix+"active") == "true",
				LastRotated: parseReportTime(field(row, prefix+"last_rotated")),
				LastUsed:    parseReportTime(field(row, prefix+"last_used_date")),
			})
		}
		users = append(users, u)
	}
	return users, nil
}

// parseReportTime parses a credential-report timestamp; the report uses
// RFC3339 plus the literals N/A, no_information and not_supported.
func parseReportTime(s string) time.Time {
	s = strings.TrimSpace(s)
	switch s {
	case "", "N/A", "no_information", "not_supported":
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// DecodePolicyDocument URL-decodes an IAM policy document as the IAM API
// returns it (RFC 3986 encoded JSON). Falls back to the input on decode
// failure, since some SDK paths already return plain JSON.
func DecodePolicyDocument(doc string) string {
	decoded, err := url.QueryUnescape(doc)
	if err != nil {
		return doc
	}
	return decoded
}
