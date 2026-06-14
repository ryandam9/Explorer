// Package trailtui is the interactive viewer for the CloudTrail activity feed
// (`aws_explorer trail --tui`). Events are streamed in per region for the chosen
// scope (a resource, a principal/event/source pivot, or the whole account) and
// shown in a table that supports quick filtering, sorting, horizontal column
// scrolling, a "failed calls only" toggle, and a per-event detail overlay — the
// same interaction language as the other TUIs in the application.
//
// Read-only (Describe*/List*/Get*) and other noisy events are filtered out
// server-side via the config's trail.hideEvents, so the feed's cap counts only
// the events that are actually shown.
package trailtui

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/debugpane"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/trail"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// regionMsg delivers one region's slice of the CloudTrail lookup as soon as it
// completes, so the feed fills in progressively instead of blocking on the
// slowest region.
type regionMsg struct {
	region    string
	events    []trail.Event
	truncated bool
	err       error
}

// streamDoneMsg signals that every region has reported (the results channel is
// closed) — the spinner can stop and any all-regions-failed error can surface.
type streamDoneMsg struct{}

// regionConcurrency bounds how many regions are queried at once; each region's
// CloudTrail has its own 2 TPS budget so they run in parallel.
const regionConcurrency = 8

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayDetail
	overlayHelp
)

// columns of the events table. The sequence number and time stay pinned while
// the rest scroll horizontally on narrow terminals. Column 0 ("#") is a
// positional row counter, not a sortable field.
var columns = []table.Column{
	{Title: "#", Width: 5},
	{Title: "TIME", Width: 20},
	{Title: "EVENT", Width: 28},
	{Title: "SERVICE", Width: 16},
	{Title: "REGION", Width: 12},
	{Title: "PRINCIPAL", Width: 24},
	{Title: "SOURCE IP", Width: 16},
	{Title: "OUTCOME", Width: 18},
}

// Model is the CloudTrail activity-feed TUI.
type Model struct {
	// Lookup inputs, captured at construction and used by the load command.
	ctx     context.Context
	cfg     aws.Config
	regions []string
	filter  trail.Filter
	opts    trail.Options // fetch options (errors-only is applied client-side)
	scope   string        // human-readable scope for the header

	loading   bool
	loadErr   error
	truncated bool
	capped    bool // the feed holds more matching events than limit shows

	// Streaming state. regionCh carries each region's result; the goroutines
	// that fill it are launched once from Init. regionsDone/regionsTotal drive
	// the "scanning N/M regions" progress, and regionErrs collects per-region
	// failures so a partial outage is visible without aborting the others.
	regionCh     chan regionMsg
	regionsTotal int
	regionsDone  int
	regionErrs   []error

	// limit caps how many matching (post-hide) events the feed shows; sourced
	// from --limit / config trail.maxEvents.
	limit int

	all     []trail.Event // every streamed-in event, newest first
	visible []trail.Event // filter + errors-only + sort + limit applied; row i = visible[i]

	tbl        table.Model
	filtering  bool
	filterIn   textinput.Model
	errorsOnly bool // "failed calls only" toggle (client-side)

	// sortCol -1 is the natural newest-first order from the API.
	sortCol int
	sortAsc bool

	overlay overlayKind

	spin   spinner.Model
	status string // transient status-bar message (copy/export results)

	debug debugpane.Model // "~" live activity overlay

	width, height int
}

// New builds the CloudTrail feed TUI. The per-region lookups run from Init and
// stream their results in as they finish. opts carries the server-side filters
// (opts.HideEvents drops noisy events, opts.IncludeReadOnly governs read-only
// events) and opts.Limit, which is the feed's post-filter cap. opts.ErrorsOnly
// is honored as the initial state of the in-TUI "failed only" toggle but the
// fetch itself pulls everything so the toggle can flip without another
// (rate-limited) API round trip.
func New(ctx context.Context, cfg aws.Config, regions []string, filter trail.Filter, opts trail.Options, scope string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	fi := textinput.New()
	fi.Placeholder = "Filter events…"
	fi.CharLimit = 128
	fi.Width = 32
	fi.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	fi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	fi.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	fi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	tbl := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(2),
	)

	errorsOnly := opts.ErrorsOnly
	opts.ErrorsOnly = false // fetch everything; the toggle filters client-side

	limit := opts.Limit
	if limit <= 0 {
		limit = trail.DefaultLimit
	}

	return Model{
		ctx:          ctx,
		cfg:          cfg,
		regions:      regions,
		filter:       filter,
		opts:         opts,
		scope:        scope,
		loading:      true,
		errorsOnly:   errorsOnly,
		limit:        limit,
		regionCh:     make(chan regionMsg, max(len(regions), 1)),
		regionsTotal: len(regions),
		tbl:          tbl,
		filterIn:     fi,
		sortCol:      -1,
		spin:         sp,
	}
}

// PageTitle implements ui.Titled.
func (m Model) PageTitle() string { return "CloudTrail" }

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spawnRegions(), m.waitRegion(), m.spin.Tick)
}

// spawnRegions launches one lookup goroutine per region (bounded by
// regionConcurrency), each pushing its result onto regionCh the moment it
// finishes, then closes the channel once all are done. It returns no message of
// its own — waitRegion drains the channel.
func (m Model) spawnRegions() tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		sem := make(chan struct{}, regionConcurrency)
		for _, region := range m.regions {
			wg.Add(1)
			go func(region string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				evs, trunc, err := trail.LookupFiltered(m.ctx, m.cfg, region, m.filter, m.opts)
				m.regionCh <- regionMsg{region: region, events: evs, truncated: trunc, err: err}
			}(region)
		}
		wg.Wait()
		close(m.regionCh)
		return nil
	}
}

// waitRegion blocks for the next region's result and delivers it; a closed
// channel becomes streamDoneMsg. It is re-issued after each regionMsg so the
// feed keeps draining until every region has reported.
func (m Model) waitRegion() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.regionCh
		if !ok {
			return streamDoneMsg{}
		}
		return msg
	}
}

// regionLabel describes the scanned regions for the header.
func regionLabel(regions []string) string {
	switch len(regions) {
	case 0:
		return ""
	case 1:
		return regions[0]
	default:
		return strconv.Itoa(len(regions)) + " regions"
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While the debug overlay is open it consumes key/mouse input; other
	// messages fall through so the load/spinner keep progressing underneath.
	if m.debug.Visible() {
		if m.debug.HandleInput(msg) {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutTable()
		return m, nil

	case spinner.TickMsg:
		m.debug.Refresh()
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case regionMsg:
		m.regionsDone++
		if msg.err != nil {
			m.regionErrs = append(m.regionErrs, msg.err)
		} else {
			m.all = append(m.all, msg.events...)
			sort.SliceStable(m.all, func(i, j int) bool { return m.all[i].Time.After(m.all[j].Time) })
			m.truncated = m.truncated || msg.truncated
		}
		m.rebuild()
		return m, m.waitRegion() // drain the next region

	case streamDoneMsg:
		m.loading = false
		// Surface an error only when every region failed; a partial outage is
		// shown as a note in the header instead (see headerView).
		if len(m.all) == 0 && len(m.regionErrs) > 0 && len(m.regionErrs) == m.regionsTotal {
			m.loadErr = m.regionErrs[0]
		}
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""

	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filterIn.Blur()
			m.filterIn.SetValue("")
			m.rebuild()
		case "enter":
			m.filtering = false
			m.filterIn.Blur()
		default:
			var cmd tea.Cmd
			m.filterIn, cmd = m.filterIn.Update(msg)
			m.rebuild()
			return m, cmd
		}
		return m, nil
	}

	if m.overlay != overlayNone {
		switch msg.String() {
		case "esc", "q", "enter", "?":
			m.overlay = overlayNone
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.filtering = true
		m.filterIn.Focus()
		return m, textinput.Blink
	case "enter":
		if m.selected() != nil {
			m.overlay = overlayDetail
		}
	case "x":
		// Toggle "failed calls only" — the security-triage view.
		m.errorsOnly = !m.errorsOnly
		m.rebuild()
	case "s":
		// Cycle: natural order (-1) → each data column → back. The "#" column
		// (index 0) is positional, so it is skipped.
		m.sortCol++
		if m.sortCol >= len(columns) {
			m.sortCol = -1
		} else if m.sortCol == 0 {
			m.sortCol = 1
		}
		m.sortAsc = true
		m.rebuild()
	case "R":
		if m.sortCol > 0 {
			m.sortAsc = !m.sortAsc
			m.rebuild()
		}
	case "r":
		m.filterIn.SetValue("")
		m.sortCol = -1
		m.errorsOnly = false
		m.rebuild()
	case "<", ",":
		m.tbl.ScrollLeft()
	case ">", ".":
		m.tbl.ScrollRight()
	case "y":
		if ev := m.selected(); ev != nil {
			m.copyToClipboard(ev.EventName)
		}
	case "C":
		m.exportCSV()
	case "?":
		m.overlay = overlayHelp
	case ui.KeyDebug:
		m.debug.Open(m.width, m.height)
	default:
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		return m, cmd
	}
	return m, nil
}

// selected returns the event under the cursor, or nil.
func (m *Model) selected() *trail.Event {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.visible) {
		return nil
	}
	return &m.visible[i]
}

func (m *Model) copyToClipboard(s string) {
	if s == "" {
		return
	}
	if err := clipboard.WriteAll(s); err != nil {
		m.status = "copy failed: clipboard unavailable"
		return
	}
	m.status = "copied " + s
}

func (m *Model) exportCSV() {
	if len(m.visible) == 0 {
		m.status = "nothing to export"
		return
	}
	dir, err := csvexport.DefaultDir()
	if err == nil {
		var rows [][]string
		for _, ev := range m.visible {
			rows = append(rows, []string{
				ev.Time.UTC().Format("2006-01-02T15:04:05Z"),
				ev.Region, ev.EventName, ev.EventSource, ev.Principal,
				ev.AccessKeyID, ev.SourceIP, ev.UserAgent,
				strconv.FormatBool(ev.FromConsole), strconv.FormatBool(ev.MFA),
				strconv.FormatBool(ev.ReadOnly), ev.ErrorCode, ev.ErrorMessage,
				ev.EventID, resourcesText(ev.Resources),
			})
		}
		var path string
		path, err = csvexport.Write(dir, "cloudtrail",
			[]string{
				"Time", "Region", "Event", "Service", "Principal", "AccessKeyID",
				"SourceIP", "UserAgent", "FromConsole", "MFA", "ReadOnly",
				"ErrorCode", "ErrorMessage", "EventID", "Resources",
			}, rows)
		if err == nil {
			m.status = "exported " + path
			return
		}
	}
	m.status = "export failed: " + err.Error()
}

// rebuild recomputes the visible events (filter + errors-only, capped at limit,
// then sorted) and the table rows. The cap is applied to the newest-first feed
// before any column sort so the window is always the most recent `limit` events.
func (m *Model) rebuild() {
	m.visible = filterEvents(m.all, m.filterIn.Value(), m.errorsOnly)
	m.capped = m.limit > 0 && len(m.visible) > m.limit
	if m.capped {
		m.visible = m.visible[:m.limit]
	}
	sortEvents(m.visible, m.sortCol, m.sortAsc)

	cols := append([]table.Column(nil), columns...)
	table.ApplySortHeader(cols, m.sortCol, m.sortAsc, func(i int) bool { return i > 0 })
	m.tbl.SetColumns(cols)

	rows := make([]table.Row, 0, len(m.visible))
	for i, ev := range m.visible {
		rows = append(rows, table.Row{
			strconv.Itoa(i + 1),
			ev.Time.UTC().Format("2006-01-02 15:04:05"),
			eventLabel(ev), serviceLabel(ev), orDash(ev.Region),
			ev.Principal, ev.SourceIP, outcomeLabel(ev),
		})
	}
	m.tbl.SetRows(rows)
	if c := m.tbl.Cursor(); c >= len(rows) && len(rows) > 0 {
		m.tbl.SetCursor(len(rows) - 1)
	}
}

// countFailed counts events that carry an errorCode (failed/denied calls).
func countFailed(evs []trail.Event) int {
	n := 0
	for _, ev := range evs {
		if ev.ErrorCode != "" {
			n++
		}
	}
	return n
}

func eventLabel(ev trail.Event) string {
	if ev.ReadOnly {
		return ev.EventName + " (read)"
	}
	return ev.EventName
}

// serviceLabel shortens the event source to the bare service name
// ("ec2.amazonaws.com" → "ec2"), falling back to a dash when unknown.
func serviceLabel(ev trail.Event) string {
	s := strings.TrimSuffix(ev.EventSource, ".amazonaws.com")
	return orDash(s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// resourcesText renders the affected resources as "type name" entries joined
// by "; " for the detail overlay and CSV export.
func resourcesText(rs []trail.Resource) string {
	if len(rs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		switch {
		case r.Type != "" && r.Name != "":
			parts = append(parts, r.Type+" "+r.Name)
		case r.Name != "":
			parts = append(parts, r.Name)
		case r.Type != "":
			parts = append(parts, r.Type)
		}
	}
	return strings.Join(parts, "; ")
}

// outcomeLabel marks failures with a ✗ and the errorCode so they stand out in
// the table without relying on color (which the column scroller can't measure).
func outcomeLabel(ev trail.Event) string {
	if ev.ErrorCode != "" {
		return "✗ " + ev.ErrorCode
	}
	return "ok"
}

// filterEvents keeps events matching the query (case-insensitive substring over
// the displayed fields) and, when errorsOnly is set, only failed/denied calls.
// Read-only and other noisy events are already excluded server-side (config
// trail.hideEvents), so there is nothing to hide here.
func filterEvents(evs []trail.Event, query string, errorsOnly bool) []trail.Event {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]trail.Event, 0, len(evs))
	for _, ev := range evs {
		if errorsOnly && ev.ErrorCode == "" {
			continue
		}
		if q == "" || matchesEvent(ev, q) {
			out = append(out, ev)
		}
	}
	return out
}

func matchesEvent(ev trail.Event, q string) bool {
	hay := strings.ToLower(strings.Join([]string{
		ev.EventName, ev.EventSource, ev.Region, ev.Principal, ev.SourceIP,
		ev.UserAgent, ev.AccessKeyID, ev.ErrorCode, ev.ErrorMessage,
		ev.Time.UTC().Format("2006-01-02 15:04:05"),
	}, " "))
	return strings.Contains(hay, q)
}

// sortEvents orders by the chosen column; col -1 keeps the API's newest-first
// order. Column indices match the table header (1=TIME … 7=OUTCOME); column 0
// ("#") is positional and never a sort target.
func sortEvents(evs []trail.Event, col int, asc bool) {
	if col <= 0 {
		return
	}
	less := func(a, b trail.Event) bool {
		switch col {
		case 1:
			return a.Time.Before(b.Time)
		case 2:
			return a.EventName < b.EventName
		case 3:
			return a.EventSource < b.EventSource
		case 4:
			return a.Region < b.Region
		case 5:
			return a.Principal < b.Principal
		case 6:
			return a.SourceIP < b.SourceIP
		default:
			return a.ErrorCode < b.ErrorCode
		}
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if asc {
			return less(evs[i], evs[j])
		}
		return less(evs[j], evs[i])
	})
}
