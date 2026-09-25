package lambdatui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
)

// ActivityReport is a finished activity run, rendered by the CLI twin
// (`lambda activity`) and summarised by the TUI.
type ActivityReport struct {
	Query    ActivityQuery
	Stats    InvocationStats
	StatsErr error
	Scan     *LogScan // nil when no regex was given (metrics only)
	Now      time.Time
}

// formatCount renders a metric sum as a grouped integer ("12,345").
func formatCount(v float64) string {
	n := int64(math.Round(v))
	s := strconv.FormatInt(n, 10)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// countOrDash renders an hour's value, "-" for no datapoint.
func countOrDash(v float64) string {
	if math.IsNaN(v) {
		return "-"
	}
	return formatCount(v)
}

// matchTime renders a match's timestamp in the day's zone to the millisecond.
func matchTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("15:04:05.000")
}

// InvocationSummary is the one-line invocation answer shared by the CLI and
// the TUI header.
func (s InvocationStats) InvocationSummary() string {
	if !s.HasData {
		return "0 invocations — no Invocations datapoints for this day (not invoked, or older than CloudWatch's 455-day retention)"
	}
	return fmt.Sprintf("%s invocations · %s errors · %s throttles",
		formatCount(s.Invocations), formatCount(s.Errors), formatCount(s.Throttles))
}

// ScanSummary is the one-line log-scan answer shared by the CLI and the TUI.
func (s *LogScan) ScanSummary() string {
	starts := formatCount(float64(s.Starts)) + " START lines"
	if !s.StartsKnown() {
		switch {
		case s.Filter != "":
			starts = "START lines not counted (server-side filter)"
		case s.Done:
			starts = "≥" + starts + " (partial scan)"
		}
	}
	if matchesAll(s.Pattern) {
		return fmt.Sprintf("%s events read · %s", formatCount(float64(s.Events)), starts)
	}
	return fmt.Sprintf("%s events read · %s · %s matches", formatCount(float64(s.Events)), starts, formatCount(float64(len(s.Matches))))
}

// ScanNote is the scan's caveat line: why it stopped early, or the error that
// ended it ("" when the day was read in full).
func (s *LogScan) ScanNote() string {
	switch {
	case s.Err != nil:
		return "log scan failed after " + formatCount(float64(s.Events)) + " events: " + s.Err.Error()
	case s.StopReason != "":
		return s.StopReason
	}
	return ""
}

// groupHeader labels a capture-group column: named groups by name, numbered
// ones as $1, $2….
func groupHeader(name string) string {
	if _, err := strconv.Atoi(name); err == nil {
		return "$" + name
	}
	return name
}

// RenderActivity writes the report in the requested format. table prints the
// summary, the hourly breakdown and the matches; json the whole report; ndjson
// and csv one row per match (per hour when no regex was given).
func RenderActivity(w io.Writer, r ActivityReport, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, activityToJSON(r))
	case "ndjson":
		enc := json.NewEncoder(w)
		if r.Scan == nil {
			for _, h := range hoursToJSON(r.Stats) {
				if err := enc.Encode(h); err != nil {
					return err
				}
			}
			return nil
		}
		for _, m := range matchesToJSON(r.Scan, r.Query.Day.Start.Location()) {
			if err := enc.Encode(m); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		return renderActivityCSV(w, r, noHeader)
	default:
		return renderActivityTable(w, r, noHeader)
	}
}

func renderActivityTable(w io.Writer, r ActivityReport, noHeader bool) error {
	q := r.Query
	loc := q.Day.Start.Location()
	day := q.Day.Label()
	if q.Day.InProgress(r.Now) {
		day += " — day in progress, counts so far"
	}
	fmt.Fprintf(w, "Function:     %s (%s)\n", q.Function, q.Region)
	fmt.Fprintf(w, "Day:          %s\n", day)
	if r.StatsErr != nil {
		fmt.Fprintf(w, "Invocations:  unknown — CloudWatch metrics could not be read: %v\n", r.StatsErr)
	} else {
		fmt.Fprintf(w, "Invocations:  %s\n", r.Stats.InvocationSummary())
		if r.Stats.Realigned {
			fmt.Fprintln(w, "              (CloudWatch returned UTC-hour buckets for this date; the day's edges are approximate)")
		}
	}
	if r.Scan != nil {
		fmt.Fprintf(w, "Log group:    %s\n", q.LogGroup)
		fmt.Fprintf(w, "Pattern:      /%s/", q.Pattern.String())
		if q.Filter != "" {
			fmt.Fprintf(w, "  (server-side filter %q)", q.Filter)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Log scan:     %s\n", r.Scan.ScanSummary())
		if note := r.Scan.ScanNote(); note != "" {
			fmt.Fprintf(w, "              %s\n", note)
		}
	}

	if r.StatsErr == nil && r.Stats.HasData {
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "HOUR\tINVOCATIONS\tERRORS\tTHROTTLES")
		}
		for _, h := range r.Stats.Hours {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Start.In(loc).Format("15:04"),
				countOrDash(h.Invocations), countOrDash(h.Errors), countOrDash(h.Throttles))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w, "(- = no datapoint: not invoked that hour)")
	}

	if r.Scan == nil {
		return nil
	}
	fmt.Fprintln(w)
	if len(r.Scan.Matches) == 0 {
		fmt.Fprintln(w, "No log events matched.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeader {
		hdr := []string{"TIME", "REQUEST ID", "LEVEL"}
		for _, g := range r.Scan.GroupNames {
			hdr = append(hdr, strings.ToUpper(groupHeader(g)))
		}
		if len(r.Scan.GroupNames) == 0 {
			hdr = append(hdr, "MATCH")
		}
		hdr = append(hdr, "MESSAGE")
		fmt.Fprintln(tw, strings.Join(hdr, "\t"))
	}
	for _, m := range r.Scan.Matches {
		row := []string{matchTime(m.Time, loc), dash(m.RequestID), dash(m.Level)}
		for _, g := range m.Groups {
			row = append(row, dash(oneLine(g)))
		}
		if len(r.Scan.GroupNames) == 0 {
			row = append(row, dash(oneLine(m.Matched)))
		}
		row = append(row, oneLine(m.Body))
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

// oneLine flattens a multi-line log message (stack traces) for a table cell.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	return strings.ReplaceAll(s, "\t", " ")
}

func renderActivityCSV(w io.Writer, r ActivityReport, noHeader bool) error {
	cw := csv.NewWriter(w)
	write := func(row []string) { _ = cw.Write(csvexport.SanitizeRow(row)) }
	if r.Scan == nil {
		if !noHeader {
			write([]string{"HourStart", "Invocations", "Errors", "Throttles"})
		}
		blank := func(v float64) string {
			if math.IsNaN(v) {
				return ""
			}
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		for _, h := range r.Stats.Hours {
			write([]string{h.Start.Format(time.RFC3339), blank(h.Invocations), blank(h.Errors), blank(h.Throttles)})
		}
		cw.Flush()
		return cw.Error()
	}
	if !noHeader {
		hdr := []string{"Time", "RequestId", "Level"}
		for _, g := range r.Scan.GroupNames {
			hdr = append(hdr, "Group_"+g)
		}
		hdr = append(hdr, "Matched", "Message", "LogStream")
		write(hdr)
	}
	loc := r.Query.Day.Start.Location()
	for _, m := range r.Scan.Matches {
		row := []string{m.Time.In(loc).Format("2006-01-02T15:04:05.000Z07:00"), m.RequestID, m.Level}
		row = append(row, m.Groups...)
		row = append(row, m.Matched, m.Message, m.Stream)
		write(row)
	}
	cw.Flush()
	return cw.Error()
}

// --- JSON ------------------------------------------------------------------

type hourJSON struct {
	HourStart   string   `json:"hourStart"`
	Invocations *float64 `json:"invocations"` // null = no datapoint that hour
	Errors      *float64 `json:"errors"`
	Throttles   *float64 `json:"throttles"`
}

type matchJSON struct {
	Time              string            `json:"time"`
	RequestID         string            `json:"requestId,omitempty"`
	RequestIDInferred bool              `json:"requestIdInferred,omitempty"`
	Level             string            `json:"level,omitempty"`
	Matched           string            `json:"matched"`
	Groups            map[string]string `json:"groups,omitempty"`
	Message           string            `json:"message"`
	LogStream         string            `json:"logStream"`
}

type scanJSON struct {
	LogGroup          string      `json:"logGroup"`
	Pattern           string      `json:"pattern"`
	FilterPattern     string      `json:"filterPattern,omitempty"`
	EventsRead        int         `json:"eventsRead"`
	StartLines        int         `json:"startLines"`
	StartLinesPartial bool        `json:"startLinesPartial"`
	Complete          bool        `json:"complete"`
	StopReason        string      `json:"stopReason,omitempty"`
	Error             string      `json:"error,omitempty"`
	Matches           []matchJSON `json:"matches"`
}

type activityJSON struct {
	Function      string     `json:"function"`
	Region        string     `json:"region"`
	Date          string     `json:"date"`
	Start         string     `json:"start"`
	End           string     `json:"end"`
	DayInProgress bool       `json:"dayInProgress"`
	Invocations   *float64   `json:"invocations"` // null when the metrics could not be read
	Errors        *float64   `json:"errors"`
	Throttles     *float64   `json:"throttles"`
	HasMetricData bool       `json:"hasMetricData"`
	MetricsError  string     `json:"metricsError,omitempty"`
	Hourly        []hourJSON `json:"hourly,omitempty"`
	LogScan       *scanJSON  `json:"logScan,omitempty"`
}

func ptrUnlessNaN(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

func hoursToJSON(s InvocationStats) []hourJSON {
	out := make([]hourJSON, 0, len(s.Hours))
	for _, h := range s.Hours {
		out = append(out, hourJSON{
			HourStart:   h.Start.Format(time.RFC3339),
			Invocations: ptrUnlessNaN(h.Invocations),
			Errors:      ptrUnlessNaN(h.Errors),
			Throttles:   ptrUnlessNaN(h.Throttles),
		})
	}
	return out
}

func matchesToJSON(s *LogScan, loc *time.Location) []matchJSON {
	out := make([]matchJSON, 0, len(s.Matches))
	for _, m := range s.Matches {
		mj := matchJSON{
			Time:              m.Time.In(loc).Format("2006-01-02T15:04:05.000Z07:00"),
			RequestID:         m.RequestID,
			RequestIDInferred: m.RequestIDInferred,
			Level:             m.Level,
			Matched:           m.Matched,
			Message:           m.Message,
			LogStream:         m.Stream,
		}
		if len(m.Groups) > 0 {
			mj.Groups = make(map[string]string, len(m.Groups))
			for i, g := range m.Groups {
				if i < len(s.GroupNames) {
					mj.Groups[s.GroupNames[i]] = g
				}
			}
		}
		out = append(out, mj)
	}
	return out
}

func activityToJSON(r ActivityReport) activityJSON {
	q := r.Query
	out := activityJSON{
		Function:      q.Function,
		Region:        q.Region,
		Date:          q.Day.Date,
		Start:         q.Day.Start.Format(time.RFC3339),
		End:           q.Day.End.Format(time.RFC3339),
		DayInProgress: q.Day.InProgress(r.Now),
	}
	if r.StatsErr != nil {
		out.MetricsError = r.StatsErr.Error()
	} else {
		inv, errs, thr := r.Stats.Invocations, r.Stats.Errors, r.Stats.Throttles
		out.Invocations, out.Errors, out.Throttles = &inv, &errs, &thr
		out.HasMetricData = r.Stats.HasData
		out.Hourly = hoursToJSON(r.Stats)
	}
	if s := r.Scan; s != nil {
		sj := &scanJSON{
			LogGroup:          q.LogGroup,
			Pattern:           q.Pattern.String(),
			FilterPattern:     q.Filter,
			EventsRead:        s.Events,
			StartLines:        s.Starts,
			StartLinesPartial: !s.StartsKnown(),
			Complete:          s.Done && s.StopReason == "" && s.Err == nil,
			StopReason:        s.StopReason,
			Matches:           matchesToJSON(s, q.Day.Start.Location()),
		}
		if s.Err != nil {
			sj.Error = s.Err.Error()
		}
		out.LogScan = sj
	}
	return out
}
