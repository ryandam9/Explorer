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

// --- YARN applications -----------------------------------------------------

type yarnJSON struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	State       string  `json:"state,omitempty"`
	FinalStatus string  `json:"finalStatus,omitempty"`
	Progress    float64 `json:"progress"`
	Queue       string  `json:"queue,omitempty"`
	User        string  `json:"user,omitempty"`
	Type        string  `json:"type,omitempty"`
	ElapsedMS   int64   `json:"elapsedMs"`
}

func yarnToJSON(apps []YarnApp) []yarnJSON {
	out := make([]yarnJSON, 0, len(apps))
	for _, a := range apps {
		out = append(out, yarnJSON{
			ID: a.ID, Name: a.Name, State: a.State, FinalStatus: a.FinalStatus,
			Progress: a.Progress, Queue: a.Queue, User: a.User, Type: a.Type, ElapsedMS: a.ElapsedMS,
		})
	}
	return out
}

// RenderYARNApps writes a cluster's live YARN applications in the requested format.
func RenderYARNApps(w io.Writer, apps []YarnApp, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, yarnToJSON(apps))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, a := range yarnToJSON(apps) {
			if err := enc.Encode(a); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"ID", "State", "FinalStatus", "Progress", "Queue", "User", "Type", "ElapsedMs"})
		}
		for _, a := range apps {
			_ = cw.Write([]string{
				a.ID, a.State, a.FinalStatus, strconv.FormatFloat(a.Progress, 'f', 0, 64),
				a.Queue, a.User, a.Type, strconv.FormatInt(a.ElapsedMS, 10),
			})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "APPLICATION\tSTATE\tFINAL\tPROG\tQUEUE\tUSER\tELAPSED")
		}
		for _, a := range apps {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%.0f%%\t%s\t%s\t%s\n",
				a.ID, dash(a.State), dash(a.FinalStatus), a.Progress, dash(a.Queue), dash(a.User), a.elapsed())
		}
		return tw.Flush()
	}
}

// --- HBase tables ----------------------------------------------------------

type hbaseJSON struct {
	Namespace string   `json:"namespace"`
	Table     string   `json:"table"`
	Qualified string   `json:"qualified"`
	State     string   `json:"state"`
	Regions   int      `json:"regions"`
	Online    int      `json:"online"`
	Families  []string `json:"families"`
}

func hbaseToJSON(tables []HBaseTable) []hbaseJSON {
	out := make([]hbaseJSON, 0, len(tables))
	for _, t := range tables {
		out = append(out, hbaseJSON{
			Namespace: t.Namespace, Table: t.Name, Qualified: t.Qualified, State: t.State,
			Regions: t.Regions, Online: t.Online, Families: t.Families,
		})
	}
	return out
}

// RenderHBaseTables writes a cluster's HBase tables in the requested format.
func RenderHBaseTables(w io.Writer, tables []HBaseTable, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, hbaseToJSON(tables))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, t := range hbaseToJSON(tables) {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Namespace", "Table", "State", "Regions", "Online", "Families"})
		}
		for _, t := range tables {
			_ = cw.Write([]string{
				t.Namespace, t.Name, t.State, strconv.Itoa(t.Regions), strconv.Itoa(t.Online),
				strings.Join(t.Families, " "),
			})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAMESPACE\tTABLE\tSTATE\tREGIONS\tONLINE\tFAMILIES")
		}
		for _, t := range tables {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
				t.Namespace, t.Name, dash(t.State), t.Regions, t.Online, strings.Join(t.Families, ","))
		}
		return tw.Flush()
	}
}

// --- Oozie jobs ------------------------------------------------------------

// RenderOozieWorkflows writes a cluster's Oozie workflow jobs in the requested format.
func RenderOozieWorkflows(w io.Writer, wf []OozieWorkflow, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, wf)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, x := range wf {
			if err := enc.Encode(x); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"ID", "AppName", "Status", "User", "StartTime", "EndTime"})
		}
		for _, x := range wf {
			_ = cw.Write([]string{x.ID, x.AppName, x.Status, x.User, x.StartTime, x.EndTime})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tSTATUS\tUSER\tSTARTED\tID")
		}
		for _, x := range wf {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", x.AppName, dash(x.Status), dash(x.User), dash(x.StartTime), x.ID)
		}
		return tw.Flush()
	}
}

// RenderOozieCoordinators writes a cluster's Oozie coordinator jobs in the requested format.
func RenderOozieCoordinators(w io.Writer, coords []OozieCoordinator, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, coords)
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, x := range coords {
			if err := enc.Encode(x); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"ID", "Name", "Status", "Frequency", "TimeUnit", "NextMaterialized", "User"})
		}
		for _, x := range coords {
			_ = cw.Write([]string{x.ID, x.Name, x.Status, x.Frequency, x.TimeUnit, x.NextMaterialized, x.User})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tSTATUS\tFREQUENCY\tNEXT MATERIALIZED\tID")
		}
		for _, x := range coords {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", x.Name, dash(x.Status), dash(x.frequency()), dash(x.NextMaterialized), x.ID)
		}
		return tw.Flush()
	}
}

// --- Describe --------------------------------------------------------------

// describeJSON is the machine-readable shape of a cluster description. It mirrors
// ClusterDescription but with stable, lower-cased JSON keys for scripting.
type describeJSON struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	Region             string                 `json:"region"`
	ARN                string                 `json:"arn,omitempty"`
	State              string                 `json:"state,omitempty"`
	ReleaseLabel       string                 `json:"releaseLabel,omitempty"`
	OSReleaseLabel     string                 `json:"osReleaseLabel,omitempty"`
	OperatingSystem    string                 `json:"operatingSystem,omitempty"`
	CustomAmiID        string                 `json:"customAmiId,omitempty"`
	Architecture       string                 `json:"architecture,omitempty"`
	EbsRootVolumeGiB   int32                  `json:"ebsRootVolumeGiB,omitempty"`
	InstanceCollection string                 `json:"instanceCollectionType,omitempty"`
	AutoTerminate      bool                   `json:"autoTerminate"`
	TerminationProt    *bool                  `json:"terminationProtected,omitempty"`
	ScaleDownBehavior  string                 `json:"scaleDownBehavior,omitempty"`
	LogURI             string                 `json:"logUri,omitempty"`
	ServiceRole        string                 `json:"serviceRole,omitempty"`
	SecurityConfig     string                 `json:"securityConfiguration,omitempty"`
	InstanceProfile    string                 `json:"iamInstanceProfile,omitempty"`
	Applications       []AppInfo              `json:"applications,omitempty"`
	Groups             []NodeGroup            `json:"instanceGroups,omitempty"`
	Instances          []Instance             `json:"instances,omitempty"`
	Network            NetworkInfo            `json:"network"`
	Configurations     []ConfigClassification `json:"configurations,omitempty"`
	Notes              []string               `json:"notes,omitempty"`
}

func describeToJSON(d ClusterDescription) describeJSON {
	return describeJSON{
		ID: d.Cluster.ID, Name: d.Cluster.Name, Region: d.Cluster.Region, ARN: d.Cluster.ARN,
		State: d.Cluster.State, ReleaseLabel: d.ReleaseLabel, OSReleaseLabel: d.OSReleaseLabel,
		OperatingSystem: osLabel(d), CustomAmiID: d.CustomAmiID, Architecture: architectureLabel(d.Groups),
		EbsRootVolumeGiB: d.EbsRootVolumeGiB, InstanceCollection: d.InstanceCollection,
		AutoTerminate: d.Cluster.AutoTerminate, TerminationProt: d.TerminationProt,
		ScaleDownBehavior: d.ScaleDownBehavior, LogURI: d.Cluster.LogURI, ServiceRole: d.Cluster.ServiceRole,
		SecurityConfig: d.Cluster.SecurityConfig, InstanceProfile: d.Cluster.InstanceProfile,
		Applications: d.Applications, Groups: d.Groups, Instances: d.Instances, Network: d.Network,
		Configurations: d.Configurations, Notes: d.Notes,
	}
}

// RenderDescribe writes a full cluster description. The default (text) format is
// the same sectioned report as the dashboard's d overlay, minus colour; json
// emits the machine-readable shape.
func RenderDescribe(w io.Writer, d ClusterDescription, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, describeToJSON(d))
	case "ndjson":
		return json.NewEncoder(w).Encode(describeToJSON(d))
	default:
		var b strings.Builder
		for i, s := range d.sections() {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(s.Title + "\n")
			b.WriteString(s.Body)
		}
		b.WriteString("\n")
		_, err := io.WriteString(w, b.String())
		return err
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
