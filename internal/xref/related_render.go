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
const relatedCaveat = "Only relationships this tool extracts are shown; un-collected link types won't appear."

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
	default:
		return renderRelatedTable(w, res, noHeader, showUses, showUsedBy, partial)
	}
}

func renderRelatedTable(w io.Writer, res RelatedResult, noHeader, showUses, showUsedBy, partial bool) error {
	if !noHeader {
		fmt.Fprintf(w, "Related: %s (%s)\n", targetLabel(res.Target), res.Target.Kind)
		if res.Depth > 1 {
			fmt.Fprintf(w, "Depth: up to %d hop(s)\n", res.Depth)
		}
		if note := unknownTargetNote(res.Target); note != "" {
			fmt.Fprintf(w, "%s\n", note)
		}
		fmt.Fprintln(w)
	}

	if showUses {
		if err := renderLinkSection(w, "Uses (depends on) →", res.Uses, res.Depth, partial, res.AllPaths); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}
	if showUsedBy {
		if err := renderLinkSection(w, "Used by ←", res.UsedBy, res.Depth, partial, res.AllPaths); err != nil {
			return err
		}
		if len(res.CheckedTypes) > 0 {
			fmt.Fprintf(w, "\nReference types checked: %s.\n", strings.Join(res.CheckedTypes, ", "))
		}
		fmt.Fprintln(w)
	}
	// The honesty caveat applies to the whole report — print it once at the
	// bottom rather than after each direction (#390).
	fmt.Fprintf(w, "%s\n", relatedCaveat)
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
	return "Note: this identifier isn't a supported target kind (IAM role, KMS key, " +
		"ACM certificate, security group); only edges to/from it are shown."
}

func renderLinkSection(w io.Writer, title string, links []Link, maxDepth int, partial, showPath bool) error {
	fmt.Fprintf(w, "%s\n", title)
	if len(links) == 0 {
		if partial {
			fmt.Fprintf(w, "  (none found — collection had errors; result may be incomplete)\n")
		} else {
			fmt.Fprintf(w, "  (none found)\n")
		}
		return nil
	}
	// With --show-paths all the same resource can appear via several distinct
	// paths; show the full path chain (and label the column PATH) so the rows
	// are visually distinguishable rather than looking like duplicates (#388).
	relHeader, rel := "VIA", func(l Link) string { return dash(l.Via) }
	if showPath {
		relHeader, rel = "PATH", func(l Link) string { return dash(l.Path) }
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if maxDepth > 1 {
		fmt.Fprintf(tw, "SNO\tHOP\tSERVICE\tTYPE\tRESOURCE\tREGION\t%s\n", relHeader)
		for i, l := range links {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				i+1, depthLabel(l.Depth), l.Service, l.Type, dash(refName(l.Reference)), dash(l.Region), rel(l))
		}
	} else {
		fmt.Fprintf(tw, "SNO\tSERVICE\tTYPE\tRESOURCE\tREGION\t%s\n", relHeader)
		for i, l := range links {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
				i+1, l.Service, l.Type, dash(refName(l.Reference)), dash(l.Region), rel(l))
		}
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
