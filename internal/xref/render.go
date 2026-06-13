package xref

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Render writes the result to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output.
func Render(w io.Writer, res Result, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, res)
	case "ndjson":
		return renderNDJSON(w, res)
	case "csv":
		return renderCSV(w, res, noHeader)
	default:
		return renderTable(w, res, noHeader)
	}
}

func renderTable(w io.Writer, res Result, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintf(w, "Where-used: %s (%s)\n\n", targetLabel(res.Target), res.Target.Kind)
	}
	if len(res.References) == 0 {
		fmt.Fprintf(w, "Not referenced by anything this tool checked.\n")
	} else {
		fmt.Fprintf(tw, "SNO\tSERVICE\tTYPE\tRESOURCE\tREGION\tVIA\n")
		for i, r := range res.References {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
				i+1, r.Service, r.Type, dash(refName(r)), dash(r.Region), r.Via)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if len(res.CheckedTypes) > 0 {
		fmt.Fprintf(w, "\nReference types checked: %s.\n", strings.Join(res.CheckedTypes, ", "))
		fmt.Fprintf(w, "(Absence above means none of these reference it — not that nothing anywhere does.)\n")
	} else {
		fmt.Fprintf(w, "\nUnrecognized resource type — pass an IAM role, KMS key, ACM certificate, or security group (ARN or ID).\n")
	}
	return nil
}

func renderJSON(w io.Writer, res Result) error {
	if res.References == nil {
		res.References = []Reference{}
	}
	if res.CheckedTypes == nil {
		res.CheckedTypes = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func renderNDJSON(w io.Writer, res Result) error {
	enc := json.NewEncoder(w)
	for _, r := range res.References {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, res Result, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Service", "Type", "ID", "Name", "Region", "Via"}); err != nil {
			return err
		}
	}
	for _, r := range res.References {
		if err := cw.Write([]string{r.Service, r.Type, r.ID, r.Name, r.Region, r.Via}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func targetLabel(t Target) string {
	if t.ID != "" {
		return t.ID
	}
	if t.ARN != "" {
		return t.ARN
	}
	return t.Input
}

func refName(r Reference) string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
