package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/huh"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	btable "github.com/evertras/bubble-table/table"
	zone "github.com/lrstanley/bubblezone"

	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/model"
)

// ── Column keys ──────────────────────────────────────────────────────────────

const (
	colNum     = "num"
	colService = "service"
	colType    = "type"
	colRegion  = "region"
	colID      = "id"
	colName    = "name"
	colState   = "state"
	colMeta    = "__resource"
)

// ── Focus ────────────────────────────────────────────────────────────────────

type panelFocus int

const (
	focusSidebar panelFocus = iota
	focusTable
	focusDetail
)

// ── Layout constants ─────────────────────────────────────────────────────────

const (
	sidebarInner = 16 // inner content width of the sidebar panel
	detailInner  = 34 // inner content width of the detail panel
)

// ── Zone IDs ─────────────────────────────────────────────────────────────────

const zoneSvc = "svc-"

// ── Message types ─────────────────────────────────────────────────────────────

type chunkMsg model.ResultChunk
type doneMsg struct{}
type clearToastMsg struct{}

// ── Styles (theme-aware; resolved at render time via color accessors) ─────────

func detailKeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorText()))
}
func detailSectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading())).Underline(true)
}
func privilegeErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorError())).
		Padding(0, 1)
}
func privilegeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorError()))
}
func privilegeHintStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning()))
}

// ── Model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	ctx    context.Context
	engine *engine.Engine
	chunks chan model.ResultChunk

	// Data
	results []model.Resource
	errors  []model.ExploreError
	loading bool
	done    bool

	// Terminal size
	width  int
	height int

	// Service sidebar
	services      []string
	activeService int

	// Resource table (evertras)
	table   btable.Model
	allRows map[string][]btable.Row

	// Detail panel
	showDetail     bool
	detail         *model.Resource
	detailViewport viewport.Model

	// Focus
	focus panelFocus

	// Toast notification
	toast    string
	toastExp time.Time

	// Filter form (huh)
	showFilter   bool
	filterForm   *huh.Form
	filterRegion string
	filterState  string

	// Spinner for loading state
	spinner spinner.Model

	// Zone manager for mouse support
	zones *zone.Manager

	// Settings panel
	showSettings bool
	settings     settingsModel
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewModel creates the TUI model.  configPath is the path to the YAML config
// file on disk (used by the settings panel to persist changes); cfg is the
// already-loaded config struct.
func NewModel(ctx context.Context, eng *engine.Engine, configPath string, cfg *config.Config) tea.Model {
	chunks := make(chan model.ResultChunk, 64)
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	zoneM := zone.New()

	m := tuiModel{
		ctx:     ctx,
		engine:  eng,
		loading: true,
		chunks:  chunks,
		focus:   focusTable,
		spinner: sp,
		zones:   zoneM,
		allRows: make(map[string][]btable.Row),
	}
	m.settings = newSettingsModel(0, 0, configPath, cfg)
	m.table = m.makeTable()
	return m
}

func (m tuiModel) Init() tea.Cmd {
	go m.engine.StreamRun(m.ctx, m.chunks)
	return tea.Batch(waitForChunk(m.chunks), m.spinner.Tick)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Route all events to the settings panel when it is open.
	if m.showSettings {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" && !m.settings.editMode {
				m.showSettings = false
				return m, nil
			}
		case settingsSavedMsg:
			m.showSettings = false
			m.setToast("Theme saved: " + msg.theme)
			cmds = append(cmds, toastCmd(3*time.Second))
			// Rebuild table styles to pick up new theme colors.
			rows := m.currentRows()
			m.table = m.makeTable().WithRows(rows)
			return m, tea.Batch(cmds...)
		case settingsErrMsg:
			m.showSettings = false
			m.setToast("Save failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
			return m, tea.Batch(cmds...)
		}
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	// Route all events to the filter form when it is open.
	if m.showFilter && m.filterForm != nil {
		newForm, formCmd := m.filterForm.Update(msg)
		if f, ok := newForm.(*huh.Form); ok {
			m.filterForm = f
		}
		cmds = append(cmds, formCmd)

		switch m.filterForm.State {
		case huh.StateCompleted:
			m.showFilter = false
			m.filterRegion = m.filterForm.GetString("region")
			m.filterState = m.filterForm.GetString("state")
			m.rebuildAllRows()
			m.updateTableRows()
			m.setToast("Filter applied")
			cmds = append(cmds, toastCmd(3*time.Second))
		case huh.StateAborted:
			m.showFilter = false
		}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Rebuild table with new dimensions.
		rows := m.currentRows()
		m.table = m.makeTable().WithRows(rows)
		if m.showDetail && m.detail != nil {
			m.syncDetailViewport()
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "esc":
			if m.showDetail {
				m.showDetail = false
				m.detail = nil
				m.focus = focusTable
				m.table = m.table.Focused(true)
			}

		case "tab":
			m.cycleFocus(1)

		case "shift+tab":
			m.cycleFocus(-1)

		case "enter":
			switch m.focus {
			case focusSidebar:
				m.focus = focusTable
				m.table = m.table.Focused(true)
			case focusTable:
				row := m.table.HighlightedRow()
				if row.Data != nil {
					if res, ok := row.Data[colMeta].(model.Resource); ok {
						m.detail = &res
						m.showDetail = true
						m.focus = focusDetail
						m.table = m.table.Focused(false)
						m.syncDetailViewport()
					}
				}
			}

		case "[", "up":
			if m.focus == focusDetail {
				m.detailViewport.LineUp(3)
				return m, nil
			}
			if m.focus == focusSidebar {
				if m.activeService > 0 {
					m.activeService--
					m.updateTableRows()
				}
				return m, nil
			}

		case "]", "down":
			if m.focus == focusDetail {
				m.detailViewport.LineDown(3)
				return m, nil
			}
			if m.focus == focusSidebar {
				if m.activeService < len(m.services)-1 {
					m.activeService++
					m.updateTableRows()
				}
				return m, nil
			}

		case "f":
			if !m.showFilter && len(m.results) > 0 {
				m.filterForm = m.buildFilterForm()
				m.showFilter = true
				cmds = append(cmds, m.filterForm.Init())
			}

		case "r":
			m.filterRegion = ""
			m.filterState = ""
			m.rebuildAllRows()
			m.updateTableRows()
			m.setToast("Filters cleared")
			cmds = append(cmds, toastCmd(3*time.Second))

		case "S":
			m.settings = newSettingsModel(m.width, m.height, m.settings.configPath, m.settings.fullConfig)
			m.showSettings = true
		}

	case tea.MouseMsg:
		// Check sidebar zone clicks.
		for i := range m.services {
			zID := fmt.Sprintf("%s%d", zoneSvc, i)
			if m.zones.Get(zID).InBounds(msg) {
				if m.activeService != i {
					m.activeService = i
					m.updateTableRows()
				}
				m.focus = focusTable
				m.table = m.table.Focused(true)
				break
			}
		}

	case chunkMsg:
		m.loading = false
		m.results = append(m.results, msg.Resources...)
		m.errors = append(m.errors, msg.Errors...)
		m.rebuildAllRows()
		m.updateTableRows()
		cmds = append(cmds, waitForChunk(m.chunks))
		return m, tea.Batch(cmds...)

	case doneMsg:
		m.loading = false
		m.done = true

	case clearToastMsg:
		m.toast = ""
		m.toastExp = time.Time{}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Forward events to the table when it has focus.
	if m.focus == focusTable {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ── Focus helpers ─────────────────────────────────────────────────────────────

func (m *tuiModel) cycleFocus(dir int) {
	panels := 2
	if m.showDetail {
		panels = 3
	}
	next := panelFocus((int(m.focus) + dir + panels) % panels)
	m.focus = next
	m.table = m.table.Focused(m.focus == focusTable)
}

// ── Toast helpers ─────────────────────────────────────────────────────────────

func (m *tuiModel) setToast(msg string) {
	m.toast = msg
	m.toastExp = time.Now().Add(3 * time.Second)
}

func toastCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearToastMsg{} })
}

// ── Data helpers ──────────────────────────────────────────────────────────────

func (m *tuiModel) rebuildAllRows() {
	// Rebuild service list.
	svcSet := map[string]bool{}
	for _, r := range m.results {
		svcSet[r.Service] = true
	}
	names := make([]string, 0, len(svcSet)+1)
	names = append(names, "All")
	for svc := range svcSet {
		names = append(names, svc)
	}
	sort.Strings(names[1:])
	m.services = names

	// Clamp active service index.
	if m.activeService >= len(m.services) {
		m.activeService = 0
	}

	// Build rows grouped by service, applying structured filters.
	m.allRows = make(map[string][]btable.Row, len(names))
	for i, r := range m.results {
		if m.filterRegion != "" && r.Region != m.filterRegion {
			continue
		}
		if m.filterState != "" && r.State != m.filterState {
			continue
		}
		row := btable.NewRow(btable.RowData{
			colNum:     fmt.Sprintf("%d", i+1),
			colService: r.Service,
			colType:    r.Type,
			colRegion:  r.Region,
			colID:      r.ID,
			colName:    r.Name,
			colState:   r.State,
			colMeta:    r,
		})
		m.allRows["All"] = append(m.allRows["All"], row)
		m.allRows[r.Service] = append(m.allRows[r.Service], row)
	}
}

func (m tuiModel) currentRows() []btable.Row {
	if len(m.services) == 0 || m.activeService >= len(m.services) {
		return nil
	}
	return m.allRows[m.services[m.activeService]]
}

func (m *tuiModel) updateTableRows() {
	m.table = m.table.WithRows(m.currentRows())
}

// ── Table construction ────────────────────────────────────────────────────────

func (m tuiModel) makeTable() btable.Model {
	w := m.tableWidth()
	pageRows := m.tableHeight() - 4
	if pageRows < 5 {
		pageRows = 5
	}

	hlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorHighlightText())).
		Background(lipgloss.Color(ColorHighlight())).
		Bold(true)

	hdrStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorMuted()))

	return btable.New(m.columns(w)).
		WithPageSize(pageRows).
		WithPaginationWrapping(false).
		Focused(m.focus == focusTable).
		Filtered(true).
		WithTargetWidth(w).
		HighlightStyle(hlStyle).
		HeaderStyle(hdrStyle).
		WithBaseStyle(lipgloss.NewStyle().Align(lipgloss.Left)).
		SortByAsc(colService)
}

func (m tuiModel) columns(tableWidth int) []btable.Column {
	// Fixed widths: #(4) Service(10) Type(12) Region(13) State(10) = 49
	// Remaining split between ID and Name.
	fixed := 4 + 10 + 12 + 13 + 10
	border := 10 // evertras draws borders and separators
	rem := tableWidth - fixed - border
	if rem < 24 {
		rem = 24
	}
	idW := rem * 2 / 5
	nameW := rem - idW
	if idW < 10 {
		idW = 10
	}
	if nameW < 10 {
		nameW = 10
	}

	return []btable.Column{
		btable.NewColumn(colNum, "#", 4),
		btable.NewColumn(colService, "Service", 10).WithFiltered(true),
		btable.NewColumn(colType, "Type", 12).WithFiltered(true),
		btable.NewColumn(colRegion, "Region", 13).WithFiltered(true),
		btable.NewColumn(colID, "ID", idW).WithFiltered(true),
		btable.NewColumn(colName, "Name", nameW).WithFiltered(true),
		btable.NewColumn(colState, "State", 10).WithFiltered(true),
	}
}

func (m tuiModel) tableWidth() int {
	// sidebar panel: border(2) + padding(2) + content(sidebarInner) = sidebarInner+4
	sidebarOuter := sidebarInner + 4
	w := m.width - sidebarOuter - 2
	if m.showDetail {
		// detail panel: border(2) + padding(2) + content(detailInner) = detailInner+4
		w -= detailInner + 4
	}
	if w < 60 {
		return 60
	}
	return w
}

func (m tuiModel) tableHeight() int {
	h := m.height - 6 // header(2) + status bar(1) + panel borders(2) + breathing room
	if h < 8 {
		return 8
	}
	return h
}

// ── Filter form ───────────────────────────────────────────────────────────────

func (m tuiModel) buildFilterForm() *huh.Form {
	regionSet := map[string]bool{}
	stateSet := map[string]bool{}
	for _, r := range m.results {
		if r.Region != "" {
			regionSet[r.Region] = true
		}
		if r.State != "" {
			stateSet[r.State] = true
		}
	}

	regionOpts := []huh.Option[string]{huh.NewOption("— All regions —", "")}
	var regions []string
	for r := range regionSet {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	for _, r := range regions {
		regionOpts = append(regionOpts, huh.NewOption(r, r))
	}

	stateOpts := []huh.Option[string]{huh.NewOption("— All states —", "")}
	var states []string
	for s := range stateSet {
		states = append(states, s)
	}
	sort.Strings(states)
	for _, s := range states {
		stateOpts = append(stateOpts, huh.NewOption(s, s))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("region").
				Title("Filter by Region").
				Options(regionOpts...),
			huh.NewSelect[string]().
				Key("state").
				Title("Filter by State").
				Options(stateOpts...),
		),
	)
}

// ── Detail viewport ───────────────────────────────────────────────────────────

func (m *tuiModel) syncDetailViewport() {
	if m.detail == nil || m.width == 0 {
		return
	}
	vpWidth := detailInner - 2
	if vpWidth < 10 {
		vpWidth = 10
	}
	vpHeight := m.tableHeight()
	if vpHeight < 4 {
		vpHeight = 4
	}
	m.detailViewport = viewport.New(vpWidth, vpHeight)
	m.detailViewport.SetContent(m.renderDetail(*m.detail, vpWidth))
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	var output string

	if m.loading && len(m.results) == 0 {
		output = fmt.Sprintf("\n  %s  Loading AWS resources…\n", m.spinner.View())
		return m.zones.Scan(output)
	}

	if len(m.errors) > 0 && len(m.results) == 0 && m.done {
		output = m.renderPrivilegeErrors() + "\n\nPress q to quit."
		return m.zones.Scan(output)
	}

	header := m.renderHeader()
	status := m.statusBar()

	if m.showSettings {
		settingsView := m.settings.View()
		centered := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, settingsView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, centered, status)
	} else if m.showFilter && m.filterForm != nil {
		formW := 52
		formH := 14
		formView := ModalStyle(formW, formH).Render(m.filterForm.View())
		modal := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, formView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, modal, status)
	} else {
		body := m.renderBody()
		output = lipgloss.JoinVertical(lipgloss.Left, header, body, status)
	}

	// Overlay the toast at top-right if active.
	if m.toast != "" && time.Now().Before(m.toastExp) {
		toastRendered := lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorHighlightText())).
			Background(lipgloss.Color(ColorHighlight())).
			Padding(0, 1).
			Bold(true).
			Render("✓ " + m.toast)
		lines := strings.SplitN(output, "\n", 3)
		if len(lines) >= 2 {
			tl := lipgloss.PlaceHorizontal(m.width, lipgloss.Right, toastRendered)
			lines[1] = tl
			output = strings.Join(lines, "\n")
		}
	}

	return m.zones.Scan(output)
}

func (m tuiModel) renderHeader() string {
	status := "streaming"
	if m.done {
		status = "complete"
	} else if m.loading && len(m.results) == 0 {
		status = m.spinner.View() + " loading"
	}

	var filterParts []string
	if m.filterRegion != "" {
		filterParts = append(filterParts, "region:"+m.filterRegion)
	}
	if m.filterState != "" {
		filterParts = append(filterParts, "state:"+m.filterState)
	}
	filterInfo := ""
	if len(filterParts) > 0 {
		filterInfo = "  [" + strings.Join(filterParts, ", ") + "]"
	}

	title := fmt.Sprintf("  AWS Explorer  ·  %s  ·  %d resources  ·  %d errors%s",
		status, len(m.results), len(m.errors), filterInfo)

	w := m.width - 2
	if w < 10 {
		w = 10
	}
	return HeaderStyle().Width(w).Render(title)
}

func (m tuiModel) renderBody() string {
	sidebar := m.renderSidebar()
	tbl := m.renderTablePanel()
	if m.showDetail && m.detail != nil {
		detail := m.renderDetailPanel()
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, tbl, detail)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, tbl)
}

func (m tuiModel) renderSidebar() string {
	var b strings.Builder
	b.WriteString(PanelTitleStyle().Render("SERVICES") + "\n\n")

	for i, svc := range m.services {
		zID := fmt.Sprintf("%s%d", zoneSvc, i)
		label := svc
		if len(label) > sidebarInner-3 {
			label = label[:sidebarInner-4] + "…"
		}
		var line string
		if i == m.activeService {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorHighlightText())).
				Background(lipgloss.Color(ColorHighlight())).
				Width(sidebarInner - 2).
				Render("▶ " + label)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText())).
				Width(sidebarInner - 2).
				Render("  " + label)
		}
		b.WriteString(m.zones.Mark(zID, line) + "\n")
	}

	style := PanelStyle()
	if m.focus == focusSidebar {
		style = SelectedPanelStyle().
			BorderForeground(lipgloss.Color(FeatherColor(0)))
	}
	h := m.tableHeight() + 2
	if h < 6 {
		h = 6
	}
	return style.Width(sidebarInner).Height(h).Render(b.String())
}

func (m tuiModel) renderTablePanel() string {
	// Update column widths and page size to match current terminal dimensions.
	w := m.tableWidth()
	pageRows := m.tableHeight() - 4
	if pageRows < 5 {
		pageRows = 5
	}
	m.table = m.table.
		WithColumns(m.columns(w)).
		WithTargetWidth(w).
		WithPageSize(pageRows)

	borderColor := lipgloss.Color(FeatherColor(1))
	if m.focus == focusTable {
		borderColor = lipgloss.Color(FeatherColor(0))
	}
	_ = borderColor // evertras draws its own border; we note focus via highlight style

	return m.table.View()
}

func (m tuiModel) renderDetailPanel() string {
	style := PanelStyle()
	if m.focus == focusDetail {
		style = SelectedPanelStyle().
			BorderForeground(lipgloss.Color(FeatherColor(0)))
	}
	h := m.tableHeight() + 2
	if h < 6 {
		h = 6
	}
	scrollHint := ""
	if m.detailViewport.TotalLineCount() > m.detailViewport.VisibleLineCount() {
		pct := m.detailViewport.ScrollPercent()
		scrollHint = fmt.Sprintf("\n─ %d%% ─", int(pct*100))
	}
	content := m.detailViewport.View() + scrollHint
	return style.Width(detailInner).Height(h).Render(content)
}

// ── Status bar ────────────────────────────────────────────────────────────────

func (m tuiModel) statusBar() string {
	w := m.width - 4
	if w < 12 {
		w = 12
	}

	svc := "All"
	if len(m.services) > 0 && m.activeService < len(m.services) {
		svc = m.services[m.activeService]
	}
	status := "streaming…"
	if m.done {
		status = "complete"
	}

	left := fmt.Sprintf("Service: %s  ·  Resources: %d  ·  Errors: %d  ·  %s",
		svc, len(m.results), len(m.errors), status)

	var hints string
	switch m.focus {
	case focusSidebar:
		hints = "↑↓:service  Tab:panel  Enter:select  S:settings  q:quit"
	case focusTable:
		hints = "↑↓:nav  Enter:detail  /:filter  f:adv-filter  r:reset  S:settings  Tab:panel  q"
	case focusDetail:
		hints = "[]:scroll  Esc:close  S:settings  Tab:panel  q:quit"
	}

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(hints)
	inner := w - 2
	gap := inner - lw - rw
	if gap < 2 {
		gap = 2
	}
	content := left + strings.Repeat(" ", gap) + hints
	return StatusBarStyle(w).Render(content)
}

// ── Detail renderer ───────────────────────────────────────────────────────────

func (m tuiModel) renderDetail(r model.Resource, width int) string {
	var b strings.Builder

	fieldVal := func(v string, keyWidth, maxW int) string {
		if maxW <= 0 || len(v) <= maxW {
			return v
		}
		indent := strings.Repeat(" ", keyWidth+1)
		chunks := chunkString(v, maxW)
		return chunks[0] + "\n" + indent + strings.Join(chunks[1:], "\n"+indent)
	}

	const keyW = 9
	valW := width - keyW - 1
	if valW < 10 {
		valW = 10
	}

	dKey := detailKeyStyle()
	dSec := detailSectionStyle()

	b.WriteString(dSec.Render("RESOURCE") + "\n\n")

	fields := []struct{ k, v string }{
		{"Service", r.Service},
		{"Type", r.Type},
		{"Region", r.Region},
		{"Account", r.AccountID},
		{"ID", r.ID},
		{"Name", r.Name},
		{"State", r.State},
	}
	for _, f := range fields {
		if f.v != "" {
			b.WriteString(dKey.Render(fmt.Sprintf("%-9s", f.k)) + " " +
				fieldVal(f.v, keyW, valW) + "\n")
		}
	}

	if r.ARN != "" {
		arnW := width - 2
		if arnW < 10 {
			arnW = 10
		}
		b.WriteString(dKey.Render("ARN") + "\n")
		for _, chunk := range chunkString(r.ARN, arnW) {
			b.WriteString("  " + chunk + "\n")
		}
	}

	if r.CreatedAt != nil {
		b.WriteString(dKey.Render("Created") + "  " +
			r.CreatedAt.Format("2006-01-02 15:04:05") + "\n")
	}

	if len(r.Summary) > 0 {
		b.WriteString("\n" + dSec.Render("SUMMARY") + "\n\n")
		keys := make([]string, 0, len(r.Summary))
		for k := range r.Summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		const sumKeyW = 20
		sumValW := width - sumKeyW - 1
		if sumValW < 10 {
			sumValW = 10
		}
		for _, k := range keys {
			b.WriteString(dKey.Render(fmt.Sprintf("%-20s", k)) + " " +
				fieldVal(r.Summary[k], sumKeyW, sumValW) + "\n")
		}
	}

	if len(r.Tags) > 0 {
		b.WriteString("\n" + dSec.Render("TAGS") + "\n\n")
		keys := make([]string, 0, len(r.Tags))
		for k := range r.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		const tagKeyW = 20
		tagValW := width - tagKeyW - 1
		if tagValW < 10 {
			tagValW = 10
		}
		for _, k := range keys {
			b.WriteString(dKey.Render(fmt.Sprintf("%-20s", k)) + " " +
				fieldVal(r.Tags[k], tagKeyW, tagValW) + "\n")
		}
	}

	return b.String()
}

// ── Error renderer ────────────────────────────────────────────────────────────

func (m tuiModel) renderPrivilegeErrors() string {
	var authErrs, otherErrs []model.ExploreError
	for _, e := range m.errors {
		if e.Code == "AccessDenied" {
			authErrs = append(authErrs, e)
		} else {
			otherErrs = append(otherErrs, e)
		}
	}

	var b strings.Builder
	if len(authErrs) > 0 {
		b.WriteString(privilegeTitleStyle().Render("INSUFFICIENT PRIVILEGES") + "\n\n")
		for _, e := range authErrs {
			b.WriteString(detailKeyStyle().Render(fmt.Sprintf("%-12s", "Service")) +
				" " + strings.ToUpper(e.Service) + " (" + e.Region + ")\n")
			b.WriteString(privilegeHintStyle().Render(e.Message) + "\n\n")
		}
		b.WriteString("Attach the missing permissions to your IAM user or role.\n")
	}
	if len(otherErrs) > 0 {
		if len(authErrs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Other errors:\n")
		for _, e := range otherErrs {
			b.WriteString(fmt.Sprintf("  [%s|%s] %s: %s\n", e.Service, e.Region, e.Code, e.Message))
		}
	}
	return privilegeErrorStyle().Render(b.String())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func waitForChunk(chunks <-chan model.ResultChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-chunks
		if !ok {
			return doneMsg{}
		}
		return chunkMsg(chunk)
	}
}

func chunkString(s string, n int) []string {
	if n <= 0 || len(s) <= n {
		return []string{s}
	}
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

