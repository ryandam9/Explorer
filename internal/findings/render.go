package findings

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ryandam9/aws_explorer/internal/costs"
	"github.com/ryandam9/aws_explorer/internal/csvexport"
)

// Render writes findings to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output. Table and json output include the total estimated
// monthly cost; ndjson and csv stay one-finding-per-record for scripting.
func Render(w io.Writer, fs []Finding, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, fs)
	case "ndjson":
		return renderNDJSON(w, fs)
	case "csv":
		return renderCSV(w, fs, noHeader)
	default:
		return renderTable(w, fs, noHeader)
	}
}

func renderTable(w io.Writer, fs []Finding, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SNO\tSEVERITY\tID\tRESOURCE\tREGION\tISSUE\tEST/MO\tFIX")
	}
	for i, f := range fs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, f.Severity.Badge(), f.ID, f.Resource, f.Region, f.Title,
			costs.Dollars(f.EstMonthlyUSD), f.Fix)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if total := TotalMonthlyUSD(fs); total > 0 {
		fmt.Fprintf(w, "\n%s — estimated potential savings ≈ %s/month\n",
			Summary(fs), costs.Dollars(total))
	} else {
		fmt.Fprintf(w, "\n%s\n", Summary(fs))
	}
	return nil
}

// jsonReport is the envelope for -o json: the findings plus the total, so
// scripts don't have to re-sum.
type jsonReport struct {
	Findings        []Finding `json:"findings"`
	TotalMonthlyUSD float64   `json:"totalMonthlyUSD"`
}

func renderJSON(w io.Writer, fs []Finding) error {
	if fs == nil {
		fs = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonReport{Findings: fs, TotalMonthlyUSD: TotalMonthlyUSD(fs)})
}

func renderNDJSON(w io.Writer, fs []Finding) error {
	enc := json.NewEncoder(w)
	for _, f := range fs {
		if err := enc.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, fs []Finding, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Severity", "ID", "Resource", "Region", "Issue", "Detail", "EstMonthlyUSD", "Fix"}); err != nil {
			return err
		}
	}
	for _, f := range fs {
		rec := []string{
			f.Severity.String(), f.ID, f.Resource, f.Region, f.Title, f.Detail,
			strconv.FormatFloat(f.EstMonthlyUSD, 'f', 2, 64), f.Fix,
		}
		if err := cw.Write(csvexport.SanitizeRow(rec)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
