package output

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// Formats lists the accepted values for the --output flag.
var Formats = []string{"table", "json", "ndjson", "csv"}

// FormatList returns the accepted output formats as a comma-separated string
// for flag help text.
func FormatList() string { return strings.Join(Formats, ", ") }

// ValidateFormat rejects unknown --output values instead of silently falling
// back to a table.
func ValidateFormat(format string) error {
	f := strings.ToLower(format)
	for _, v := range Formats {
		if f == v {
			return nil
		}
	}
	return fmt.Errorf("invalid output format %q (valid formats: %s)", format, FormatList())
}

// Options tunes how streamed results are rendered.
type Options struct {
	// NoHeader omits the header row in table and csv output, for scripting.
	NoHeader bool
	// TotalTasks is the number of planned collection tasks (service × region
	// pairs); when stderr is a terminal it drives the live progress meter.
	TotalTasks int
}

// stderrRenderer renders ANSI for the error summary and progress meter on
// stderr; lipgloss's default renderer probes stdout, which may be piped
// while stderr is still a terminal.
var stderrRenderer = lipgloss.NewRenderer(os.Stderr)

// StreamOutput reads chunks from the channel and prints results incrementally.
// Collection errors are gathered and summarized on stderr after the run, so
// they never interleave with results.
func StreamOutput(chunks <-chan model.ResultChunk, format string, opts Options) {
	switch strings.ToLower(format) {
	case "json":
		streamJSON(os.Stdout, chunks, opts)
	case "ndjson":
		streamNDJSON(os.Stdout, chunks, opts)
	case "csv":
		streamCSV(os.Stdout, chunks, opts)
	default:
		streamTable(os.Stdout, chunks, opts)
	}
}

// ── Progress meter ───────────────────────────────────────────────────────────

// meter shows live scan progress on stderr while results stream to stdout.
// It is inert when stderr is not a terminal.
type meter struct {
	out       io.Writer
	enabled   bool
	total     int
	done      int
	resources int
}

func newMeter(total int) *meter {
	m := &meter{out: os.Stderr, total: total}
	m.enabled = total > 0 && isatty.IsTerminal(os.Stderr.Fd())
	m.render()
	return m
}

func (m *meter) render() {
	if !m.enabled {
		return
	}
	line := stderrRenderer.NewStyle().Faint(true).
		Render(fmt.Sprintf("⠿ scanning %d/%d tasks · %d resources", m.done, m.total, m.resources))
	fmt.Fprintf(m.out, "\r\x1b[2K%s", line)
}

// observe accounts for one streamed chunk and refreshes the meter.
func (m *meter) observe(chunk model.ResultChunk) {
	if chunk.Progress != nil {
		m.done++
	}
	m.resources += len(chunk.Resources)
	m.render()
}

// pause clears the progress line so rows can be written to the terminal
// without colliding with it.
func (m *meter) pause() {
	if m.enabled {
		fmt.Fprint(m.out, "\r\x1b[2K")
	}
}

// finish removes the progress line for good.
func (m *meter) finish() { m.pause() }

// ── Streaming renderers ──────────────────────────────────────────────────────

// streamRowFmt fixes each column's width up front so alignment is identical
// across every streamed chunk. A tabwriter cannot provide this: it computes
// widths per flush, and flushing per chunk (required for streaming) makes
// columns drift between chunks. Values longer than their floor extend only
// their own row.
const streamRowFmt = "%-14s  %-14s  %-15s  %-44s  %-30s  %s\n"

// stateStyle colors well-known resource states with the terminal's base
// palette: green for healthy, yellow for transitional, red for stopped or
// failed. Rendering degrades to plain text automatically when stdout is not
// a terminal or NO_COLOR is set.
func stateStyle(state string) string {
	if state == "" {
		return state
	}
	var color string
	switch strings.ToLower(state) {
	case "running", "active", "available", "in-use", "healthy", "enabled", "issued", "deployed":
		color = "2" // green
	case "pending", "stopping", "creating", "updating", "modifying", "rebooting", "provisioning", "starting":
		color = "3" // yellow
	case "stopped", "terminated", "failed", "error", "deleting", "deleted", "unhealthy", "disabled", "shutting-down":
		color = "1" // red
	default:
		return state
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(state)
}

func streamTable(w io.Writer, chunks <-chan model.ResultChunk, opts Options) {
	m := newMeter(opts.TotalTasks)
	if !opts.NoHeader {
		// The whole line is styled after formatting so the fixed column
		// widths are computed on plain text.
		header := strings.TrimSuffix(
			fmt.Sprintf(streamRowFmt, "SERVICE", "TYPE", "REGION", "ID", "NAME", "STATE"), "\n")
		m.pause()
		fmt.Fprintln(w, lipgloss.NewStyle().Bold(true).Render(header))
	}

	var errs []model.ExploreError
	anyOutput := false
	for chunk := range chunks {
		errs = append(errs, chunk.Errors...)
		if len(chunk.Resources) > 0 {
			m.pause()
		}
		for _, r := range chunk.Resources {
			// State is the final column, so styling it cannot skew the
			// fixed-width padding of the columns before it.
			fmt.Fprintf(w, streamRowFmt,
				r.Service, r.Type, r.Region, r.ID, r.Name, stateStyle(r.State))
			anyOutput = true
		}
		m.observe(chunk)
	}
	m.finish()

	if !anyOutput {
		fmt.Fprintln(w, "No resources found.")
	}
	PrintErrors(os.Stderr, errs)
}

func streamCSV(w io.Writer, chunks <-chan model.ResultChunk, opts Options) {
	m := newMeter(opts.TotalTasks)
	cw := csv.NewWriter(w)
	if !opts.NoHeader {
		_ = cw.Write([]string{"service", "type", "region", "id", "name", "state"})
	}

	var errs []model.ExploreError
	for chunk := range chunks {
		errs = append(errs, chunk.Errors...)
		for _, r := range chunk.Resources {
			_ = cw.Write([]string{r.Service, r.Type, r.Region, r.ID, r.Name, r.State})
		}
		// Flush per chunk so consumers see rows as they are collected.
		cw.Flush()
		m.observe(chunk)
	}
	m.finish()

	if err := cw.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write CSV: %v\n", err)
	}
	PrintErrors(os.Stderr, errs)
}

func streamJSON(out io.Writer, chunks <-chan model.ResultChunk, opts Options) {
	m := newMeter(opts.TotalTasks)
	bw := bufio.NewWriter(out)
	defer bw.Flush()

	var errs []model.ExploreError
	first := true
	bw.WriteString("[")
	for chunk := range chunks {
		errs = append(errs, chunk.Errors...)
		for _, r := range chunk.Resources {
			if !first {
				bw.WriteByte(',')
			}
			first = false
			data, err := json.Marshal(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to marshal resource: %v\n", err)
				continue
			}
			bw.Write(data)
		}
		bw.Flush()
		m.observe(chunk)
	}
	bw.WriteString("]\n")
	bw.Flush()
	m.finish()
	PrintErrors(os.Stderr, errs)
}

// streamNDJSON writes one JSON object per line as resources arrive — the
// friendliest format for piping into jq or log processors.
func streamNDJSON(out io.Writer, chunks <-chan model.ResultChunk, opts Options) {
	m := newMeter(opts.TotalTasks)
	bw := bufio.NewWriter(out)
	defer bw.Flush()

	var errs []model.ExploreError
	for chunk := range chunks {
		errs = append(errs, chunk.Errors...)
		for _, r := range chunk.Resources {
			data, err := json.Marshal(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to marshal resource: %v\n", err)
				continue
			}
			bw.Write(data)
			bw.WriteByte('\n')
		}
		bw.Flush()
		m.observe(chunk)
	}
	m.finish()
	PrintErrors(os.Stderr, errs)
}

// ── Error summary ────────────────────────────────────────────────────────────

// errGroup is one deduplicated error: the same service+code+message seen in
// one or more regions.
type errGroup struct {
	service, code, message string
	partial                bool
	regions                []string
}

// groupErrors merges identical errors that differ only by region, so a scan
// of 17 regions reports one access-denied line per service instead of 17.
func groupErrors(errs []model.ExploreError) []errGroup {
	idx := map[string]int{}
	var groups []errGroup
	for _, e := range errs {
		key := e.Service + "\x00" + e.Code + "\x00" + e.Message
		if i, ok := idx[key]; ok {
			groups[i].regions = append(groups[i].regions, e.Region)
			groups[i].partial = groups[i].partial || e.Partial
			continue
		}
		idx[key] = len(groups)
		groups = append(groups, errGroup{
			service: e.Service, code: e.Code, message: e.Message,
			partial: e.Partial, regions: []string{e.Region},
		})
	}
	for i := range groups {
		sort.Strings(groups[i].regions)
	}
	return groups
}

// regionList renders a group's regions compactly: up to three names, then a
// "+N more" suffix.
func regionList(regions []string) string {
	if len(regions) <= 3 {
		return strings.Join(regions, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(regions[:3], ", "), len(regions)-3)
}

// PrintErrors summarizes collection errors on w, separating access-denied
// errors from everything else and merging duplicates across regions.
// Styling degrades to plain text when stderr is not a terminal.
func PrintErrors(w io.Writer, errs []model.ExploreError) {
	if len(errs) == 0 {
		return
	}

	headingErr := stderrRenderer.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	headingWarn := stderrRenderer.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	dim := stderrRenderer.NewStyle().Faint(true)

	var authErrs, otherErrs []model.ExploreError
	for _, e := range errs {
		if e.Code == "AccessDenied" {
			authErrs = append(authErrs, e)
		} else {
			otherErrs = append(otherErrs, e)
		}
	}

	if len(authErrs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, headingErr.Render("✗ Insufficient privileges")+
			dim.Render(" — some services could not be scanned:"))
		for _, g := range groupErrors(authErrs) {
			suffix := ""
			if g.partial {
				suffix = dim.Render(" (resources collected before the failure were kept)")
			}
			fmt.Fprintf(w, "  • %s  %s\n    %s%s\n",
				g.service, dim.Render(regionList(g.regions)), g.message, suffix)
		}
	}

	if len(otherErrs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, headingWarn.Render("⚠ Collection errors")+
			dim.Render(" — results may be incomplete:"))
		for _, g := range groupErrors(otherErrs) {
			suffix := ""
			if g.partial {
				suffix = dim.Render(" (partial results kept)")
			}
			fmt.Fprintf(w, "  • %s  %s\n    %s: %s%s\n",
				g.service, dim.Render(regionList(g.regions)),
				g.code, g.message, suffix)
		}
	}
	fmt.Fprintln(w)
}
