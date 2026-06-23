package xref

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
)

// relatedCaveat is the always-printed honesty note (§8): the result only
// reflects the relationship types collection extracts, so an empty side never
// means "this resource is isolated".
const relatedCaveat = "Only the link types listed above are checked — connections of other kinds won't appear here."

// humanKind renders a Target kind in plain English for the report header.
func humanKind(k Kind) string {
	switch k {
	case KindIAMRole:
		return "IAM role"
	case KindKMSKey:
		return "KMS key"
	case KindACMCert:
		return "ACM certificate"
	case KindSecurityGroup:
		return "security group"
	default:
		return "resource"
	}
}

// targetKindLabel is the parenthetical label in the report header. The four
// classified kinds get their plain name; any other ARN is named from the ARN
// itself (e.g. "lambda function") rather than the vague "resource" — a Lambda
// or S3 ARN queries perfectly well, it just has no scoped reverse-footer kind.
func targetKindLabel(t Target) string {
	if t.Kind != KindUnknown {
		return humanKind(t.Kind)
	}
	if t.ARN != "" {
		svc, typ := arnService(t.ARN), arnResourceType(t.ARN)
		switch {
		case svc != "" && typ != "":
			return svc + " " + typ
		case svc != "":
			return svc + " resource"
		}
	}
	return "resource"
}

// RenderRelated writes a bidirectional related-resources result in the
// requested format. showUses / showUsedBy select which directions to print.
// partial is set when collection reported errors, so an empty side is flagged
// as possibly-incomplete rather than presented as a definitive "none" (§6a).
// Machine formats are intentionally unaffected by partial (scripting stability,
// §13) — it only changes the human table's empty-section wording and header.
func RenderRelated(w io.Writer, res RelatedResult, format string, noHeader, showUses, showUsedBy, partial bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderRelatedJSON(w, res, showUses, showUsedBy)
	case "ndjson":
		return renderRelatedNDJSON(w, res, showUses, showUsedBy)
	case "csv":
		return renderRelatedCSV(w, res, noHeader, showUses, showUsedBy)
	case "dot":
		return renderRelatedGraph(w, res, showUses, showUsedBy, dotStyle)
	case "mermaid":
		return renderRelatedGraph(w, res, showUses, showUsedBy, mermaidStyle)
	default:
		return renderRelatedTable(w, res, noHeader, showUses, showUsedBy, partial)
	}
}

// graphStyle abstracts the two graph dialects (#397) so the node/edge walk is
// written once. Uses → are drawn target→resource; Used by ← are drawn
// resource→target, matching the arrow direction of the relationship.
type graphStyle struct {
	header string
	footer string
	node   func(id, label string) string
	edge   func(from, to, label string) string
}

var dotStyle = graphStyle{
	header: "digraph related {\n  rankdir=LR;\n  node [shape=box];\n",
	footer: "}\n",
	node:   func(id, label string) string { return fmt.Sprintf("  %s [label=%q];\n", id, label) },
	edge:   func(from, to, label string) string { return fmt.Sprintf("  %s -> %s [label=%q];\n", from, to, label) },
}

var mermaidStyle = graphStyle{
	header: "graph LR\n",
	footer: "",
	node:   func(id, label string) string { return fmt.Sprintf("  %s[%q]\n", id, label) },
	edge: func(from, to, label string) string {
		return fmt.Sprintf("  %s -->|%s| %s\n", from, mermaidEdgeLabel(label), to)
	},
}

// renderRelatedGraph emits the result as a directed graph in the given dialect:
// a central target node, an edge out to each "uses" resource, and an edge in
// from each "used by" resource. (Multi-hop rows are drawn against the target
// labelled with their full path, since the intermediate nodes aren't retained
// in the flat result.)
func renderRelatedGraph(w io.Writer, res RelatedResult, showUses, showUsedBy bool, style graphStyle) error {
	if _, err := io.WriteString(w, style.header); err != nil {
		return err
	}
	ids := map[string]string{}
	nodeID := func(key, label string) (string, error) {
		if id, ok := ids[key]; ok {
			return id, nil
		}
		id := fmt.Sprintf("n%d", len(ids))
		ids[key] = id
		_, err := io.WriteString(w, style.node(id, label))
		return id, err
	}

	target, err := nodeID("target\x00"+res.Target.ID, targetLabel(res.Target))
	if err != nil {
		return err
	}
	edgeLabel := func(l Link) string {
		if l.Depth > 1 && l.Path != "" {
			return l.Path
		}
		return l.Via
	}
	emit := func(links []Link, fromTarget bool) error {
		for _, l := range links {
			n, err := nodeID(l.Service+"\x00"+l.ID, graphNodeLabel(l))
			if err != nil {
				return err
			}
			from, to := target, n
			if !fromTarget {
				from, to = n, target
			}
			if _, err := io.WriteString(w, style.edge(from, to, edgeLabel(l))); err != nil {
				return err
			}
		}
		return nil
	}
	if showUses {
		if err := emit(res.Uses, true); err != nil {
			return err
		}
	}
	if showUsedBy {
		if err := emit(res.UsedBy, false); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, style.footer)
	return err
}

func graphNodeLabel(l Link) string {
	name := refName(l.Reference)
	if l.Service != "" {
		return l.Service + ":" + l.Type + "\n" + name
	}
	return name
}

// mermaidEdgeLabel neutralizes characters that break a Mermaid `-->|...|` edge
// label (pipes, quotes, the ▸ path separator).
func mermaidEdgeLabel(s string) string {
	r := strings.NewReplacer("|", "/", "\"", "'", "▸", ">", "\n", " ")
	out := strings.TrimSpace(r.Replace(s))
	if out == "" {
		return " "
	}
	return out
}

func renderRelatedTable(w io.Writer, res RelatedResult, noHeader, showUses, showUsedBy, partial bool) error {
	if !noHeader {
		fmt.Fprintf(w, "Related resources for %s (%s)\n", targetLabel(res.Target), targetKindLabel(res.Target))
		fmt.Fprintln(w, "Two lists follow: what this resource depends on, and what depends on it.")
		if res.Depth > 1 {
			fmt.Fprintf(w, "Following links up to %d hop(s) away.\n", res.Depth)
		}
		if note := unknownTargetNote(res.Target); note != "" {
			fmt.Fprintf(w, "%s\n", note)
		}
		fmt.Fprintln(w)
	}

	if showUses {
		if err := renderLinkSection(w, "Depends on →  (what this resource uses)", "uses", res.Uses, res.Depth, partial, res.AllPaths, noHeader); err != nil {
			return err
		}
		if !noHeader {
			fmt.Fprintln(w)
		}
	}
	if showUsedBy {
		if err := renderLinkSection(w, "Used by ←  (what uses this resource)", "used_by", res.UsedBy, res.Depth, partial, res.AllPaths, noHeader); err != nil {
			return err
		}
		if !noHeader {
			if len(res.CheckedTypes) > 0 {
				// Explain what the list is: the kinds of links searched for, so an
				// empty "Used by" reads as "none of these", not "definitely none".
				// One per line — a long comma list is hard to scan.
				fmt.Fprintln(w, "\nFor \"Used by\", the tool searched for these kinds of links:")
				for _, ct := range res.CheckedTypes {
					fmt.Fprintf(w, "  • %s\n", ct)
				}
			}
			fmt.Fprintln(w)
		}
	}
	// Human annotations (the honesty caveat, §390) are part of the report, not
	// the data — suppress them under --no-header so the output is clean rows.
	if !noHeader {
		fmt.Fprintf(w, "%s\n", relatedCaveat)
	}
	return nil
}

// unknownTargetNote returns a one-line hint when the queried identifier isn't
// one of the supported kinds, so an empty result reads as "not a supported
// target" rather than a real "isolated" answer. VPC-style ids get a pointer to
// the command that actually lists their contents.
func unknownTargetNote(t Target) string {
	if t.Kind != KindUnknown {
		return ""
	}
	if strings.HasPrefix(t.Input, "vpc-") {
		return "Note: 'related' walks resource-to-resource references, not VPC membership. " +
			"To list resources inside a VPC, use 'aws_explorer vpc'."
	}
	if t.ARN != "" {
		// A real ARN (Lambda, S3, RDS, …) queries fine; it just isn't one of the
		// four kinds that get a scoped "Used by" footer. No warning needed — the
		// bottom caveat already states the honesty contract.
		return ""
	}
	return "Note: this identifier isn't a supported target kind (IAM role, KMS key, " +
		"ACM certificate, security group); only edges to/from it are shown."
}

// renderLinkSection renders one direction. In human mode it prints a section
// title, a column header, and a cosmetic SNO column. Under --no-header it emits
// only data rows, dropping the title/header/SNO and instead prefixing each row
// with the direction so rows stay self-describing for scripts (#391). direction
// is the token used for that column ("uses" / "used_by").
func renderLinkSection(w io.Writer, title, direction string, links []Link, maxDepth int, partial, showPath, noHeader bool) error {
	if !noHeader {
		fmt.Fprintf(w, "%s\n", title)
	}
	if len(links) == 0 {
		// In script mode an empty direction is simply no rows; the human note
		// would be noise.
		if !noHeader {
			if partial {
				fmt.Fprintf(w, "  (nothing found — the scan hit errors, so this may be incomplete)\n")
			} else {
				fmt.Fprintf(w, "  (nothing found)\n")
			}
		}
		return nil
	}
	// With --show-paths all the same resource can appear via several distinct
	// paths; show the full path chain (and label the column PATH) so the rows
	// are visually distinguishable rather than looking like duplicates (#388).
	// "RELATIONSHIP" reads clearer than "VIA" for newcomers.
	relHeader, rel := "RELATIONSHIP", func(l Link) string { return dash(l.Via) }
	if showPath {
		relHeader, rel = "PATH", func(l Link) string { return dash(l.Path) }
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := func() []string {
		cols := []string{"SNO"}
		if maxDepth > 1 {
			cols = append(cols, "HOP")
		}
		return append(cols, "SERVICE", "TYPE", "RESOURCE", "REGION", relHeader)
	}
	row := func(i int, l Link) []string {
		var cols []string
		if noHeader {
			// Direction replaces the cosmetic SNO so script rows self-describe
			// without a header line (SNO is human-only per §13).
			cols = append(cols, direction)
		} else {
			cols = append(cols, fmt.Sprintf("%d", i+1))
		}
		if maxDepth > 1 {
			cols = append(cols, depthLabel(l.Depth))
		}
		return append(cols, l.Service, l.Type, dash(refName(l.Reference)), dash(l.Region), rel(l))
	}

	if !noHeader {
		fmt.Fprintln(tw, strings.Join(header(), "\t"))
	}
	for i, l := range links {
		fmt.Fprintln(tw, strings.Join(row(i, l), "\t"))
	}
	return tw.Flush()
}

func renderRelatedJSON(w io.Writer, res RelatedResult, showUses, showUsedBy bool) error {
	out := res
	if !showUses {
		out.Uses = nil
	} else if out.Uses == nil {
		out.Uses = []Link{}
	}
	if !showUsedBy {
		out.UsedBy = nil
		out.CheckedTypes = nil
	} else if out.UsedBy == nil {
		out.UsedBy = []Link{}
	}
	if out.CheckedTypes == nil && showUsedBy {
		out.CheckedTypes = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// renderRelatedNDJSON emits one object per link, tagged with its direction, so
// the two directions stay distinguishable in a flat stream.
func renderRelatedNDJSON(w io.Writer, res RelatedResult, showUses, showUsedBy bool) error {
	enc := json.NewEncoder(w)
	emit := func(direction string, links []Link) error {
		for _, l := range links {
			row := struct {
				Direction string `json:"direction"`
				Link
			}{Direction: direction, Link: l}
			if err := enc.Encode(row); err != nil {
				return err
			}
		}
		return nil
	}
	if showUses {
		if err := emit("uses", res.Uses); err != nil {
			return err
		}
	}
	if showUsedBy {
		if err := emit("used_by", res.UsedBy); err != nil {
			return err
		}
	}
	return nil
}

func renderRelatedCSV(w io.Writer, res RelatedResult, noHeader, showUses, showUsedBy bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Direction", "Depth", "Service", "Type", "ID", "Name", "Region", "Via"}); err != nil {
			return err
		}
	}
	write := func(direction string, links []Link) error {
		for _, l := range links {
			rec := csvexport.SanitizeRow([]string{direction, depthLabel(l.Depth), l.Service, l.Type, l.ID, l.Name, l.Region, l.Via})
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
		return nil
	}
	if showUses {
		if err := write("uses", res.Uses); err != nil {
			return err
		}
	}
	if showUsedBy {
		if err := write("used_by", res.UsedBy); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
