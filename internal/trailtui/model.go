// Package trailtui is the interactive viewer for the CloudTrail activity feed
// (`aws_explorer trail --tui`). Events are fetched once for the chosen scope
// (a resource, a principal/event/source pivot, or the whole account) and shown
// in a table that supports quick filtering, sorting, horizontal column
// scrolling, a "failed calls only" toggle, and a per-event detail overlay —
// the same interaction language as the other TUIs in the application.
package trailtui

import (
	"context"
	"sort"
	"strconv"
	"strings"

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

// loadedMsg delivers the result of the one-shot CloudTrail lookup.
type loadedMsg struct {
	events    []trail.Event
	truncated bool
	err       error
}

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
	{Title: "EVENT", Width: 30},
	{Title: "PRINCIPAL", Width: 26},
	{Title: "SOURCE IP", Width: 18},
	{Title: "OUTCOME", Width: 18},
}

// Model is the CloudTrail activity-feed TUI.
type Model struct {
	// Lookup inputs, captured at construction and used by the load command.
	ctx    context.Context
	cfg    aws.Config
	region string
	filter trail.Filter
	opts   trail.Options // fetch options (errors-only is applied client-side)
	scope  string        // human-readable scope for the header

	loading   bool
	loadErr   error
	truncated bool

	all     []trail.Event // every fetched event, newest first
	visible []trail.Event // filter + errors-only + sort applied; row i = visible[i]

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

// New builds the CloudTrail feed TUI. The lookup runs in Init. opts.ErrorsOnly
// is honored as the initial state of the in-TUI "failed only" toggle but the
// fetch itself pulls everything, so the toggle can be flipped without another
// (rate-limited) API round trip.
func New(ctx context.Context, cfg aws.Config, region string, filter trail.Filter, opts trail.Options, scope string) Model {
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

	return Model{
		ctx:        ctx,
		cfg:        cfg,
		region:     region,
		filter:     filter,
		opts:       opts,
		scope:      scope,
		loading:    true,
		errorsOnly: errorsOnly,
		tbl:        tbl,
		filterIn:   fi,
		sortCol:    -1,
		spin:       sp,
	}
}

// PageTitle implements ui.Titled.
func (m Model) PageTitle() string { return "CloudTrail" }

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.spin.Tick)
}

func (m Model) load() tea.Cmd {
	return func() tea.Msg {
		evs, trunc, err := trail.LookupFiltered(m.ctx, m.cfg, m.region, m.filter, m.opts)
		return loadedMsg{events: evs, truncated: trunc, err: err}
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

	case loadedMsg:
		m.loading = false
		m.loadErr = msg.err
		m.all = msg.events
		m.truncated = msg.truncated
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
				ev.EventName, ev.Principal, ev.SourceIP,
				strconv.FormatBool(ev.ReadOnly), ev.ErrorCode,
			})
		}
		var path string
		path, err = csvexport.Write(dir, "cloudtrail",
			[]string{"Time", "Event", "Principal", "SourceIP", "ReadOnly", "ErrorCode"}, rows)
		if err == nil {
			m.status = "exported " + path
			return
		}
	}
	m.status = "export failed: " + err.Error()
}

// rebuild recomputes the visible events (filter + errors-only + sort) and the
// table rows.
func (m *Model) rebuild() {
	m.visible = filterEvents(m.all, m.filterIn.Value(), m.errorsOnly)
	sortEvents(m.visible, m.sortCol, m.sortAsc)

	cols := append([]table.Column(nil), columns...)
	table.ApplySortHeader(cols, m.sortCol, m.sortAsc, func(i int) bool { return i > 0 })
	m.tbl.SetColumns(cols)

	rows := make([]table.Row, 0, len(m.visible))
	for i, ev := range m.visible {
		rows = append(rows, table.Row{
			strconv.Itoa(i + 1),
			ev.Time.UTC().Format("2006-01-02 15:04:05"),
			eventLabel(ev), ev.Principal, ev.SourceIP, outcomeLabel(ev),
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
		ev.EventName, ev.Principal, ev.SourceIP, ev.ErrorCode,
		ev.Time.UTC().Format("2006-01-02 15:04:05"),
	}, " "))
	return strings.Contains(hay, q)
}

// sortEvents orders by the chosen column; col -1 keeps the API's newest-first
// order. Column indices match the table header (1=TIME … 5=OUTCOME); column 0
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
			return a.Principal < b.Principal
		case 4:
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
