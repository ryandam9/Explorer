// Package billtui is the live bill screen (`aws_explorer bill --tui`). It
// shows the period's cost per service and usage type with usage quantities
// and a running total, and re-fetches from Cost Explorer on a fixed interval
// so activity that incurs cost surfaces without restarting — each refresh is
// one paid API request, which is why the interval is user-controlled and the
// status line names it. A CHANGE column shows what moved since the last refresh,
// and a per-service resource breakdown answers "which resource exactly?"
// when the account has resource-level data enabled.
package billtui

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/billing"
	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/debugpane"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// billMsg delivers one refresh's bill (or its failure).
type billMsg struct {
	bill *billing.Bill
	err  error
}

// tickMsg fires the next auto-refresh; stale timer IDs are ignored so manual
// refreshes don't stack timers.
type tickMsg struct{ id int }

// resourcesMsg delivers the per-resource breakdown for one service.
type resourcesMsg struct {
	service string
	rows    []billing.ResourceCost
	start   time.Time
	err     error
}

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayDetail
	overlayResources
	overlayHelp
)

// columns of the bill table. The sequence number and service stay pinned
// while the rest scroll horizontally on narrow terminals. Column 0 ("#") is a
// positional row counter, not a sortable field.
var columns = []table.Column{
	{Title: "#", Width: 5},
	{Title: "SERVICE", Width: 30},
	{Title: "USAGE TYPE", Width: 32},
	{Title: "USAGE", Width: 14},
	{Title: "UNIT", Width: 10},
	{Title: "COST", Width: 12},
	{Title: "CHANGE", Width: 11},
}

// Model is the live bill TUI.
type Model struct {
	ctx      context.Context
	api      billing.API
	interval time.Duration
	start    time.Time
	end      time.Time
	label    string // header period name ("June 2026 (month-to-date)")
	profile  string // for SSO login hints on credential errors

	bill     *billing.Bill
	visible  []billing.Line
	deltas   map[string]float64 // line key → amount change vs previous fetch
	fetching bool
	fetchErr string
	updated  time.Time
	timerID  int

	tbl       table.Model
	filtering bool
	filter    textinput.Model

	// sortCol -1 is the natural cost-descending order from billing.Fetch.
	sortCol int
	sortAsc bool

	overlay     overlayKind
	resService  string
	resRows     []billing.ResourceCost
	resStart    time.Time
	resErr      string
	resFetching bool
	resScroll   int

	spin   spinner.Model
	status string // transient message in the status bar (copy/export results)

	debug debugpane.Model // "~" live activity overlay

	width, height int
}

// New builds the live bill TUI for the period [start, end). interval is the
// auto-refresh cadence; profile feeds the SSO login hint on auth errors.
func New(ctx context.Context, api billing.API, start, end time.Time, label string, interval time.Duration, profile string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	fi := textinput.New()
	fi.Placeholder = "Filter bill lines…"
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

	return Model{
		ctx:      ctx,
		api:      api,
		interval: interval,
		start:    start,
		end:      end,
		label:    label,
		profile:  profile,
		fetching: true,
		tbl:      tbl,
		filter:   fi,
		sortCol:  -1,
		spin:     sp,
	}
}

// PageTitle implements ui.Titled.
func (m Model) PageTitle() string { return "Bill › " + m.start.Format("January 2006") }

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), m.spin.Tick)
}

// fetchCmd fetches the bill off the Update loop.
func (m Model) fetchCmd() tea.Cmd {
	ctx, api, start, end := m.ctx, m.api, m.start, m.end
	return func() tea.Msg {
		slog.Info("Fetching cost & usage from Cost Explorer",
			"start", start.Format("2006-01-02"), "end", end.Format("2006-01-02"))
		b, err := billing.Fetch(ctx, api, start, end)
		if err != nil {
			slog.Warn("Cost Explorer fetch failed", "error", err.Error())
		} else if b != nil {
			slog.Info("Fetched bill", "lines", len(b.Lines))
		}
		return billMsg{bill: b, err: err}
	}
}

// scheduleTick arms the next auto-refresh, invalidating any pending timer.
func (m *Model) scheduleTick() tea.Cmd {
	m.timerID++
	id := m.timerID
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{id: id} })
}

// resourcesCmd fetches the per-resource breakdown for one service.
func (m Model) resourcesCmd(service string) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		slog.Info("Fetching per-resource costs", "service", service)
		rows, start, err := billing.FetchResources(ctx, api, service, time.Now())
		if err != nil {
			slog.Warn("Per-resource cost fetch failed", "service", service, "error", err.Error())
		}
		return resourcesMsg{service: service, rows: rows, start: start, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While the debug overlay is open, it consumes key/mouse input; every other
	// message falls through so refreshes keep flowing underneath.
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
		if !m.fetching && !m.resFetching {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case billMsg:
		m.fetching = false
		if msg.err != nil {
			m.fetchErr = friendlyError(msg.err, m.profile)
			// Keep the previous bill on screen and retry on the next tick.
			return m, m.scheduleTick()
		}
		m.fetchErr = ""
		m.deltas = diffBills(m.bill, msg.bill)
		m.bill = msg.bill
		m.updated = time.Now()
		m.rebuild()
		return m, m.scheduleTick()

	case tickMsg:
		if msg.id != m.timerID || m.fetching {
			return m, nil
		}
		m.fetching = true
		return m, tea.Batch(m.fetchCmd(), m.spin.Tick)

	case resourcesMsg:
		if msg.service != m.resService {
			return m, nil // a stale fetch for a closed overlay
		}
		m.resFetching = false
		m.resRows, m.resStart = msg.rows, msg.start
		if msg.err != nil {
			if errors.Is(msg.err, billing.ErrResourceDataDisabled) {
				m.resErr = billing.ErrResourceDataDisabled.Error()
			} else {
				m.resErr = friendlyError(msg.err, m.profile)
			}
		}
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
			m.filter.Blur()
			m.filter.SetValue("")
			m.rebuild()
		case "enter":
			m.filtering = false
			m.filter.Blur()
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.rebuild()
			return m, cmd
		}
		return m, nil
	}

	if m.overlay != overlayNone {
		switch msg.String() {
		case "up", "k":
			if m.overlay == overlayResources && m.resScroll > 0 {
				m.resScroll--
			}
		case "down", "j":
			if m.overlay == overlayResources && m.resScroll < len(m.resRows)-1 {
				m.resScroll++
			}
		case "esc", "q", "enter", "?", "x":
			m.overlay = overlayNone
			m.resService = ""
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "enter":
		if m.selected() != nil {
			m.overlay = overlayDetail
		}
	case "u":
		if !m.fetching {
			m.fetching = true
			return m, tea.Batch(m.fetchCmd(), m.spin.Tick)
		}
	case "x":
		if l := m.selected(); l != nil && !m.resFetching {
			m.overlay = overlayResources
			m.resService = l.Service
			m.resRows, m.resErr, m.resScroll = nil, "", 0
			m.resFetching = true
			return m, tea.Batch(m.resourcesCmd(l.Service), m.spin.Tick)
		}
	case "s":
		// Cycle: natural cost ranking (-1) → each data column → back. The "#"
		// column (index 0) is a positional counter, so it is skipped. USAGE,
		// COST and CHANGE start descending (biggest first).
		m.sortCol++
		if m.sortCol >= len(columns) {
			m.sortCol = -1
		} else if m.sortCol == 0 {
			m.sortCol = 1
		}
		m.sortAsc = !(m.sortCol == 3 || m.sortCol == 5 || m.sortCol == 6)
		m.rebuild()
	case "R":
		if m.sortCol > 0 {
			m.sortAsc = !m.sortAsc
			m.rebuild()
		}
	case "r":
		m.filter.SetValue("")
		m.sortCol = -1
		m.rebuild()
	case "<", ",":
		m.tbl.ScrollLeft()
	case ">", ".":
		m.tbl.ScrollRight()
	case "y":
		if l := m.selected(); l != nil {
			m.copyToClipboard(l.Service + " " + l.UsageType)
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

// selected returns the bill line under the cursor, or nil.
func (m *Model) selected() *billing.Line {
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
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}
	dir, err := csvexport.DefaultDir()
	if err == nil {
		var rows [][]string
		for _, l := range m.visible {
			rows = append(rows, []string{
				l.Service, l.UsageType,
				strconv.FormatFloat(l.Quantity, 'f', -1, 64), l.Unit,
				strconv.FormatFloat(l.Amount, 'f', -1, 64), currency,
			})
		}
		var path string
		path, err = csvexport.Write(dir, "bill",
			[]string{"Service", "UsageType", "Usage", "Unit", "Cost", "Currency"}, rows)
		if err == nil {
			m.status = "exported " + path
			return
		}
	}
	m.status = "export failed: " + err.Error()
}

// rebuild recomputes the visible lines (filter + sort) and table rows.
func (m *Model) rebuild() {
	var lines []billing.Line
	if m.bill != nil {
		lines = m.bill.Lines
	}
	m.visible = filterLines(lines, m.filter.Value())
	m.sortVisible()

	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}
	rows := make([]table.Row, 0, len(m.visible))
	for i, l := range m.visible {
		rows = append(rows, table.Row{
			strconv.Itoa(i + 1),
			l.Service, l.UsageType, billing.FormatQty(l.Quantity), l.Unit,
			billing.FormatAmount(l.Amount, currency),
			formatDelta(m.deltas[l.Key()], currency),
		})
	}
	m.tbl.SetRows(rows)
	if c := m.tbl.Cursor(); c >= len(rows) && len(rows) > 0 {
		m.tbl.SetCursor(len(rows) - 1)
	}
}

// diffBills maps each line key to its amount change between two fetches.
// Lines that newly appeared count with their full amount; unchanged lines
// are absent.
func diffBills(prev, next *billing.Bill) map[string]float64 {
	if prev == nil || next == nil {
		return nil
	}
	old := make(map[string]float64, len(prev.Lines))
	for _, l := range prev.Lines {
		old[l.Key()] = l.Amount
	}
	deltas := map[string]float64{}
	for _, l := range next.Lines {
		if d := l.Amount - old[l.Key()]; d != 0 {
			deltas[l.Key()] = d
		}
	}
	return deltas
}

// formatDelta renders an amount change for the CHANGE column ("+$0.42"); zero
// or untracked is blank so a quiet bill reads quiet.
func formatDelta(d float64, currency string) string {
	if d == 0 {
		return ""
	}
	if d > 0 {
		return "+" + billing.FormatAmount(d, currency)
	}
	return billing.FormatAmount(d, currency)
}

// filterLines keeps lines matching the query in any field (case-insensitive
// substring), preserving order.
func filterLines(lines []billing.Line, query string) []billing.Line {
	out := make([]billing.Line, 0, len(lines))
	q := strings.ToLower(strings.TrimSpace(query))
	for _, l := range lines {
		if q == "" || strings.Contains(strings.ToLower(l.Service+" "+l.UsageType+" "+l.Unit), q) {
			out = append(out, l)
		}
	}
	return out
}

// sortVisible orders by the chosen column; col -1 keeps the cost-descending
// order lines arrive in. Column indices match the table header (1=SERVICE …
// 6=CHANGE); column 0 ("#") is positional and never a sort target.
func (m *Model) sortVisible() {
	col, asc := m.sortCol, m.sortAsc
	if col <= 0 {
		return
	}
	less := func(a, b billing.Line) bool {
		switch col {
		case 1:
			return a.Service < b.Service
		case 2:
			return a.UsageType < b.UsageType
		case 3:
			return a.Quantity < b.Quantity
		case 4:
			return a.Unit < b.Unit
		case 6:
			return m.deltas[a.Key()] < m.deltas[b.Key()]
		default:
			return a.Amount < b.Amount
		}
	}
	sort.SliceStable(m.visible, func(i, j int) bool {
		if asc {
			return less(m.visible[i], m.visible[j])
		}
		return less(m.visible[j], m.visible[i])
	})
}

// friendlyError prefers the SSO login hint over the raw SDK error chain.
func friendlyError(err error, profile string) string {
	if hint, ok := awserr.LoginHint(err, profile); ok {
		return hint
	}
	return err.Error()
}
