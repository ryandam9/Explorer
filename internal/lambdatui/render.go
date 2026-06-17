package lambdatui

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

// --- Functions -------------------------------------------------------------

type functionJSON struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	ARN          string `json:"arn"`
	Runtime      string `json:"runtime,omitempty"`
	PackageType  string `json:"packageType,omitempty"`
	MemoryMB     int32  `json:"memoryMB,omitempty"`
	TimeoutSec   int32  `json:"timeoutSeconds,omitempty"`
	Handler      string `json:"handler,omitempty"`
	State        string `json:"state,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	HasDLQ       bool   `json:"hasDeadLetterQueue"`
}

func functionsToJSON(fns []Function) []functionJSON {
	out := make([]functionJSON, 0, len(fns))
	for _, f := range fns {
		out = append(out, functionJSON{
			Name: f.Name, Region: f.Region, ARN: f.ARN,
			Runtime: f.Runtime, PackageType: f.PackageType,
			MemoryMB: f.MemoryMB, TimeoutSec: f.TimeoutSec, Handler: f.Handler,
			State: f.State, LastModified: isoOrEmpty(f.LastModified),
			HasDLQ: f.DLQTargetArn != "",
		})
	}
	return out
}

// RenderFunctions writes functions in the requested format.
func RenderFunctions(w io.Writer, fns []Function, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, functionsToJSON(fns))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, f := range functionsToJSON(fns) {
			if err := enc.Encode(f); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "Runtime", "PackageType", "MemoryMB", "TimeoutSeconds", "State", "LastModified", "ARN"})
		}
		for _, f := range fns {
			_ = cw.Write([]string{f.Name, f.Region, f.Runtime, f.PackageType, strconv.Itoa(int(f.MemoryMB)), strconv.Itoa(int(f.TimeoutSec)), f.State, isoOrEmpty(f.LastModified), f.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tRUNTIME\tMEMORY\tTIMEOUT\tSTATE\tLAST MODIFIED")
		}
		for _, f := range fns {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				f.Name, f.Region, dash(runtimeLabel(f.Runtime, f.PackageType)),
				dash(formatMemory(f.MemoryMB)), dash(formatTimeout(f.TimeoutSec)),
				dash(f.State), tableTime(f.LastModified))
		}
		return tw.Flush()
	}
}

// --- Layers ----------------------------------------------------------------

type layerJSON struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	ARN           string   `json:"arn"`
	LatestVersion int64    `json:"latestVersion"`
	Runtimes      []string `json:"compatibleRuntimes,omitempty"`
	Architectures []string `json:"compatibleArchitectures,omitempty"`
	Description   string   `json:"description,omitempty"`
}

func layersToJSON(layers []Layer) []layerJSON {
	out := make([]layerJSON, 0, len(layers))
	for _, l := range layers {
		out = append(out, layerJSON{
			Name: l.Name, Region: l.Region, ARN: l.ARN,
			LatestVersion: l.LatestVersion, Runtimes: l.Runtimes,
			Architectures: l.Architectures, Description: l.Description,
		})
	}
	return out
}

// RenderLayers writes layers in the requested format.
func RenderLayers(w io.Writer, layers []Layer, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, layersToJSON(layers))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, l := range layersToJSON(layers) {
			if err := enc.Encode(l); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Name", "Region", "LatestVersion", "CompatibleRuntimes", "CompatibleArchitectures", "ARN"})
		}
		for _, l := range layers {
			_ = cw.Write([]string{l.Name, l.Region, strconv.FormatInt(l.LatestVersion, 10), strings.Join(l.Runtimes, " "), strings.Join(l.Architectures, " "), l.ARN})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "NAME\tREGION\tVERSION\tRUNTIMES")
		}
		for _, l := range layers {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", l.Name, l.Region, l.LatestVersion, dash(strings.Join(l.Runtimes, ", ")))
		}
		return tw.Flush()
	}
}

// --- Event source mappings -------------------------------------------------

type eventSourceJSON struct {
	UUID                 string `json:"uuid"`
	Region               string `json:"region"`
	ARN                  string `json:"arn,omitempty"`
	Function             string `json:"function"`
	Source               string `json:"source"`
	State                string `json:"state,omitempty"`
	BatchSize            int32  `json:"batchSize,omitempty"`
	LastModified         string `json:"lastModified,omitempty"`
	LastProcessingResult string `json:"lastProcessingResult,omitempty"`
}

func eventSourcesToJSON(es []EventSource) []eventSourceJSON {
	out := make([]eventSourceJSON, 0, len(es))
	for _, m := range es {
		out = append(out, eventSourceJSON{
			UUID: m.UUID, Region: m.Region, ARN: m.ARN,
			Function: m.FunctionName, Source: m.SourceLabel, State: m.State,
			BatchSize: m.BatchSize, LastModified: isoOrEmpty(m.LastModified),
			LastProcessingResult: m.LastProcessingResult,
		})
	}
	return out
}

// RenderEventSources writes event-source mappings in the requested format.
func RenderEventSources(w io.Writer, es []EventSource, format string, noHeader bool) error {
	switch strings.ToLower(format) {
	case "json":
		return writeJSON(w, eventSourcesToJSON(es))
	case "ndjson":
		enc := json.NewEncoder(w)
		for _, m := range eventSourcesToJSON(es) {
			if err := enc.Encode(m); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		cw := csv.NewWriter(w)
		if !noHeader {
			_ = cw.Write([]string{"Function", "Source", "State", "BatchSize", "LastModified", "LastProcessingResult", "UUID", "Region"})
		}
		for _, m := range es {
			_ = cw.Write([]string{m.FunctionName, m.SourceLabel, m.State, strconv.Itoa(int(m.BatchSize)), isoOrEmpty(m.LastModified), m.LastProcessingResult, m.UUID, m.Region})
		}
		cw.Flush()
		return cw.Error()
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !noHeader {
			fmt.Fprintln(tw, "FUNCTION\tREGION\tSOURCE\tSTATE\tBATCH\tLAST RESULT")
		}
		for _, m := range es {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
				dash(m.FunctionName), m.Region, dash(m.SourceLabel), dash(m.State), m.BatchSize, dash(m.LastProcessingResult))
		}
		return tw.Flush()
	}
}
