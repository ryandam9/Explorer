package quotas

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

// Render writes the rows to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output.
func Render(w io.Writer, rows []Row, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, rows)
	case "ndjson":
		return renderNDJSON(w, rows)
	case "csv":
		return renderCSV(w, rows, noHeader)
	default:
		return renderTable(w, rows, noHeader)
	}
}

func renderTable(w io.Writer, rows []Row, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SNO\tQUOTA\tREGION\tUSED / LIMIT\t%\tSTATUS")
	}
	for i, r := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, r.Name, r.Region, usageCell(r), pctCell(r), statusBadge(r.Status))
	}
	return tw.Flush()
}

func usageCell(r Row) string {
	limit := trimFloat(r.Limit)
	if r.Unit != "" {
		limit += " " + r.Unit
	}
	if r.FromDefault {
		// The applied quota couldn't be read; this is AWS's generic default, so
		// a near-limit reading may be wrong. Flag it (§8) — already in JSON/CSV.
		limit += " (default)"
	}
	if r.Used == nil {
		return "— / " + limit
	}
	return trimFloat(*r.Used) + " / " + limit
}

func pctCell(r Row) string {
	if r.Percent == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *r.Percent)
}

func statusBadge(s Status) string {
	switch s {
	case StatusCritical:
		return "CRITICAL"
	case StatusWarn:
		return "WARN"
	case StatusUnknown:
		return "no usage metric"
	default:
		return "ok"
	}
}

func renderJSON(w io.Writer, rows []Row) error {
	if rows == nil {
		rows = []Row{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func renderNDJSON(w io.Writer, rows []Row) error {
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, rows []Row, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Quota", "Service", "Region", "Used", "Limit", "Unit", "Percent", "Status", "FromDefault"}); err != nil {
			return err
		}
	}
	for _, r := range rows {
		used := ""
		if r.Used != nil {
			used = trimFloat(*r.Used)
		}
		pct := ""
		if r.Percent != nil {
			pct = fmt.Sprintf("%.1f", *r.Percent)
		}
		rec := []string{
			r.Name, r.Service, r.Region, used, trimFloat(r.Limit), r.Unit, pct,
			string(r.Status), strconv.FormatBool(r.FromDefault),
		}
		if err := cw.Write(csvexport.SanitizeRow(rec)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// trimFloat formats a float without a trailing ".0" for whole numbers.
func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
