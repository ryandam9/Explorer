package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/summary"
	"github.com/user/aws_explorer/internal/table"
	"github.com/user/aws_explorer/internal/ui"
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
	sidebarInner  = 16 // inner content width of the sidebar panel
	detailInner   = 34 // inner content width of the detail panel
	minTableInner = 40 // table panel keeps at least this much content width
)

// ── Zone IDs ─────────────────────────────────────────────────────────────────

const zoneSvc = "svc-"

// ── Message types ─────────────────────────────────────────────────────────────

type chunkMsg model.ResultChunk
type doneMsg struct{}
type clearToastMsg struct{}

// ── Styles (theme-aware; resolved at render time via color accessors) ─────────

func detailKeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorText()))
}
func detailSectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Underline(true)
}
func privilegeErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorError())).
		Padding(0, 1)
}
func privilegeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorError()))
}
func privilegeHintStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
}

// ── Model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	ctx    context.Context
	engine *engine.Engine
	chunks chan model.ResultChunk

	// Data
	seed    []model.Resource // pre-fetched resources (e.g. the all-services sweep) merged with streamed results
	results []model.Resource
	sorted  []model.Resource // results sorted by service+name; rebuilt only when results change
	errors  []model.ExploreError
	loading bool
	done    bool

	// Terminal size
	width  int
	height int

	// Service sidebar
	services      []string
	activeService int

	// Resource table (shared themed table with horizontal column scrolling).
	// allRows / allRes are parallel: allRes[svc][i] is the resource shown in
	// allRows[svc][i], so the cursor maps straight to a resource.
	table   table.Model
	allRows map[string][]table.Row
	allRes  map[string][]model.Resource

	// Quick text filter ("/"): matches any cell of a row.
	filtering   bool
	filterInput textinput.Model
	filterText  string

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

	// Help & settings overlays
	showHelp     bool
	showSettings bool
	settings     ui.SettingsModel

	// Config (path + loaded struct) needed to (re)build the settings panel.
	configPath string
	cfg        *config.Config
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewModel creates the TUI model.  configPath is the path to the YAML config
// file on disk (used by the settings panel to persist changes); cfg is the
// already-loaded config struct.
func NewModel(ctx context.Context, eng *engine.Engine, configPath string, cfg *config.Config) tea.Model {
	return NewModelWithSeed(ctx, eng, configPath, cfg, nil)
}

// NewModelWithSeed is like NewModel but pre-populates the table with seed
// resources (e.g. the all-services Tagging API sweep) which are merged and
// deduplicated with the resources streamed from the engine's typed collectors.
func NewModelWithSeed(ctx context.Context, eng *engine.Engine, configPath string, cfg *config.Config, seed []model.Resource) tea.Model {
	chunks := make(chan model.ResultChunk, 64)
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	zoneM := zone.New()

	m := tuiModel{
		ctx:        ctx,
		engine:     eng,
		seed:       seed,
		loading:    true,
		chunks:     chunks,
		focus:      focusTable,
		spinner:    sp,
		zones:      zoneM,
		allRows:    make(map[string][]table.Row),
		allRes:     make(map[string][]model.Resource),
		configPath: configPath,
		cfg:        cfg,
	}
	m.settings = ui.NewSettingsModel(0, 0, configPath, cfg)

	m.filterInput = textinput.New()
	m.filterInput.Placeholder = "Filter resources..."
	m.filterInput.CharLimit = 128
	m.filterInput.Width = 32
	m.filterInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.filterInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.filterInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.filterInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.table = table.New(
		table.WithColumns(m.columns()),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithStyles(ui.TableStyles()),
	)

	// Surface seed resources immediately; typed results stream in and merge.
	if len(seed) > 0 {
		m.onResultsChanged()
	}
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
			if msg.String() == "esc" && !m.settings.EditMode() {
				m.showSettings = false
				return m, nil
			}
		case ui.SettingsSavedMsg:
			m.showSettings = false
			m.setToast("Theme saved: " + msg.Theme)
			cmds = append(cmds, toastCmd(3*time.Second))
			// Restyle the table to pick up the new theme colors.
			m.table.SetStyles(ui.TableStyles())
			return m, tea.Batch(cmds...)
		case ui.SettingsErrMsg:
			m.showSettings = false
			m.setToast("Save failed: " + msg.Err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
			return m, tea.Batch(cmds...)
		}
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	// Swallow keys while the help overlay is open; Esc or ? closes it.
	if m.showHelp {
		if key, ok := msg.(tea.KeyMsg); ok {
			if s := key.String(); s == "esc" || s == ui.KeyHelp || s == "q" {
				m.showHelp = false
			}
		}
		return m, nil
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

	// Route keys to the quick-filter input while it is active.
	if m.filtering {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "enter":
				m.filtering = false
				m.filterInput.Blur()
				m.syncTableLayout()
				return m, nil
			case "esc":
				m.filtering = false
				m.filterInput.Blur()
				m.filterInput.SetValue("")
				m.filterText = ""
				m.rebuildAllRows()
				m.syncTableLayout()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.filterText = m.filterInput.Value()
				m.rebuildAllRows()
				m.updateTableRows()
				return m, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncTableLayout()
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
				m.table.Focus()
				m.syncTableLayout()
			}
			return m, tea.Batch(cmds...)

		case "tab":
			m.cycleFocus(1)
			return m, tea.Batch(cmds...)

		case "shift+tab":
			m.cycleFocus(-1)
			return m, tea.Batch(cmds...)

		case "enter":
			switch m.focus {
			case focusSidebar:
				m.focus = focusTable
				m.table.Focus()
			case focusTable:
				if res, ok := m.selectedResource(); ok {
					m.detail = &res
					m.showDetail = true
					m.focus = focusDetail
					m.table.Blur()
					m.syncTableLayout()
					m.syncDetailViewport()
				}
			}
			return m, tea.Batch(cmds...)

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

		case ">", ".":
			if m.focus == focusTable {
				m.table.ScrollRight()
				return m, nil
			}

		case "<", ",":
			if m.focus == focusTable {
				m.table.ScrollLeft()
				return m, nil
			}

		case "/":
			if m.focus == focusTable && len(m.sorted) > 0 {
				m.filtering = true
				m.filterInput.Focus()
				m.syncTableLayout()
				return m, nil
			}

		case "f":
			if !m.showFilter && len(m.sorted) > 0 {
				m.filterForm = m.buildFilterForm()
				m.showFilter = true
				cmds = append(cmds, m.filterForm.Init())
			}
			return m, tea.Batch(cmds...)

		case "r":
			m.filterRegion = ""
			m.filterState = ""
			m.filterText = ""
			m.filterInput.SetValue("")
			m.rebuildAllRows()
			m.syncTableLayout()
			m.setToast("Filters cleared")
			cmds = append(cmds, toastCmd(3*time.Second))
			return m, tea.Batch(cmds...)

		case ui.KeySettings:
			m.settings = ui.NewSettingsModel(m.width, m.height, m.configPath, m.cfg)
			m.showSettings = true
			return m, tea.Batch(cmds...)

		case ui.KeyHelp:
			m.showHelp = true
			return m, tea.Batch(cmds...)
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
				m.table.Focus()
				break
			}
		}

	case chunkMsg:
		m.loading = false
		m.results = append(m.results, msg.Resources...)
		m.errors = append(m.errors, msg.Errors...)
		m.onResultsChanged()
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

	// Forward remaining events (navigation keys) to the table when it has focus.
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
	if m.focus == focusTable {
		m.table.Focus()
	} else {
		m.table.Blur()
	}
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

// matchesTextFilter reports whether any of the row's cells contains the quick
// filter text. query must already be lower-cased; matching is case-insensitive.
func matchesTextFilter(query string, cells ...string) bool {
	if query == "" {
		return true
	}
	for _, c := range cells {
		if strings.Contains(strings.ToLower(c), query) {
			return true
		}
	}
	return false
}

// onResultsChanged re-derives the service list and the sorted view after new
// chunks arrive, then rebuilds the visible rows. Filter changes alone only
// need rebuildAllRows, which reuses the cached sorted slice.
func (m *tuiModel) onResultsChanged() {
	// Merge the pre-fetched seed (all-services sweep) with the streamed typed
	// results, deduping by ARN so the richer typed entry wins.
	combined := summary.Dedupe(append(append([]model.Resource{}, m.seed...), m.results...))

	// Rebuild service list.
	svcSet := map[string]bool{}
	for _, r := range combined {
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

	// Sort the combined resources (by service, then name) so the table renders
	// in a stable, grouped order regardless of arrival order.
	m.sorted = combined
	sort.SliceStable(m.sorted, func(i, j int) bool {
		if m.sorted[i].Service != m.sorted[j].Service {
			return m.sorted[i].Service < m.sorted[j].Service
		}
		return m.sorted[i].Name < m.sorted[j].Name
	})

	m.rebuildAllRows()
}

func (m *tuiModel) rebuildAllRows() {
	// Build rows grouped by service, applying the structured filters and the
	// quick text filter.
	query := strings.ToLower(m.filterText)
	m.allRows = make(map[string][]table.Row, len(m.services))
	m.allRes = make(map[string][]model.Resource, len(m.services))
	for _, r := range m.sorted {
		if m.filterRegion != "" && r.Region != m.filterRegion {
			continue
		}
		if m.filterState != "" && r.State != m.filterState {
			continue
		}
		if !matchesTextFilter(query, r.Service, r.Type, r.Region, r.ID, r.Name, r.State) {
			continue
		}
		for _, key := range []string{"All", r.Service} {
			row := table.Row{
				fmt.Sprintf("%d", len(m.allRows[key])+1),
				r.Service, r.Type, r.Region, r.ID, r.Name, r.State,
			}
			m.allRows[key] = append(m.allRows[key], row)
			m.allRes[key] = append(m.allRes[key], r)
		}
	}
}

func (m tuiModel) currentService() string {
	if len(m.services) == 0 || m.activeService >= len(m.services) {
		return "All"
	}
	return m.services[m.activeService]
}

// selectedResource returns the resource under the table cursor.
func (m tuiModel) selectedResource() (model.Resource, bool) {
	res := m.allRes[m.currentService()]
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(res) {
		return model.Resource{}, false
	}
	return res[idx], true
}

func (m *tuiModel) updateTableRows() {
	m.table.SetRows(m.allRows[m.currentService()])
}

// ── Table layout ──────────────────────────────────────────────────────────────

// columns returns the table columns sized for the current width. ID and Name
// flex to fill leftover space; when the panel is too narrow for everything,
// the table scrolls horizontally instead of truncating columns away.
func (m tuiModel) columns() []table.Column {
	// Fixed widths: #(4) Service(10) Type(12) Region(13) State(10) = 49,
	// plus each column's cell padding (2 × 7 columns).
	const fixed = 4 + 10 + 12 + 13 + 10
	const padding = 2 * 7
	rem := m.tableInnerWidth() - fixed - padding
	idW := rem * 2 / 5
	nameW := rem - idW
	if idW < 10 {
		idW = 10
	}
	if nameW < 10 {
		nameW = 10
	}

	return []table.Column{
		{Title: "#", Width: 4},
		{Title: "Service", Width: 10},
		{Title: "Type", Width: 12},
		{Title: "Region", Width: 13},
		{Title: "ID", Width: idW},
		{Title: "Name", Width: nameW},
		{Title: "State", Width: 10},
	}
}

// detailInline reports whether the detail panel fits beside the sidebar and
// table. On narrower terminals it is drawn as a centered overlay instead, so
// body lines never exceed the terminal width (the terminal would wrap them
// and scroll the header off screen).
func (m tuiModel) detailInline() bool {
	return m.width >= (sidebarInner+4)+(minTableInner+6)+(detailInner+4)
}

// tableInnerWidth is the content width inside the table panel: terminal width
// minus the sidebar panel, the table panel's own border + padding, and the
// detail panel when shown inline.
func (m tuiModel) tableInnerWidth() int {
	sidebarOuter := sidebarInner + 4
	w := m.width - sidebarOuter - 2 - 4
	if m.showDetail && m.detailInline() {
		w -= detailInner + 4
	}
	if w < minTableInner {
		return minTableInner
	}
	return w
}

func (m tuiModel) tableHeight() int {
	// Total frame budget: header text(1) + header margin(1) + panel
	// borders(2) + the +2 added back by syncTableLayout/renderTablePanel +
	// status bar(1) = 7. Anything less tall than the terminal and Bubble Tea
	// trims the frame from the top, hiding the header.
	h := m.height - 7
	if h < 8 {
		return 8
	}
	return h
}

// syncTableLayout resizes the table to the current terminal dimensions.
func (m *tuiModel) syncTableLayout() {
	inner := m.tableInnerWidth()
	tableH := m.tableHeight() + 2
	if m.filtering || m.filterText != "" {
		tableH-- // the filter line sits under the table inside the panel
	}
	m.table.SetColumns(m.columns())
	m.table.SetWidth(inner)
	m.table.SetHeight(max(tableH, 4))
	// SetColumns resets the horizontal scroll; keep the row set in sync.
	m.updateTableRows()
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
	// Preserve scroll position across resizes so the user doesn't jump back
	// to the top when the terminal is resized while reading the detail panel.
	savedOffset := m.detailViewport.YOffset
	m.detailViewport = viewport.New(vpWidth, vpHeight)
	m.detailViewport.SetContent(m.renderDetail(*m.detail, vpWidth))
	if savedOffset > 0 {
		m.detailViewport.SetYOffset(savedOffset)
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

// helpView renders the keybinding help overlay for the summary explorer,
// using the shared themed renderer so it matches the other browsers' help.
func (m tuiModel) helpView() string {
	body := lipgloss.JoinVertical(lipgloss.Left,
		"Navigation",
		"  ↑/↓, [ ]           Move selection / scroll detail",
		"  < >                Scroll table columns (when more columns than fit)",
		"  Tab / Shift+Tab    Switch panel focus",
		"  Enter              Select service / open detail",
		"  Esc                Close detail or overlay",
		"",
		"Resources",
		"  /                  Quick text filter",
		"  f                  Advanced filter (region / state)",
		"  r                  Reset all filters",
		"",
		"Utility",
		"  S                  Settings (theme & colors)",
		"  ?                  Toggle this help",
		"  q, Ctrl+C          Quit",
	)
	w := m.width - 12
	if w > 72 {
		w = 72
	}
	if w < 32 {
		w = 32
	}
	return ui.HelpView("AWS Explorer Help", body, w)
}

func (m tuiModel) View() string {
	var output string

	if m.loading && len(m.sorted) == 0 {
		output = fmt.Sprintf("\n  %s  Loading AWS resources…\n", m.spinner.View())
		return m.zones.Scan(output)
	}

	if len(m.errors) > 0 && len(m.sorted) == 0 && m.done {
		output = m.renderPrivilegeErrors() + "\n\nPress q to quit."
		return m.zones.Scan(output)
	}

	header := m.renderHeader()
	status := m.statusBar()

	if m.showHelp {
		centered := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, m.helpView())
		output = lipgloss.JoinVertical(lipgloss.Left, header, centered, status)
	} else if m.showSettings {
		settingsView := m.settings.View()
		centered := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, settingsView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, centered, status)
	} else if m.showFilter && m.filterForm != nil {
		formW := 52
		formH := 14
		formView := ui.ModalStyle(formW, formH).Render(m.filterForm.View())
		modal := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, formView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, modal, status)
	} else {
		body := m.renderBody()
		output = lipgloss.JoinVertical(lipgloss.Left, header, body, status)
	}

	// Overlay the toast at top-right if active.
	if m.toast != "" && time.Now().Before(m.toastExp) {
		toastRendered := lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorHighlightText())).
			Background(lipgloss.Color(ui.ColorHighlight())).
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

	// Hard-clip the final frame to the terminal dimensions. If the rendered
	// output is taller than the terminal, Bubble Tea cuts lines from the TOP,
	// which hides the header. If a line is wider than the terminal it wraps
	// and adds a phantom row, pushing subsequent rows down.
	if m.width > 0 && m.height > 0 {
		output = ui.ClipToSize(output, m.width, m.height)
	}
	return m.zones.Scan(output)
}

func (m tuiModel) renderHeader() string {
	status := "streaming"
	if m.done {
		status = "complete"
	} else if m.loading && len(m.sorted) == 0 {
		status = m.spinner.View() + " loading"
	}

	var filterParts []string
	if m.filterRegion != "" {
		filterParts = append(filterParts, "region:"+m.filterRegion)
	}
	if m.filterState != "" {
		filterParts = append(filterParts, "state:"+m.filterState)
	}
	if m.filterText != "" {
		filterParts = append(filterParts, "text:"+m.filterText)
	}
	filterInfo := ""
	if len(filterParts) > 0 {
		filterInfo = "  [" + strings.Join(filterParts, ", ") + "]"
	}

	title := fmt.Sprintf("  AWS Explorer  ·  %s  ·  %d resources  ·  %d errors%s",
		status, len(m.sorted), len(m.errors), filterInfo)

	w := m.width - 2
	if w < 10 {
		w = 10
	}
	// Truncate instead of letting lipgloss wrap: a wrapped header makes the
	// frame taller than the terminal and the top of the screen scrolls away.
	title = ansi.Truncate(title, w-2, "…") // -2 for the style's padding
	return ui.HeaderStyle().Width(w).Render(title)
}

func (m tuiModel) renderBody() string {
	sidebar := m.renderSidebar()
	tbl := m.renderTablePanel()
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, tbl)
	if m.showDetail && m.detail != nil {
		detail := m.renderDetailPanel()
		if m.detailInline() {
			return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, tbl, detail)
		}
		// Narrow terminal: float the detail panel centered over the body
		// instead of widening the layout past the terminal edge.
		return lipgloss.Place(lipgloss.Width(body), lipgloss.Height(body),
			lipgloss.Center, lipgloss.Center, detail)
	}
	return body
}

func (m tuiModel) renderSidebar() string {
	var b strings.Builder
	b.WriteString(ui.PanelTitleStyle().Render("SERVICES") + "\n\n")

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Background(lipgloss.Color(ui.ColorHighlight())).
		Width(sidebarInner - 2)
	idleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorText())).
		Width(sidebarInner - 2)

	for i, svc := range m.services {
		zID := fmt.Sprintf("%s%d", zoneSvc, i)
		label := svc
		if len(label) > sidebarInner-3 {
			label = label[:sidebarInner-4] + "…"
		}
		var line string
		if i == m.activeService {
			line = activeStyle.Render("▶ " + label)
		} else {
			line = idleStyle.Render("  " + label)
		}
		b.WriteString(m.zones.Mark(zID, line) + "\n")
	}

	style := ui.PanelStyle()
	if m.focus == focusSidebar {
		style = ui.SelectedPanelStyle()
	}
	h := m.tableHeight() + 2
	if h < 6 {
		h = 6
	}
	return style.Width(sidebarInner).Height(h).Render(b.String())
}

func (m tuiModel) renderTablePanel() string {
	inner := m.tableInnerWidth()
	innerH := m.tableHeight() + 2

	filterActive := m.filterText != "" || m.filterRegion != "" || m.filterState != ""
	tableView := m.table.View()

	// When a filter is active but nothing matched, replace the empty table
	// body with a helpful message so the user isn't staring at a blank panel.
	if filterActive && len(m.allRows[m.currentService()]) == 0 {
		hint := lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("  No resources match current filter  •  press r to reset")
		tableView = lipgloss.JoinVertical(lipgloss.Left, tableView, hint)
	}

	parts := []string{tableView}
	if m.filtering || m.filterText != "" {
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorMuted())).Render("Filter: ")+
			m.filterInput.View())
	}
	if ind := ui.TableScrollIndicator(&m.table); ind != "" {
		parts[0] = lipgloss.JoinVertical(lipgloss.Left, parts[0], ind)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return ui.TablePanelStyle(m.focus == focusTable).
		Width(inner).
		Height(innerH).
		MaxHeight(innerH + 2).
		Render(content)
}

func (m tuiModel) renderDetailPanel() string {
	style := ui.PanelStyle()
	if m.focus == focusDetail {
		style = ui.SelectedPanelStyle()
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
		svc, len(m.sorted), len(m.errors), status)

	return ui.StatusBar(w, left, m.statusHints())
}

// statusHints returns only the shortcuts usable right now, given the open
// overlay and panel focus.
func (m tuiModel) statusHints() []ui.KeyHint {
	switch {
	case m.showSettings:
		// The settings panel renders its own hint bar.
		return nil
	case m.showHelp:
		return []ui.KeyHint{ui.H("?/Esc", "close help")}
	case m.showFilter:
		return []ui.KeyHint{
			ui.H("↑/↓", "choose"),
			ui.H("Enter", "apply"),
			ui.H("Esc", "cancel"),
		}
	case m.filtering:
		return []ui.KeyHint{
			ui.H("type", "to filter"),
			ui.H("Enter", "keep filter"),
			ui.H("Esc", "clear"),
		}
	}

	switch m.focus {
	case focusSidebar:
		return []ui.KeyHint{
			ui.H("↑/↓", "service"),
			ui.H("Enter", "select"),
			ui.H("Tab", "panel"),
			ui.H("S", "theme"),
			ui.H("q", "quit"),
			ui.H("?", "help"),
		}
	case focusTable:
		hints := []ui.KeyHint{
			ui.H("↑/↓", "navigate"),
			ui.H("Enter", "detail"),
		}
		if l, r := m.table.ColScrollInfo(); l+r > 0 {
			hints = append(hints, ui.H("</>", fmt.Sprintf("cols (%d more)", l+r)))
		}
		hints = append(hints,
			ui.H("/", "filter"),
			ui.H("f", "adv filter"),
		)
		if m.filterRegion != "" || m.filterState != "" || m.filterText != "" {
			hints = append(hints, ui.H("r", "reset filters"))
		}
		return append(hints,
			ui.H("Tab", "panel"),
			ui.H("S", "theme"),
			ui.H("q", "quit"),
			ui.H("?", "help"),
		)
	case focusDetail:
		return []ui.KeyHint{
			ui.H("↑/↓ or [ ]", "scroll"),
			ui.H("Esc", "close"),
			ui.H("Tab", "panel"),
			ui.H("q", "quit"),
			ui.H("?", "help"),
		}
	}
	return []ui.KeyHint{ui.H("?", "help")}
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
		{"AZ", r.AZ},
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
