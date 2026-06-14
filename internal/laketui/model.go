// Package laketui is the interactive viewer for CloudTrail Lake query results
// (`aws_explorer lake --tui`). A query runs once; its generic columnar result
// is shown in a table that supports quick filtering, (numeric-aware) sorting,
// horizontal column scrolling, a per-row detail overlay, and CSV export — the
// same interaction language as the other TUIs.
package laketui

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
	"github.com/ryandam9/aws_explorer/internal/traillake"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

type loadedMsg struct {
	result traillake.Result
	err    error
}

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayDetail
	overlayHelp
)

const maxColWidth = 48

// Model is the CloudTrail Lake results TUI.
type Model struct {
	ctx  context.Context
	cfg  aws.Config
	sql  string
	opts traillake.QueryOptions

	title  string // human-readable query description for the header
	store  string // event data store name/id, for the header
	region string

	loading bool
	loadErr error
	result  traillake.Result

	baseCols []table.Column // built from the result columns (no sort arrow)
	allRows  [][]string     // every result row
	visible  [][]string     // filter + sort applied; row i = visible[i]

	tbl       table.Model
	filtering bool
	filterIn  textinput.Model

	sortCol int // -1 natural; otherwise a result-column index (0-based)
	sortAsc bool

	overlay overlayKind

	spin   spinner.Model
	status string
	debug  debugpane.Model

	width, height int
}

// New builds the Lake results TUI. The query runs in Init.
func New(ctx context.Context, cfg aws.Config, sql string, opts traillake.QueryOptions, title, store, region string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	fi := textinput.New()
	fi.Placeholder = "Filter rows…"
	fi.CharLimit = 128
	fi.Width = 32
	fi.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	fi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	fi.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	fi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	tbl := table.New(
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)

	return Model{
		ctx:      ctx,
		cfg:      cfg,
		sql:      sql,
		opts:     opts,
		title:    title,
		store:    store,
		region:   region,
		loading:  true,
		tbl:      tbl,
		filterIn: fi,
		sortCol:  -1,
		spin:     sp,
	}
}

// PageTitle implements ui.Titled.
func (m Model) PageTitle() string { return "CloudTrail Lake" }

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.spin.Tick)
}

func (m Model) load() tea.Cmd {
	return func() tea.Msg {
		res, err := traillake.RunQuery(m.ctx, m.cfg, m.sql, m.opts)
		return loadedMsg{result: res, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.result = msg.result
		m.allRows = msg.result.Rows
		m.baseCols = m.columnsFor(msg.result.Columns)
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
	case "s":
		// Cycle: natural (-1) → each result column → back. Column 0 ("#") is
		// positional and skipped.
		m.sortCol++
		if m.sortCol >= len(m.result.Columns) {
			m.sortCol = -1
		}
		m.sortAsc = true
		m.rebuild()
	case "R":
		if m.sortCol >= 0 {
			m.sortAsc = !m.sortAsc
			m.rebuild()
		}
	case "r":
		m.filterIn.SetValue("")
		m.sortCol = -1
		m.rebuild()
	case "<", ",":
		m.tbl.ScrollLeft()
	case ">", ".":
		m.tbl.ScrollRight()
	case "y":
		if row := m.selected(); row != nil {
			m.copyToClipboard(strings.Join(*row, "\t"))
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

func (m *Model) selected() *[]string {
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
	m.status = "copied row"
}

func (m *Model) exportCSV() {
	if len(m.visible) == 0 {
		m.status = "nothing to export"
		return
	}
	dir, err := csvexport.DefaultDir()
	if err == nil {
		var path string
		path, err = csvexport.Write(dir, "cloudtrail-lake", m.result.Columns, m.visible)
		if err == nil {
			m.status = "exported " + path
			return
		}
	}
	m.status = "export failed: " + err.Error()
}

// columnsFor builds table columns from the result column names, sizing each to
// its widest value (capped), with a leading positional "#" column.
func (m *Model) columnsFor(names []string) []table.Column {
	cols := make([]table.Column, 0, len(names)+1)
	cols = append(cols, table.Column{Title: "#", Width: 5})
	for ci, name := range names {
		w := len(name)
		for _, row := range m.allRows {
			if ci < len(row) && len(row[ci]) > w {
				w = len(row[ci])
			}
		}
		if w > maxColWidth {
			w = maxColWidth
		}
		if w < 6 {
			w = 6
		}
		cols = append(cols, table.Column{Title: strings.ToUpper(name), Width: w})
	}
	return cols
}

func (m *Model) rebuild() {
	m.visible = filterRows(m.allRows, m.filterIn.Value())
	sortRows(m.visible, m.sortCol, m.sortAsc)

	// The table sort arrow goes on the active column. Table column index is the
	// result column index + 1 (for the leading "#").
	cols := append([]table.Column(nil), m.baseCols...)
	tableSortCol := -1
	if m.sortCol >= 0 {
		tableSortCol = m.sortCol + 1
	}
	table.ApplySortHeader(cols, tableSortCol, m.sortAsc, func(i int) bool { return i > 0 })
	m.tbl.SetColumns(cols)

	rows := make([]table.Row, 0, len(m.visible))
	for i, r := range m.visible {
		rows = append(rows, append(table.Row{strconv.Itoa(i + 1)}, r...))
	}
	m.tbl.SetRows(rows)
	if c := m.tbl.Cursor(); c >= len(rows) && len(rows) > 0 {
		m.tbl.SetCursor(len(rows) - 1)
	}
}

func filterRows(rows [][]string, query string) [][]string {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		if q == "" || rowMatches(r, q) {
			out = append(out, r)
		}
	}
	return out
}

func rowMatches(row []string, q string) bool {
	return strings.Contains(strings.ToLower(strings.Join(row, " ")), q)
}

// sortRows orders by the given result-column index; col < 0 keeps the query's
// own order. Cells that both parse as numbers compare numerically, so count
// columns rank correctly instead of lexically ("10" before "9").
func sortRows(rows [][]string, col int, asc bool) {
	if col < 0 {
		return
	}
	cell := func(r []string) string {
		if col < len(r) {
			return r[col]
		}
		return ""
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := cell(rows[i]), cell(rows[j])
		if asc {
			return lessCell(a, b)
		}
		return lessCell(b, a)
	})
}

func lessCell(a, b string) bool {
	af, aerr := strconv.ParseFloat(a, 64)
	bf, berr := strconv.ParseFloat(b, 64)
	if aerr == nil && berr == nil {
		return af < bf
	}
	return a < b
}
