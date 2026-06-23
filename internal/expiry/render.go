package expiry

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
)

// Render writes the items to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output.
func Render(w io.Writer, items []Item, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, items)
	case "ndjson":
		return renderNDJSON(w, items)
	case "csv":
		return renderCSV(w, items, noHeader)
	default:
		return renderTable(w, items, noHeader)
	}
}

func renderTable(w io.Writer, items []Item, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SNO\t DAYS\tWHAT\tRESOURCE\tREGION\tDETAIL")
	}
	for i, it := range items {
		fmt.Fprintf(tw, "%d\t%5d\t%s\t%s\t%s\t%s\n",
			i+1, it.Days, it.Kind, it.Resource, it.Region, it.Detail)
	}
	return tw.Flush()
}

func renderJSON(w io.Writer, items []Item) error {
	if items == nil {
		items = []Item{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func renderNDJSON(w io.Writer, items []Item) error {
	enc := json.NewEncoder(w)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, items []Item, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Days", "Date", "What", "Resource", "Region", "Detail"}); err != nil {
			return err
		}
	}
	for _, it := range items {
		rec := []string{
			strconv.Itoa(it.Days), it.Date.Format("2006-01-02"),
			it.Kind, it.Resource, it.Region, it.Detail,
		}
		if err := cw.Write(csvexport.SanitizeRow(rec)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
