package lambdatui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// parseLambdaTime parses the LastModified string the Lambda API returns. It is
// ISO-8601 with a sub-second fraction and a numeric zone
// (2026-06-15T01:14:00.000+0000); RFC3339 is accepted as a fallback. An
// unparseable or empty value yields the zero time (rendered as an em dash).
func parseLambdaTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339, "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// runtimeLabel renders a function's runtime for the table; container-image
// functions report no runtime, so they show "Image" instead of a blank.
func runtimeLabel(runtime, packageType string) string {
	if runtime != "" {
		return runtime
	}
	if strings.EqualFold(packageType, "Image") {
		return "Image"
	}
	return "—"
}

// stateLabel pairs a function/event-source state with a leading glyph, e.g.
// "✓ Active". An empty state renders as a muted em dash.
func stateLabel(state string) string {
	if state == "" {
		return "—"
	}
	return stateGlyph(state) + " " + state
}

// stateGlyph maps a Lambda state string to an at-a-glance glyph. Covers both
// the function State vocabulary (Active/Inactive/Pending/Failed) and the
// event-source-mapping State vocabulary (Enabled/Disabled/Creating/…).
func stateGlyph(state string) string {
	switch strings.ToLower(state) {
	case "active", "enabled":
		return "✓"
	case "pending", "creating", "enabling", "disabling", "updating", "inprogress":
		return "●"
	case "failed", "deleting":
		return "✗"
	case "inactive", "disabled":
		return "○"
	default:
		return "•"
	}
}

// formatMemory renders a function's memory size, em dash when unset.
func formatMemory(mb int32) string {
	if mb <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d MB", mb)
}

// formatTimeout renders a function's timeout, em dash when unset.
func formatTimeout(sec int32) string {
	if sec <= 0 {
		return "—"
	}
	return fmt.Sprintf("%ds", sec)
}

// formatCodeSize renders a deployment-package size in the largest sensible unit.
func formatCodeSize(bytes int64) string {
	switch {
	case bytes <= 0:
		return "—"
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// shortTime renders a timestamp as "2026-06-15 01:14" in local time, or "—" for
// the zero time.
func shortTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// truncate shortens s to width runes, appending an ellipsis when it overflows.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

// lastSegment returns the portion of an ARN/path after the final ':' or '/',
// e.g. the queue name from an SQS ARN or the stream name from a Kinesis ARN.
func lastSegment(s string) string {
	if i := strings.LastIndexAny(s, ":/"); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	return s
}

// functionNameFromARN extracts the function name from a function ARN
// (arn:aws:lambda:region:acct:function:NAME[:qualifier]). A non-ARN value is
// returned unchanged so a bare name still works.
func functionNameFromARN(arn string) string {
	if !strings.HasPrefix(arn, "arn:") {
		return arn
	}
	parts := strings.Split(arn, ":")
	// …:function:NAME or …:function:NAME:QUALIFIER
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "function" {
			return parts[i+1]
		}
	}
	return lastSegment(arn)
}

// eventSourceLabel renders a mapping's source as a short "service:resource"
// string. Stream/queue sources expose an EventSourceArn; self-managed Kafka and
// MSK expose other shapes, handled best-effort so the column is never blank.
func eventSourceLabel(m lambdatypes.EventSourceMappingConfiguration) string {
	if arn := aws.ToString(m.EventSourceArn); arn != "" {
		svc := arnService(arn)
		if svc == "" {
			return lastSegment(arn)
		}
		return svc + ":" + lastSegment(arn)
	}
	if len(m.Queues) > 0 {
		return "kafka:" + m.Queues[0]
	}
	if m.SelfManagedEventSource != nil {
		return "kafka (self-managed)"
	}
	return "—"
}

// arnService returns the service segment of an ARN (the 3rd ':'-delimited
// field), e.g. "sqs" / "kinesis" / "dynamodb". Empty for a non-ARN.
func arnService(arn string) string {
	if !strings.HasPrefix(arn, "arn:") {
		return ""
	}
	parts := strings.Split(arn, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// sortedMapKeys returns a map's keys in sorted order, for deterministic
// rendering of environment-variable keys.
func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dlqLabel renders the dead-letter target as a short "service:resource", or a
// muted note when none is configured.
func dlqLabel(arn string) string {
	if arn == "" {
		return "none configured"
	}
	if svc := arnService(arn); svc != "" {
		return svc + ":" + lastSegment(arn)
	}
	return lastSegment(arn)
}

// emptyDash returns s, or an em dash when s is empty — for aligned detail rows.
func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// joinOrDash joins parts with ", ", or returns an em dash when empty.
func joinOrDash(parts []string) string {
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}
