package acctsnap

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Report is the machine-readable shape of a diff: the baseline's metadata
// plus the changes, so automation can tell *since when* things changed.
type Report struct {
	BaselineTakenAt string   `json:"baselineTakenAt"`
	AccountID       string   `json:"accountId,omitempty"`
	Regions         []string `json:"regions"`
	Added           int      `json:"added"`
	Removed         int      `json:"removed"`
	Modified        int      `json:"modified"`
	Changes         []Change `json:"changes"`
}

// NewReport pairs a baseline with the changes found against it.
func NewReport(baseline Snapshot, changes []Change) Report {
	if changes == nil {
		changes = []Change{}
	}
	added, removed, modified := Counts(changes)
	return Report{
		BaselineTakenAt: baseline.TakenAt.UTC().Format("2006-01-02 15:04 MST"),
		AccountID:       baseline.AccountID,
		Regions:         baseline.Regions,
		Added:           added,
		Removed:         removed,
		Modified:        modified,
		Changes:         changes,
	}
}

// Render writes the report to w in the requested format (table, json,
// ndjson, csv). Unknown formats fall back to table. noHeader omits the
// summary line in table output and the header row in csv.
func Render(w io.Writer, rep Report, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, c := range rep.Changes {
			if err := enc.Encode(c); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		return renderCSV(w, rep, noHeader)
	default:
		return renderTable(w, rep, noHeader)
	}
}

func renderTable(w io.Writer, rep Report, noHeader bool) error {
	if !noHeader {
		fmt.Fprintf(w, "Changes since baseline %s — %d added, %d removed, %d modified\n",
			rep.BaselineTakenAt, rep.Added, rep.Removed, rep.Modified)
	}
	if len(rep.Changes) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, c := range rep.Changes {
		fmt.Fprintf(tw, "%s %s\t%s\t%s\t%s\n",
			glyph(c.Kind), c.Type, resourceLabel(c), region(c), strings.Join(c.Deltas, "; "))
	}
	return tw.Flush()
}

func renderCSV(w io.Writer, rep Report, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Kind", "Type", "Name", "ID", "Region", "Deltas"}); err != nil {
			return err
		}
	}
	for _, c := range rep.Changes {
		rec := []string{c.Kind, c.Type, c.Name, c.ID, c.Region, strings.Join(c.Deltas, "; ")}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func glyph(kind string) string {
	switch kind {
	case KindAdded:
		return "+"
	case KindRemoved:
		return "-"
	default:
		return "~"
	}
}

// resourceLabel shows the most recognizable handle: "id (name)" when both
// exist and differ, otherwise whichever is present.
func resourceLabel(c Change) string {
	switch {
	case c.ID != "" && c.Name != "" && c.ID != c.Name:
		return c.ID + " (" + c.Name + ")"
	case c.ID != "":
		return c.ID
	case c.Name != "":
		return c.Name
	default:
		return c.Key
	}
}

func region(c Change) string {
	if c.Region == "" {
		return "global"
	}
	return c.Region
}
