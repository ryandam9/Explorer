package tagstui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/model"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// RenderStrings writes a simple list (tag keys, or a key's values) in the chosen
// format. columnHeader names the single column for table/CSV output.
func RenderStrings(w io.Writer, items []string, columnHeader, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		if items == nil {
			items = []string{}
		}
		return writeJSON(w, items)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, s := range items {
			if err := enc.Encode(s); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{columnHeader})
		}
		for _, s := range items {
			_ = cw.Write(csvexport.SanitizeRow([]string{s}))
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, strings.ToUpper(columnHeader))
		}
		for _, s := range items {
			fmt.Fprintln(tw, s)
		}
		return tw.Flush()
	}
}

// RenderResources writes tagged resources in the chosen format. CSV cells are
// sanitized against formula injection (§13); cosmetic columns are never added to
// machine output.
func RenderResources(w io.Writer, res []model.Resource, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		if res == nil {
			res = []model.Resource{}
		}
		return writeJSON(w, res)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, r := range res {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Service", "Type", "Name", "Region", "AccountID", "ID", "ARN", "Tags"})
		}
		for _, r := range res {
			_ = cw.Write(csvexport.SanitizeRow([]string{
				r.Service, r.Type, r.Name, r.Region, r.AccountID, r.ID, r.ARN, tagsString(r.Tags),
			}))
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "SERVICE\tTYPE\tNAME\tREGION\tID")
		}
		for _, r := range res {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Service, r.Type, dash(r.Name), r.Region, r.ID)
		}
		return tw.Flush()
	}
}

// tagsString renders a resource's tags as a stable "k=v;k2=v2" string for CSV.
func tagsString(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+tags[k])
	}
	return strings.Join(parts, ";")
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
