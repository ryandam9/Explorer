package trail

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Render writes the events to w in the requested format (table, json, ndjson,
// csv). Unknown formats fall back to table. noHeader omits the header row in
// table and csv output.
func Render(w io.Writer, events []Event, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, events)
	case "ndjson":
		return renderNDJSON(w, events)
	case "csv":
		return renderCSV(w, events, noHeader)
	default:
		return renderTable(w, events, noHeader)
	}
}

func renderTable(w io.Writer, events []Event, noHeader bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		fmt.Fprintln(tw, "SNO\tTIME\tEVENT\tPRINCIPAL\tSOURCE IP\tOUTCOME")
	}
	for i, ev := range events {
		name := ev.EventName
		if ev.ReadOnly {
			name += " (read)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, ev.Time.UTC().Format("2006-01-02 15:04:05"), name, ev.Principal, ev.SourceIP, outcome(ev))
	}
	return tw.Flush()
}

// outcome is the event's result shown in the table: "ok" for a successful
// call, or the failure's errorCode so denied/failed attempts stand out.
func outcome(ev Event) string {
	if ev.ErrorCode != "" {
		return ev.ErrorCode
	}
	return "ok"
}

func renderJSON(w io.Writer, events []Event) error {
	if events == nil {
		events = []Event{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(events)
}

func renderNDJSON(w io.Writer, events []Event) error {
	enc := json.NewEncoder(w)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, events []Event, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write([]string{"Time", "Event", "Principal", "SourceIP", "ReadOnly", "ErrorCode"}); err != nil {
			return err
		}
	}
	for _, ev := range events {
		rec := []string{
			ev.Time.UTC().Format("2006-01-02T15:04:05Z"),
			ev.EventName, ev.Principal, ev.SourceIP, strconv.FormatBool(ev.ReadOnly), ev.ErrorCode,
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
