package billing

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Render writes the bill to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output. ndjson emits one line item per line (the period and
// total are derivable), the other formats end with the total.
func Render(w io.Writer, b *Bill, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, b)
	case "ndjson":
		return renderNDJSON(w, b)
	case "csv":
		return renderCSV(w, b, noHeader)
	default:
		return renderTable(w, b, noHeader)
	}
}

func renderTable(w io.Writer, b *Bill, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SERVICE\tUSAGE TYPE\tUSAGE\tUNIT\tCOST")
	}
	for _, l := range b.Lines {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			l.Service, l.UsageType, FormatQty(l.Quantity), l.Unit, FormatAmount(l.Amount, b.Currency))
	}
	total := "TOTAL"
	if b.Estimated {
		total += " (estimated)"
	}
	fmt.Fprintf(tw, "%s\t%s → %s\t\t\t%s\n",
		total, b.Start.Format(dateFmt), b.End.Format(dateFmt), FormatAmount(b.Total, b.Currency))
	return tw.Flush()
}

func renderJSON(w io.Writer, b *Bill) error {
	out := *b
	if out.Lines == nil {
		out.Lines = []Line{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderNDJSON(w io.Writer, b *Bill) error {
	enc := json.NewEncoder(w)
	for _, l := range b.Lines {
		if err := enc.Encode(l); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, b *Bill, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Service", "UsageType", "Usage", "Unit", "Cost", "Currency"}); err != nil {
			return err
		}
	}
	for _, l := range b.Lines {
		rec := []string{
			l.Service, l.UsageType,
			strconv.FormatFloat(l.Quantity, 'f', -1, 64), l.Unit,
			strconv.FormatFloat(l.Amount, 'f', -1, 64), b.Currency,
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
