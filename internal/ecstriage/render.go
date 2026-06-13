package ecstriage

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Render writes the records to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output.
func Render(w io.Writer, recs []Record, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, recs)
	case "ndjson":
		return renderNDJSON(w, recs)
	case "csv":
		return renderCSV(w, recs, noHeader)
	default:
		return renderTable(w, recs, noHeader)
	}
}

func stoppedAt(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func renderTable(w io.Writer, recs []Record, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SNO\tSTOPPED AT (UTC)\tCLUSTER\tTASK\tREASON\tCONTAINER\tEXIT")
	}
	for i, r := range recs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, stoppedAt(r.StoppedAt), r.Cluster, r.Task,
			truncate(r.Reason, 60), dash(r.Container), r.ExitDisplay())
	}
	return tw.Flush()
}

func renderJSON(w io.Writer, recs []Record) error {
	if recs == nil {
		recs = []Record{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(recs)
}

func renderNDJSON(w io.Writer, recs []Record) error {
	enc := json.NewEncoder(w)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, recs []Record, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"StoppedAt", "Region", "Cluster", "Task", "Group", "StopCode", "Reason", "Container", "ExitCode", "ExitNote"}); err != nil {
			return err
		}
	}
	for _, r := range recs {
		exit := ""
		if r.ExitCode != nil {
			exit = strconv.FormatInt(int64(*r.ExitCode), 10)
		}
		rec := []string{
			stoppedAt(r.StoppedAt), r.Region, r.Cluster, r.Task,
			r.Group, r.StopCode, r.Reason, r.Container, exit, r.ExitNote,
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
