// Package loggroup derives the CloudWatch Logs group (or group-name prefix) a
// resource writes to, so the summary TUI can jump straight from a resource to
// its logs (AXE-011). It is a pure string transform with table tests — no AWS
// calls — and is deliberately conservative: a resource whose log group cannot
// be derived from its identity alone returns ok=false rather than a guess that
// would drop the user into an empty, confusing log view.
//
// The returned string is used as the cw TUI's group *filter* (its --group
// behaves as a prefix/pattern), so an exact group name and a prefix both work:
// RDS exports several per-log-type groups under one prefix, so the prefix is
// the right target.
package loggroup

import "strings"

// Resource is the minimal view of a model.Resource this package needs. Keeping
// it local avoids a dependency on the model package and keeps the function
// trivially testable.
type Resource struct {
	Service string
	Type    string
	ID      string
	Name    string
}

// For returns the CloudWatch log group filter for a resource and whether one
// could be derived. Supported: Lambda functions, RDS DB instances, and EKS
// clusters — the services with a deterministic, convention-based log group.
// ECS is intentionally excluded: its log group is defined in the task
// definition's awslogs driver and cannot be derived from the service's
// identity without an API call.
func For(r Resource) (string, bool) {
	name := strings.TrimSpace(r.Name)
	id := strings.TrimSpace(r.ID)
	switch strings.ToLower(r.Service) {
	case "lambda":
		if name == "" {
			return "", false
		}
		return "/aws/lambda/" + name, true
	case "rds":
		// RDS publishes one group per exported log type under this prefix
		// (…/error, …/general, …/postgresql, …). The prefix lists them all.
		dbid := firstNonEmpty(name, id)
		if dbid == "" {
			return "", false
		}
		return "/aws/rds/instance/" + dbid + "/", true
	case "eks":
		cluster := firstNonEmpty(name, id)
		if cluster == "" {
			return "", false
		}
		return "/aws/eks/" + cluster + "/cluster", true
	default:
		return "", false
	}
}

// Supported reports whether For can derive a log group for the resource's
// service — used by the TUI to decide whether to advertise the jump key.
func Supported(service string) bool {
	switch strings.ToLower(service) {
	case "lambda", "rds", "eks":
		return true
	default:
		return false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
