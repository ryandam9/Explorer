package emrtui

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

// These Render* functions back the CLI twins of the dashboard. They render the
// same typed data as plain table / JSON / NDJSON / CSV — no glyphs or colour, so
// output pipes cleanly.

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

// --- Clusters --------------------------------------------------------------

type clusterJSON struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Region        string `json:"region"`
	ARN           string `json:"arn,omitempty"`
	State         string `json:"state,omitempty"`
	ReleaseLabel  string `json:"releaseLabel,omitempty"`
	Applications  string `json:"applications,omitempty"`
	InstanceHours int32  `json:"normalizedInstanceHours"`
	AutoTerminate bool   `json:"autoTerminate"`
	Created       string `json:"created,omitempty"`
	StateReason   string `json:"stateChangeReason,omitempty"`
}

func clustersToJSON(clusters []Cluster) []clusterJSON {
	out := make([]clusterJSON, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, clusterJSON{
			ID: c.ID, Name: c.Name, Region: c.Region, ARN: c.ARN, State: c.State,
			ReleaseLabel: c.ReleaseLabel, Applications: c.Applications,
			InstanceHours: c.InstanceHours, AutoTerminate: c.AutoTerminate,
			Created: isoOrEmpty(c.Created), StateReason: c.StateReason,
		})
	}
	return out
}

// RenderClusters writes clusters in the requested format.
func RenderClusters(w io.Writer, clusters []Cluster, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, clustersToJSON(clusters))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, c := range clustersToJSON(clusters) {
			if err := enc.Encode(c); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"ID", "Name", "Region", "State", "ReleaseLabel", "Applications", "NormalizedInstanceHours", "AutoTerminate", "Created", "ARN"})
		}
		for _, c := range clusters {
			_ = cw.Write([]string{
				c.ID, c.Name, c.Region, c.State, c.ReleaseLabel, c.Applications,
				strconv.Itoa(int(c.InstanceHours)), strconv.FormatBool(c.AutoTerminate),
				isoOrEmpty(c.Created), c.ARN,
			})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tID\tREGION\tSTATE\tRELEASE\tAPPLICATIONS\tHRS")
		}
		for _, c := range clusters {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
				c.Name, c.ID, c.Region, dash(c.State), dash(c.ReleaseLabel),
				dash(c.Applications), c.InstanceHours)
		}
		return tw.Flush()
	}
}

// --- Steps -----------------------------------------------------------------

type stepJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	State           string `json:"state,omitempty"`
	ActionOnFailure string `json:"actionOnFailure,omitempty"`
	Created         string `json:"created,omitempty"`
	Started         string `json:"started,omitempty"`
	Ended           string `json:"ended,omitempty"`
	DurationSeconds int64  `json:"durationSeconds"`
	FailureReason   string `json:"failureReason,omitempty"`
	FailureLog      string `json:"failureLog,omitempty"`
}

func stepDurationSeconds(s Step) int64 {
	if s.Started.IsZero() || s.Ended.IsZero() || !s.Ended.After(s.Started) {
		return 0
	}
	return int64(s.Ended.Sub(s.Started).Seconds())
}

func stepsToJSON(steps []Step) []stepJSON {
	out := make([]stepJSON, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepJSON{
			ID: s.ID, Name: s.Name, State: s.State, ActionOnFailure: s.ActionOnFailure,
			Created: isoOrEmpty(s.Created), Started: isoOrEmpty(s.Started), Ended: isoOrEmpty(s.Ended),
			DurationSeconds: stepDurationSeconds(s),
			FailureReason:   s.FailureReason, FailureLog: s.FailureLog,
		})
	}
	return out
}

// RenderSteps writes a cluster's step history in the requested format.
func RenderSteps(w io.Writer, steps []Step, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, stepsToJSON(steps))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, s := range stepsToJSON(steps) {
			if err := enc.Encode(s); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Started", "State", "DurationSeconds", "ActionOnFailure", "Name", "FailureReason", "FailureLog"})
		}
		for _, s := range steps {
			_ = cw.Write([]string{
				isoOrEmpty(s.Started), s.State, strconv.FormatInt(stepDurationSeconds(s), 10),
				s.ActionOnFailure, s.Name, s.FailureReason, s.FailureLog,
			})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "STARTED\tSTATE\tDURATION\tACTION-ON-FAIL\tNAME\tREASON")
		}
		for _, s := range steps {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				tableTime(s.Created), dash(s.State), formatDuration(s.Started, s.Ended),
				dash(s.ActionOnFailure), s.Name, truncate(s.FailureReason, 60))
		}
		return tw.Flush()
	}
}

// --- Instances -------------------------------------------------------------

// RenderInstances writes a cluster's instances in the requested format.
func RenderInstances(w io.Writer, instances []Instance, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, instances)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, in := range instances {
			if err := enc.Encode(in); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"ID", "EC2ID", "Type", "Market", "State", "PrivateDNS", "PublicDNS", "Group"})
		}
		for _, in := range instances {
			_ = cw.Write([]string{in.ID, in.EC2ID, in.Type, in.Market, in.State, in.PrivateDNS, in.PublicDNS, in.Group})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "EC2-ID\tTYPE\tMARKET\tSTATE\tPRIVATE-DNS")
		}
		for _, in := range instances {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				dash(in.EC2ID), dash(in.Type), dash(in.Market), dash(in.State), dash(in.PrivateDNS))
		}
		return tw.Flush()
	}
}

// --- Applications ----------------------------------------------------------

// RenderApps writes a cluster's installed applications in the requested format.
func RenderApps(w io.Writer, apps []AppInfo, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, apps)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, a := range apps {
			if err := enc.Encode(a); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Version"})
		}
		for _, a := range apps {
			_ = cw.Write([]string{a.Name, a.Version})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "APPLICATION\tVERSION")
		}
		for _, a := range apps {
			fmt.Fprintf(tw, "%s\t%s\n", a.Name, dash(a.Version))
		}
		return tw.Flush()
	}
}

// FilterStepsByStatus returns only the steps whose state matches status
// (case-insensitive); an empty status returns all steps.
func FilterStepsByStatus(steps []Step, status string) []Step {
	if status == "" {
		return steps
	}
	want := strings.ToUpper(status)
	var out []Step
	for _, s := range steps {
		if strings.ToUpper(s.State) == want {
			out = append(out, s)
		}
	}
	return out
}

// FilterClustersByState returns only the clusters whose state matches one of the
// comma-separated states (case-insensitive); an empty filter returns all.
func FilterClustersByState(clusters []Cluster, states string) []Cluster {
	states = strings.TrimSpace(states)
	if states == "" {
		return clusters
	}
	want := map[string]bool{}
	for _, s := range strings.Split(states, ",") {
		if s = strings.TrimSpace(s); s != "" {
			want[strings.ToUpper(s)] = true
		}
	}
	var out []Cluster
	for _, c := range clusters {
		if want[strings.ToUpper(c.State)] {
			out = append(out, c)
		}
	}
	return out
}
