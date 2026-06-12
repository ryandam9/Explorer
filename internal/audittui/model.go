// Package audittui is the interactive viewer for audit findings
// (`aws_explorer audit --tui`). Findings stream in per region while the scan
// runs; the table supports quick filtering, sorting, horizontal column
// scrolling, a detail overlay per finding, CSV export, and an errors overlay
// — the same interaction language as every other TUI in the application.
package audittui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/audit"
	"github.com/ryandam9/aws_explorer/internal/costs"
	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// chunkMsg delivers one region's audit result (ok=false: channel closed,
// the scan is complete).
type chunkMsg struct {
	chunk audit.CostChunk
	ok    bool
}

func waitForChunk(ch <-chan audit.CostChunk) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		return chunkMsg{chunk: c, ok: ok}
	}
}

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayDetail
	overlayErrors
	overlayHelp
)

// columns of the findings table. Severity stays pinned while the rest scroll
// horizontally on narrow terminals.
var columns = []table.Column{
	{Title: "SEVERITY", Width: 12},
	{Title: "ID", Width: 14},
	{Title: "RESOURCE", Width: 26},
	{Title: "REGION", Width: 14},
	{Title: "ISSUE", Width: 44},
	{Title: "EST/MO", Width: 9},
}

// Model is the audit TUI.
type Model struct {
	ch       <-chan audit.CostChunk
	regions  int
	scanned  int
	scanning bool

	all     []findings.Finding // merged, kept in findings.Sort order
	visible []findings.Finding // filter + sort applied; row i shows visible[i]
	errs    []model.ExploreError

	tbl       table.Model
	filtering bool
	filter    textinput.Model

	// sortCol -1 is the natural severity/cost ranking from findings.Sort.
	sortCol int
	sortAsc bool

	overlay   overlayKind
	errScroll int

	spin   spinner.Model
	status string // transient message in the status bar (copy/export results)

	width, height int
}

// New builds the audit TUI. regions sizes the progress meter; ch streams one
// chunk per region and is closed by the producer when the scan completes.
func New(regions []string, ch <-chan audit.CostChunk) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	fi := textinput.New()
	fi.Placeholder = "Filter findings…"
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
		table.WithFrozenColumns(1),
	)

	return Model{
		ch:       ch,
		regions:  len(regions),
		scanning: true,
		tbl:      tbl,
		filter:   fi,
		sortCol:  -1,
		spin:     sp,
	}
}

// PageTitle implements ui.Titled.
func (m Model) PageTitle() string { return "Audit › Cost" }

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForChunk(m.ch), m.spin.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutTable()
		return m, nil

	case spinner.TickMsg:
		if !m.scanning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case chunkMsg:
		if !msg.ok {
			m.scanning = false
			return m, nil
		}
		m.scanned++
		m.all = append(m.all, msg.chunk.Findings...)
		findings.Sort(m.all)
		m.errs = append(m.errs, msg.chunk.Errors...)
		m.rebuild()
		return m, waitForChunk(m.ch)

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
			if m.overlay == overlayErrors && m.errScroll > 0 {
				m.errScroll--
			}
		case "down", "j":
			if m.overlay == overlayErrors && m.errScroll < len(m.errs)-1 {
				m.errScroll++
			}
		case "esc", "q", "enter", "?", "e":
			m.overlay = overlayNone
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
	case "s":
		// Cycle: ranking (-1) → each column → back to ranking. Severity and
		// EST/MO start descending (worst/most expensive first).
		m.sortCol++
		if m.sortCol >= len(columns) {
			m.sortCol = -1
		}
		m.sortAsc = !(m.sortCol == 0 || m.sortCol == 5)
		m.rebuild()
	case "R":
		if m.sortCol >= 0 {
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
		if f := m.selected(); f != nil {
			m.copyToClipboard(firstNonEmpty(f.ARN, f.Resource))
		}
	case "C":
		m.exportCSV()
	case "e":
		if len(m.errs) > 0 {
			m.errScroll = 0
			m.overlay = overlayErrors
		}
	case "?":
		m.overlay = overlayHelp
	default:
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		return m, cmd
	}
	return m, nil
}

// selected returns the finding under the cursor, or nil.
func (m *Model) selected() *findings.Finding {
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
		for _, f := range m.visible {
			rows = append(rows, []string{
				f.Severity.String(), f.ID, f.Resource, f.Region, f.Title, f.Detail,
				strconv.FormatFloat(f.EstMonthlyUSD, 'f', 2, 64), f.Fix,
			})
		}
		var path string
		path, err = csvexport.Write(dir, "cost-audit",
			[]string{"Severity", "ID", "Resource", "Region", "Issue", "Detail", "EstMonthlyUSD", "Fix"}, rows)
		if err == nil {
			m.status = "exported " + path
			return
		}
	}
	m.status = "export failed: " + err.Error()
}

// rebuild recomputes the visible findings (filter + sort) and table rows.
func (m *Model) rebuild() {
	m.visible = filterFindings(m.all, m.filter.Value())
	sortFindings(m.visible, m.sortCol, m.sortAsc)

	rows := make([]table.Row, 0, len(m.visible))
	for _, f := range m.visible {
		rows = append(rows, table.Row{
			f.Severity.Badge(), f.ID, f.Resource, f.Region, f.Title,
			costs.Dollars(f.EstMonthlyUSD),
		})
	}
	m.tbl.SetRows(rows)
	if c := m.tbl.Cursor(); c >= len(rows) && len(rows) > 0 {
		m.tbl.SetCursor(len(rows) - 1)
	}
}

// filterFindings keeps findings matching the query in any displayed or
// detail field (case-insensitive substring), preserving order.
func filterFindings(fs []findings.Finding, query string) []findings.Finding {
	out := make([]findings.Finding, 0, len(fs))
	q := strings.ToLower(strings.TrimSpace(query))
	for _, f := range fs {
		if q == "" || matchesFinding(f, q) {
			out = append(out, f)
		}
	}
	return out
}

func matchesFinding(f findings.Finding, q string) bool {
	hay := strings.ToLower(strings.Join([]string{
		f.Severity.String(), f.ID, f.Resource, f.Region, f.Service,
		f.Title, f.Detail, f.Fix, f.ARN,
	}, " "))
	return strings.Contains(hay, q)
}

// sortFindings orders by the chosen column; col -1 keeps the incoming
// findings.Sort ranking.
func sortFindings(fs []findings.Finding, col int, asc bool) {
	if col < 0 {
		return
	}
	less := func(a, b findings.Finding) bool {
		switch col {
		case 0:
			return a.Severity < b.Severity
		case 1:
			return a.ID < b.ID
		case 2:
			return a.Resource < b.Resource
		case 3:
			return a.Region < b.Region
		case 4:
			return a.Title < b.Title
		default:
			return a.EstMonthlyUSD < b.EstMonthlyUSD
		}
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if asc {
			return less(fs[i], fs[j])
		}
		return less(fs[j], fs[i])
	})
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
