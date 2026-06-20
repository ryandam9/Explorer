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
func RenderRelated(w io.Writer, res RelatedResult, format string, noHeader, showUses, showUsedBy bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderRelatedJSON(w, res, showUses, showUsedBy)
	case "ndjson":
		return renderRelatedNDJSON(w, res, showUses, showUsedBy)
	case "csv":
		return renderRelatedCSV(w, res, noHeader, showUses, showUsedBy)
	default:
		return renderRelatedTable(w, res, noHeader, showUses, showUsedBy)
	}
}

func renderRelatedTable(w io.Writer, res RelatedResult, noHeader, showUses, showUsedBy bool) error {
	if !noHeader {
		fmt.Fprintf(w, "Related: %s (%s)\n", targetLabel(res.Target), res.Target.Kind)
		if res.Depth > 1 {
			fmt.Fprintf(w, "Depth: up to %d hop(s)\n", res.Depth)
		}
		fmt.Fprintln(w)
	}

	if showUses {
		if err := renderLinkSection(w, "Uses (depends on) →", res.Uses, res.Depth); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n%s\n\n", relatedCaveat)
	}
	if showUsedBy {
		if err := renderLinkSection(w, "Used by ←", res.UsedBy, res.Depth); err != nil {
			return err
		}
		if len(res.CheckedTypes) > 0 {
			fmt.Fprintf(w, "\nReference types checked: %s.\n", strings.Join(res.CheckedTypes, ", "))
		}
		fmt.Fprintf(w, "\n%s\n", relatedCaveat)
	}
	return nil
}

func renderLinkSection(w io.Writer, title string, links []Link, maxDepth int) error {
	fmt.Fprintf(w, "%s\n", title)
	if len(links) == 0 {
		fmt.Fprintf(w, "  (none found)\n")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if maxDepth > 1 {
		fmt.Fprintf(tw, "SNO\tHOP\tSERVICE\tTYPE\tRESOURCE\tREGION\tVIA\n")
		for i, l := range links {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				i+1, depthLabel(l.Depth), l.Service, l.Type, dash(refName(l.Reference)), dash(l.Region), dash(l.Via))
		}
	} else {
		fmt.Fprintf(tw, "SNO\tSERVICE\tTYPE\tRESOURCE\tREGION\tVIA\n")
		for i, l := range links {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
				i+1, l.Service, l.Type, dash(refName(l.Reference)), dash(l.Region), dash(l.Via))
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
