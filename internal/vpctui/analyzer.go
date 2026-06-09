package vpctui

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// AWS Reachability Analyzer integration
//
// Read-only by default: list and render the Network Insights analyses that
// already exist in the account. Creating a new analysis is opt-in and gated by
// an explicit confirmation, because it provisions real resources and AWS
// charges per analysis. The pure helpers here (parsing and verdict/line
// formatting) are unit-tested; the AWS calls live on VPCClient.
// ---------------------------------------------------------------------------

// NetInsightsAnalysis is a flattened view of a Reachability Analyzer analysis
// joined with its path.
type NetInsightsAnalysis struct {
	AnalysisID    string
	PathID        string
	Source        string
	Destination   string
	Protocol      string
	DestPort      int32
	Status        string // running | succeeded | failed
	StatusMessage string
	PathFound     *bool // nil while running / on failure
	StartDate     string
}

// analysisVerdict returns a short human verdict for an analysis.
func analysisVerdict(a NetInsightsAnalysis) string {
	switch strings.ToLower(a.Status) {
	case "running":
		return "running"
	case "failed":
		return "failed"
	default:
		if a.PathFound != nil && *a.PathFound {
			return "reachable"
		}
		return "not reachable"
	}
}

// analysisGlyph returns the status glyph for an analysis.
func analysisGlyph(a NetInsightsAnalysis) string {
	switch analysisVerdict(a) {
	case "reachable":
		return "✓"
	case "not reachable":
		return "✗"
	case "failed":
		return "⚠"
	default:
		return "…"
	}
}

// analysisLine renders a one-line summary of an analysis.
func analysisLine(a NetInsightsAnalysis) string {
	dst := a.Destination
	if a.DestPort > 0 {
		dst = fmt.Sprintf("%s:%d", dst, a.DestPort)
	}
	proto := a.Protocol
	if proto == "" {
		proto = "tcp"
	}
	return fmt.Sprintf("%s %s → %s (%s)  [%s]",
		analysisGlyph(a), orDash(a.Source), orDash(dst), proto, analysisVerdict(a))
}

// parseAnalyzerInput parses a "source -> destination[:port]" specification used
// to create a new analysis. ok is false when both endpoints are not provided.
func parseAnalyzerInput(s string) (source, dest string, port int, ok bool) {
	parts := strings.SplitN(s, "->", 2)
	if len(parts) != 2 {
		return "", "", 0, false
	}
	source = strings.TrimSpace(parts[0])
	dest, port = parseTraceTarget(parts[1])
	if source == "" || dest == "" {
		return "", "", 0, false
	}
	return source, dest, port, true
}
