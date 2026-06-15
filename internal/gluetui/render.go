package gluetui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ryandam9/aws_explorer/internal/costs"
)

// These Render* functions back the CLI twins of the dashboard (AXE-030). They
// render the same typed data as plain table / JSON / NDJSON / CSV — no glyphs or
// colour, so output pipes cleanly.

func isoOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func tableTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- Jobs ------------------------------------------------------------------

type jobJSON struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	ARN          string `json:"arn"`
	LastRunState string `json:"lastRunState,omitempty"`
	LastRun      string `json:"lastRunStarted,omitempty"`
	DurationSecs int32  `json:"lastRunDurationSeconds,omitempty"`
	Worker       string `json:"worker,omitempty"`
	GlueVersion  string `json:"glueVersion,omitempty"`
}

func jobsToJSON(jobs []Job) []jobJSON {
	out := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobJSON{
			Name: j.Name, Region: j.Region, ARN: j.ARN,
			LastRunState: j.LastRunState, LastRun: isoOrEmpty(j.LastRunStarted),
			DurationSecs: j.LastRunSeconds, Worker: j.Worker, GlueVersion: j.GlueVersion,
		})
	}
	return out
}

// RenderJobs writes jobs in the requested format.
func RenderJobs(w io.Writer, jobs []Job, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, jobsToJSON(jobs))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, j := range jobsToJSON(jobs) {
			if err := enc.Encode(j); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "LastRunState", "LastRunStarted", "DurationSeconds", "Worker", "GlueVersion", "ARN"})
		}
		for _, j := range jobs {
			_ = cw.Write([]string{j.Name, j.Region, j.LastRunState, isoOrEmpty(j.LastRunStarted), strconv.Itoa(int(j.LastRunSeconds)), j.Worker, j.GlueVersion, j.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tLAST RUN\tSTATE\tDURATION\tWORKER\tVERSION")
		}
		for _, j := range jobs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				j.Name, j.Region, tableTime(j.LastRunStarted), dash(j.LastRunState),
				formatDuration(j.LastRunSeconds), dash(j.Worker), dash(j.GlueVersion))
		}
		return tw.Flush()
	}
}

// --- Crawlers --------------------------------------------------------------

func RenderCrawlers(w io.Writer, crawlers []Crawler, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, crawlers)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, c := range crawlers {
			if err := enc.Encode(c); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "State", "LastCrawlStatus", "Database", "Schedule", "ARN"})
		}
		for _, c := range crawlers {
			_ = cw.Write([]string{c.Name, c.Region, c.State, c.LastCrawlStatus, c.Database, c.Schedule, c.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tSTATE\tLAST CRAWL\tDATABASE\tSCHEDULE")
		}
		for _, c := range crawlers {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				c.Name, c.Region, dash(c.State), dash(c.LastCrawlStatus), dash(c.Database), dash(c.Schedule))
		}
		return tw.Flush()
	}
}

// --- Triggers --------------------------------------------------------------

func RenderTriggers(w io.Writer, triggers []Trigger, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, triggers)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, t := range triggers {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "Type", "State", "Schedule", "Workflow", "ARN"})
		}
		for _, t := range triggers {
			_ = cw.Write([]string{t.Name, t.Region, t.Type, t.State, t.Schedule, t.Workflow, t.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tTYPE\tSTATE\tSCHEDULE\tWORKFLOW")
		}
		for _, t := range triggers {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				t.Name, t.Region, dash(t.Type), dash(t.State), dash(t.Schedule), dash(t.Workflow))
		}
		return tw.Flush()
	}
}

// --- Workflows -------------------------------------------------------------

func RenderWorkflows(w io.Writer, workflows []Workflow, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, workflows)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, wf := range workflows {
			if err := enc.Encode(wf); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "ARN"})
		}
		for _, wf := range workflows {
			_ = cw.Write([]string{wf.Name, wf.Region, wf.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION")
		}
		for _, wf := range workflows {
			fmt.Fprintf(tw, "%s\t%s\n", wf.Name, wf.Region)
		}
		return tw.Flush()
	}
}

// --- Connections -----------------------------------------------------------

func RenderConnections(w io.Writer, conns []Connection, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, conns)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, c := range conns {
			if err := enc.Encode(c); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "Type", "Status", "ARN"})
		}
		for _, c := range conns {
			_ = cw.Write([]string{c.Name, c.Region, c.Type, c.Status, c.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tTYPE\tSTATUS")
		}
		for _, c := range conns {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Name, c.Region, dash(c.Type), dash(c.Status))
		}
		return tw.Flush()
	}
}

// --- Job runs --------------------------------------------------------------

type runJSON struct {
	ID              string  `json:"id"`
	State           string  `json:"state"`
	Started         string  `json:"started,omitempty"`
	Completed       string  `json:"completed,omitempty"`
	DurationSeconds int32   `json:"durationSeconds"`
	DPUHours        float64 `json:"dpuHours"`
	EstUSD          float64 `json:"estUsd"`
	Worker          string  `json:"worker,omitempty"`
	Attempt         int32   `json:"attempt"`
	Error           string  `json:"error,omitempty"`
}

func runsToJSON(runs []JobRun) []runJSON {
	out := make([]runJSON, 0, len(runs))
	for _, r := range runs {
		out = append(out, runJSON{
			ID: r.ID, State: r.State,
			Started: isoOrEmpty(r.Started), Completed: isoOrEmpty(r.Completed),
			DurationSeconds: r.ExecSecs,
			DPUHours:        costs.GlueRunDPUHours(r.DPUSeconds),
			EstUSD:          costs.GlueRunCostUSD(r.DPUSeconds),
			Worker:          r.Worker, Attempt: r.Attempt, Error: r.Error,
		})
	}
	return out
}

// RenderRuns writes a job's run history in the requested format.
func RenderRuns(w io.Writer, runs []JobRun, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, runsToJSON(runs))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, r := range runsToJSON(runs) {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Started", "State", "DurationSeconds", "DpuHours", "EstUsd", "Worker", "Attempt", "Error"})
		}
		for _, r := range runs {
			_ = cw.Write([]string{
				isoOrEmpty(r.Started), r.State, strconv.Itoa(int(r.ExecSecs)),
				strconv.FormatFloat(costs.GlueRunDPUHours(r.DPUSeconds), 'f', 2, 64),
				strconv.FormatFloat(costs.GlueRunCostUSD(r.DPUSeconds), 'f', 2, 64),
				r.Worker, strconv.Itoa(int(r.Attempt)), r.Error,
			})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "STARTED\tSTATE\tDURATION\tDPU-HRS\tEST\tWORKER\tATTEMPT\tERROR")
		}
		for _, r := range runs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
				tableTime(r.Started), dash(r.State), formatDuration(r.ExecSecs),
				formatDPUHours(r.DPUSeconds), formatCost(r.DPUSeconds), dash(r.Worker),
				r.Attempt, truncate(r.Error, 60))
		}
		return tw.Flush()
	}
}

// FilterRunsByStatus returns only the runs whose state matches status
// (case-insensitive); an empty status returns all runs.
func FilterRunsByStatus(runs []JobRun, status string) []JobRun {
	if status == "" {
		return runs
	}
	want := strings.ToUpper(status)
	var out []JobRun
	for _, r := range runs {
		if strings.ToUpper(r.State) == want {
			out = append(out, r)
		}
	}
	return out
}
