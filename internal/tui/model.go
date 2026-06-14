package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/ryandam9/aws_explorer/internal/acctsnap"
	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/debuglog"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/loggroup"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/sparkline"
	"github.com/ryandam9/aws_explorer/internal/summary"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/trail"
	"github.com/ryandam9/aws_explorer/internal/ui"
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
	sidebarInner  = 20 // inner content width of the sidebar panel (name + count)
	detailInner   = 34 // inner content width of the detail panel
	minTableInner = 40 // table panel keeps at least this much content width
	// tablePanelHPad is the table panel's horizontal padding (Padding(0,1) =>
	// 1 left + 1 right). The table content must be sized this much narrower than
	// the panel's Width, or lipgloss wraps the last columns onto a new line.
	tablePanelHPad = 2
)

// ── Zone IDs ─────────────────────────────────────────────────────────────────

const (
	zoneSvc = "svc-"
	zoneRow = "row-"
)

// ── Message types ─────────────────────────────────────────────────────────────

// chunkMsg and doneMsg carry the scan generation they belong to, so chunks
// from a scan that was cancelled (profile/region switch) are ignored when
// they straggle in after the restart.
type chunkMsg struct {
	gen   int
	chunk model.ResultChunk
}
type doneMsg struct{ gen int }
type clearToastMsg struct{}

// engineSwitchedMsg reports the result of rebuilding the engine for a new
// profile/region selection.
type engineSwitchedMsg struct {
	eng     *engine.Engine
	profile string
	region  string
	err     error
}

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

	// Scan lifecycle: scanCtx bounds the running StreamRun goroutine so a
	// profile/region switch can cancel it; scanGen tags chunk messages so
	// stragglers from a cancelled scan are dropped.
	scanCtx    context.Context
	scanCancel context.CancelFunc
	scanGen    int

	// Data. sorted is the single store of every resource (seed + streamed),
	// deduped by ARN and kept sorted by service+name: incoming chunks are
	// merged in (O(n+k)) instead of re-sorting the world on every arrival.
	sorted     []model.Resource
	searchText []string       // searchText[i] is sorted[i]'s filterable cells, pre-joined and lower-cased
	byARN      map[string]int // ARN -> index into sorted, for richer-entry dedupe (see summary.Dedupe)
	svcSet     map[string]bool
	svcTotals  map[string]int // unfiltered resource count per service (and "All"), for the filter match indicator
	svcErrs    map[string]int // error count per service, for the sidebar badges
	errors     []model.ExploreError
	loading    bool
	done       bool

	// Scan progress: planned task keys ("service@region") not yet finished.
	tasksPending map[string]bool
	tasksTotal   int
	tasksDone    int

	// Column sorting: s cycles the column, R flips the direction.
	// sortCol -1 is the natural service+name order.
	sortCol int
	sortAsc bool

	// Raw JSON detail view ("J" in the detail panel).
	detailRaw bool

	// Profile / region switcher overlay ("P").
	showSwitcher bool
	switcherForm *huh.Form
	switching    bool

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

	// Global fuzzy finder ("Ctrl+P"): jump to any resource (see finder.go).
	showFinder  bool
	finderInput textinput.Model
	finderHits  []int // indices into sorted, best match first
	finderSel   int

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
	helpViewport viewport.Model // scrolls the help body when it is taller than the screen
	showSettings bool
	settings     ui.SettingsModel

	// Errors overlay: lists access-denied / collection errors so they can be
	// read even when some resources were returned. Scrollable when long.
	showErrors     bool
	errorsViewport viewport.Model

	// Debug overlay ("~"): a live view of the captured scan activity log, so
	// the user can see what the tool is doing — which regions, services and
	// API calls are in flight — instead of staring at a blank screen.
	showDebug     bool
	debugViewport viewport.Model

	// Account snapshot diff ("d"): what changed since the saved baseline
	// (internal/acctsnap). First press saves a baseline; later presses show
	// the diff overlay, where b re-baselines.
	showAcctDiff bool
	acctDiffVP   viewport.Model
	acctDiffRep  acctsnap.Report

	// Config (path + loaded struct) needed to (re)build the settings panel.
	configPath string
	cfg        *config.Config

	// Cloud Support & Debugging (Feature 1, 3, 7, 8)
	showTimeline    bool
	timelineLoading bool
	timelineEvents  []trail.Event
	timelineErr     error

	showLogs    bool
	logsLoading bool
	logsLines   []string
	logsErr     error

	showMetrics    bool
	metricsLoading bool
	metricsData    *awsutil.SparklineMetric
	metricsErr     error

	watchMode    bool
	watchTimerID int
	prevStates   map[string]string
	changedRows  map[string]time.Time
	watchSeen    map[string]bool // keys seen during the in-flight watch refresh

	showXref      bool
	xrefResources []model.Resource
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

	scanCtx, scanCancel := context.WithCancel(ctx)
	m := tuiModel{
		ctx:        ctx,
		engine:     eng,
		scanCtx:    scanCtx,
		scanCancel: scanCancel,
		loading:    true,
		chunks:     chunks,
		focus:      focusTable,
		spinner:    sp,
		zones:      zoneM,
		byARN:      make(map[string]int),
		svcSet:     make(map[string]bool),
		svcTotals:  make(map[string]int),
		svcErrs:    make(map[string]int),
		allRows:    make(map[string][]table.Row),
		allRes:     make(map[string][]model.Resource),
		sortCol:    -1,
		sortAsc:    true,
		configPath: configPath,
		cfg:        cfg,
	}
	m.resetTaskProgress()
	m.settings = ui.NewSettingsModel(0, 0, configPath, cfg)

	m.filterInput = textinput.New()
	m.filterInput.Placeholder = "Filter resources…"
	m.filterInput.CharLimit = 128
	m.filterInput.Width = 32
	m.filterInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.filterInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.filterInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.filterInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.finderInput = textinput.New()
	m.finderInput.Placeholder = "Search every resource — name, ID, ARN, type…"
	m.finderInput.CharLimit = 128
	m.finderInput.Width = 48
	m.finderInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.finderInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.finderInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.finderInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.table = table.New(
		table.WithColumns(m.columns()),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithStyles(ui.TableStyles()),
	)
	// Mark every visible row with a mouse zone so clicks can select it.
	m.table.MarkRow = func(i int, rendered string) string {
		return zoneM.Mark(fmt.Sprintf("%s%d", zoneRow, i), rendered)
	}

	// Surface seed resources immediately; typed results stream in and merge.
	if len(seed) > 0 {
		m.mergeResources(seed)
		m.onResultsChanged()
	}
	return m
}

func (m tuiModel) Init() tea.Cmd {
	if m.engine == nil {
		// Offline snapshot view: no scan to run, the seed is the inventory.
		close(m.chunks)
		return tea.Batch(waitForChunk(m.chunks, m.scanGen), m.spinner.Tick)
	}
	go m.engine.StreamRun(m.scanCtx, m.chunks)
	return tea.Batch(waitForChunk(m.chunks, m.scanGen), m.spinner.Tick)
}

// resetTaskProgress (re)derives the planned task set from the engine so the
// header can show real done/total scan progress.
func (m *tuiModel) resetTaskProgress() {
	m.tasksPending = make(map[string]bool)
	m.tasksDone = 0
	m.tasksTotal = 0
	if m.engine == nil {
		return
	}
	keys := m.engine.PlannedTaskKeys()
	m.tasksTotal = len(keys)
	for _, k := range keys {
		m.tasksPending[k] = true
	}
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

	// While the help overlay is open, scroll the reference and close on
	// Esc/?/q. Only input is intercepted; scan messages fall through so an
	// in-progress scan keeps collecting underneath.
	if m.showHelp {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", ui.KeyHelp, "q":
				m.showHelp = false
			case "up", "k", "[":
				m.helpViewport.LineUp(3)
			case "down", "j", "]":
				m.helpViewport.LineDown(3)
			case "g":
				m.helpViewport.GotoTop()
			case "G":
				m.helpViewport.GotoBottom()
			}
			return m, nil
		case tea.MouseMsg:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.helpViewport.LineUp(3)
			case tea.MouseButtonWheelDown:
				m.helpViewport.LineDown(3)
			}
			return m, nil
		}
		// Non-input messages fall through to the normal update path below.
	}

	// While the debug overlay is open, intercept only key and mouse events
	// (to scroll the pane or close it); every other message — scan chunks,
	// completion, spinner ticks — must fall through to the normal update path
	// so the scan keeps progressing underneath. The scan is a pull loop where
	// each chunk re-issues waitForChunk, so swallowing a chunkMsg here would
	// stall collection and freeze the inventory.
	if m.showDebug {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", ui.KeyDebug, "q":
				m.showDebug = false
			case "up", "k", "[":
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.LineUp(3)
			case "down", "j", "]":
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.LineDown(3)
			case "g":
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.GotoTop()
			case "G":
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.GotoBottom()
			}
			// Swallow every key while the pane is open so it never leaks to
			// the table/sidebar underneath.
			return m, nil
		case tea.MouseMsg:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.LineUp(3)
			case tea.MouseButtonWheelDown:
				m.debugViewport.SetContent(m.debugBody())
				m.debugViewport.LineDown(3)
			}
			return m, nil
		}
		// Non-input messages fall through to the normal update path below.
	}

	// While the errors overlay is open, allow scrolling and close on Esc/e/q.
	// Intercept only key/mouse input; let scan messages fall through so an
	// in-progress scan keeps collecting underneath (see the debug overlay note
	// above: swallowing a chunkMsg here would stall the pull loop).
	if m.showErrors {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "e", "q":
				m.showErrors = false
			case "up", "k", "[":
				m.errorsViewport.LineUp(3)
			case "down", "j", "]":
				m.errorsViewport.LineDown(3)
			}
			return m, nil
		case tea.MouseMsg:
			// Wheel scrolls the overlay's viewport.
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.errorsViewport.LineUp(3)
			case tea.MouseButtonWheelDown:
				m.errorsViewport.LineDown(3)
			}
			return m, nil
		}
		// Non-input messages fall through to the normal update path below.
	}

	// While the account-diff overlay is open: scroll, b to re-baseline,
	// Esc/d/q to close. As with the other overlays, only input is intercepted
	// so a running scan keeps progressing underneath.
	if m.showAcctDiff {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "D", "q":
				m.showAcctDiff = false
			case "up", "k", "[":
				m.acctDiffVP.LineUp(3)
			case "down", "j", "]":
				m.acctDiffVP.LineDown(3)
			case "b":
				m.showAcctDiff = false
				m.saveAcctBaseline()
				return m, toastCmd(4 * time.Second)
			}
			return m, nil
		case tea.MouseMsg:
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.acctDiffVP.LineUp(3)
			case tea.MouseButtonWheelDown:
				m.acctDiffVP.LineDown(3)
			}
			return m, nil
		}
		// Non-input messages fall through to the normal update path below.
	}

	// Route all events to the profile/region switcher form when it is open.
	if m.showSwitcher && m.switcherForm != nil {
		newForm, formCmd := m.switcherForm.Update(msg)
		if f, ok := newForm.(*huh.Form); ok {
			m.switcherForm = f
		}
		cmds = append(cmds, formCmd)

		switch m.switcherForm.State {
		case huh.StateCompleted:
			m.showSwitcher = false
			cmds = append(cmds, m.applySwitcher(
				m.switcherForm.GetString("profile"),
				m.switcherForm.GetString("region")))
		case huh.StateAborted:
			m.showSwitcher = false
		}
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
			m.invalidateRows()
			m.updateTableRows()
			m.setToast("Filter applied")
			cmds = append(cmds, toastCmd(3*time.Second))
		case huh.StateAborted:
			m.showFilter = false
		}
		return m, tea.Batch(cmds...)
	}

	// Route keys to the global finder while it is open. Non-key messages
	// (scan chunks, ticks) fall through so the stream keeps flowing behind
	// the palette.
	if m.showFinder {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc", "ctrl+p":
				m.closeFinder()
				return m, nil
			case "enter":
				if m.finderSel >= 0 && m.finderSel < len(m.finderHits) {
					idx := m.finderHits[m.finderSel]
					m.closeFinder()
					m.jumpToResource(idx)
				} else {
					m.closeFinder()
				}
				return m, nil
			case "up", "ctrl+k":
				if m.finderSel > 0 {
					m.finderSel--
				}
				return m, nil
			case "down", "ctrl+j":
				if m.finderSel < len(m.finderHits)-1 {
					m.finderSel++
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.finderInput, cmd = m.finderInput.Update(key)
				m.computeFinderHits()
				return m, cmd
			}
		}
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
				m.invalidateRows()
				m.syncTableLayout()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.filterText = m.filterInput.Value()
				m.invalidateRows()
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
				m.showTimeline = false
				m.showLogs = false
				m.showMetrics = false
				m.showXref = false
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
					m.showTimeline = false
					m.showLogs = false
					m.showMetrics = false
					m.showXref = false
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
			m.invalidateRows()
			m.syncTableLayout()
			m.setToast("Filters cleared")
			cmds = append(cmds, toastCmd(3*time.Second))
			return m, tea.Batch(cmds...)

		case "s":
			if m.focus == focusTable && len(m.sorted) > 0 {
				// Cycle: natural order → Service → … → State → natural.
				m.sortCol++
				if m.sortCol == 0 { // skip the row-number column
					m.sortCol = 1
				}
				if m.sortCol >= len(m.columns()) {
					m.sortCol = -1
				}
				m.invalidateRows()
				m.syncTableLayout()
			}
			return m, tea.Batch(cmds...)

		case "R":
			if m.focus == focusTable && m.sortCol > 0 {
				m.sortAsc = !m.sortAsc
				m.invalidateRows()
				m.syncTableLayout()
			}
			return m, tea.Batch(cmds...)

		case "y", "Y":
			res, ok := m.selectedResource()
			if m.showDetail && m.detail != nil {
				// The open detail is the source of truth; the cursor may
				// have moved under it (e.g. a filter edit).
				res, ok = *m.detail, true
			}
			if ok {
				text, what := res.ARN, "ARN"
				switch {
				case msg.String() == "Y":
					text, what = res.ID, "ID"
				case m.showDetail && m.detailRaw && m.focus == focusDetail:
					// In the raw JSON view, y grabs the whole document.
					if data, err := json.MarshalIndent(res, "", "  "); err == nil {
						text, what = string(data), "JSON"
					}
				case text == "":
					text, what = res.ID, "ID (no ARN)"
				}
				if err := clipboard.WriteAll(text); err != nil {
					m.setToast("Copy failed: " + err.Error())
				} else {
					m.setToast("Copied " + what)
				}
				cmds = append(cmds, toastCmd(3*time.Second))
			}
			return m, tea.Batch(cmds...)

		case "J":
			if m.showDetail && m.detail != nil {
				m.detailRaw = !m.detailRaw
				m.syncDetailViewport()
			}
			return m, tea.Batch(cmds...)

		case "C":
			if rows := m.rowsFor(m.currentService()); len(rows) > 0 {
				path, err := m.exportCurrentView()
				if err != nil {
					m.setToast("Export failed: " + err.Error())
				} else {
					m.setToast("Exported " + path)
				}
				cmds = append(cmds, toastCmd(5*time.Second))
			}
			return m, tea.Batch(cmds...)

		case "P":
			if !m.switching {
				m.switcherForm = m.buildSwitcherForm()
				m.showSwitcher = true
				cmds = append(cmds, m.switcherForm.Init())
			}
			return m, tea.Batch(cmds...)

		case "ctrl+p":
			m.openFinder()
			cmds = append(cmds, textinput.Blink)
			return m, tea.Batch(cmds...)

		case ui.KeySettings:
			m.settings = ui.NewSettingsModel(m.width, m.height, m.configPath, m.cfg)
			m.showSettings = true
			return m, tea.Batch(cmds...)

		case ui.KeyHelp:
			m.openHelpOverlay()
			return m, tea.Batch(cmds...)

		case "e":
			if len(m.errors) > 0 {
				m.openErrorsOverlay()
			}
			return m, tea.Batch(cmds...)

		case ui.KeyDebug:
			m.openDebugOverlay()
			return m, tea.Batch(cmds...)

		case "w":
			m.saveSnapshot()
			cmds = append(cmds, toastCmd(4*time.Second))
			return m, tea.Batch(cmds...)

		case "D":
			// What changed since the account baseline: save one on first use,
			// diff against it thereafter (mirrors the VPC explorer's w).
			m.openAcctDiff()
			cmds = append(cmds, toastCmd(4*time.Second))
			return m, tea.Batch(cmds...)

		case "W":
			if m.engine == nil {
				m.setToast("Watch mode unavailable in offline snapshot view")
				cmds = append(cmds, toastCmd(3*time.Second))
				return m, tea.Batch(cmds...)
			}
			m.watchMode = !m.watchMode
			if m.watchMode {
				m.watchTimerID++
				m.setToast("Watch mode enabled (5s auto-refresh)")
				m.snapshotStates()
				cmds = append(cmds, m.watchTick(5*time.Second, m.watchTimerID))
			} else {
				m.setToast("Watch mode disabled")
			}
			cmds = append(cmds, toastCmd(3*time.Second))
			return m, tea.Batch(cmds...)

		// The debug panes and copy helpers below only apply while the detail
		// panel is open; in table focus the same keys belong to the table
		// (k = up, g = top, …) and fall through to it.
		case "t":
			if m.focus == focusDetail && m.detail != nil {
				res := *m.detail
				m.showTimeline = !m.showTimeline
				if m.showTimeline {
					m.showLogs = false
					m.showMetrics = false
					m.showXref = false
					m.timelineLoading = true
					m.timelineErr = nil
					m.timelineEvents = nil
					cmds = append(cmds, m.fetchTimelineCmd(res))
				}
				m.syncDetailViewport()
				return m, tea.Batch(cmds...)
			}

		case "l":
			if m.focus == focusDetail && m.detail != nil {
				res := *m.detail
				m.showLogs = !m.showLogs
				if m.showLogs {
					m.showTimeline = false
					m.showMetrics = false
					m.showXref = false
					logGroup := logGroupFor(res)
					if logGroup == "" {
						m.logsErr = fmt.Errorf("no standard log group known for service %s", res.Service)
						m.logsLoading = false
						m.logsLines = nil
					} else {
						m.logsLoading = true
						m.logsErr = nil
						m.logsLines = nil
						cmds = append(cmds, m.fetchLogsCmd(res, logGroup))
					}
				}
				m.syncDetailViewport()
				return m, tea.Batch(cmds...)
			}

		case "L":
			// Jump to the CloudWatch Logs TUI pre-filtered to this resource's
			// log group (AXE-011). The summary TUI suspends, the cw TUI runs in
			// the same terminal, and quitting it returns here with selection,
			// filters and scroll intact (the model is untouched).
			if m.focus == focusDetail && m.detail != nil {
				res := *m.detail
				group, ok := loggroup.For(loggroup.Resource{
					Service: res.Service, Type: res.Type, ID: res.ID, Name: res.Name,
				})
				if !ok {
					m.setToast(fmt.Sprintf("No CloudWatch log group derivable for %s — press 'l' for inline logs", res.Service))
					cmds = append(cmds, toastCmd(4*time.Second))
					return m, tea.Batch(cmds...)
				}
				return m, m.jumpToLogsCmd(res.Region, group)
			}

		case "g":
			if m.focus == focusDetail && m.detail != nil {
				res := *m.detail
				m.showMetrics = !m.showMetrics
				if m.showMetrics {
					m.showTimeline = false
					m.showLogs = false
					m.showXref = false
					ns, name, dims, _, ok := metricParamsFor(res)
					if !ok {
						m.metricsErr = fmt.Errorf("no metric mapping for service %s", res.Service)
						m.metricsLoading = false
						m.metricsData = nil
					} else {
						m.metricsLoading = true
						m.metricsErr = nil
						m.metricsData = nil
						cmds = append(cmds, m.fetchMetricsCmd(res, ns, name, dims))
					}
				}
				m.syncDetailViewport()
				return m, tea.Batch(cmds...)
			}

		case "x":
			if m.focus == focusDetail && m.detail != nil {
				res := *m.detail
				m.showXref = !m.showXref
				if m.showXref {
					m.showTimeline = false
					m.showLogs = false
					m.showMetrics = false
					m.xrefResources = m.findReferences(res.ID)
				}
				m.syncDetailViewport()
				return m, tea.Batch(cmds...)
			}

		case "o":
			if m.focus == focusDetail && m.detail != nil {
				url, specific := consolelink.URL(*m.detail)
				if err := clipboard.WriteAll(url); err != nil {
					m.setToast("Copy URL failed: " + err.Error())
				} else {
					msg := "Copied AWS Console URL"
					if !specific {
						msg = "Copied console ARN-search URL"
					}
					if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
						msg += " — opened in browser"
					}
					m.setToast(msg)
				}
				cmds = append(cmds, toastCmd(3*time.Second))
				return m, tea.Batch(cmds...)
			}

		case "k":
			if m.focus == focusDetail && m.detail != nil {
				cmdStr := awsutil.AWSCLICommand(*m.detail)
				if err := clipboard.WriteAll(cmdStr); err != nil {
					m.setToast("Copy command failed: " + err.Error())
				} else {
					m.setToast("Copied AWS CLI command")
				}
				cmds = append(cmds, toastCmd(3*time.Second))
				return m, tea.Batch(cmds...)
			}
		}

	case tea.MouseMsg:
		// Wheel scrolling goes to the focused panel: the detail viewport when
		// it has focus, otherwise the table (3 rows per tick) or the sidebar.
		switch msg.Button {
		case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
			down := msg.Button == tea.MouseButtonWheelDown
			switch m.focus {
			case focusDetail:
				if down {
					m.detailViewport.LineDown(3)
				} else {
					m.detailViewport.LineUp(3)
				}
			case focusSidebar:
				if down && m.activeService < len(m.services)-1 {
					m.activeService++
					m.updateTableRows()
				} else if !down && m.activeService > 0 {
					m.activeService--
					m.updateTableRows()
				}
			case focusTable:
				if down {
					m.table.MoveDown(3)
				} else {
					m.table.MoveUp(3)
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Check sidebar zone clicks.
		sidebarHit := false
		for i := range m.services {
			zID := fmt.Sprintf("%s%d", zoneSvc, i)
			if m.zones.Get(zID).InBounds(msg) {
				if m.activeService != i {
					m.activeService = i
					m.updateTableRows()
				}
				m.focus = focusTable
				m.table.Focus()
				sidebarHit = true
				break
			}
		}

		// Table row clicks select the row; only the rendered rows have zones.
		if !sidebarHit && msg.Button == tea.MouseButtonLeft {
			start, end := m.table.VisibleRange()
			for i := start; i < end; i++ {
				if m.zones.Get(fmt.Sprintf("%s%d", zoneRow, i)).InBounds(msg) {
					m.table.SetCursor(i)
					m.focus = focusTable
					m.table.Focus()
					break
				}
			}
		}

	case chunkMsg:
		if msg.gen != m.scanGen {
			// Straggler from a scan that was cancelled by a profile/region
			// switch; its data belongs to the old account/region view.
			return m, tea.Batch(cmds...)
		}
		m.loading = false
		m.applyChunk(msg.chunk)
		// Drain whatever else is already buffered so a burst of page-level
		// chunks costs one merge + view rebuild instead of one per chunk.
		closed := false
	drain:
		for {
			select {
			case c, ok := <-m.chunks:
				if !ok {
					closed = true
					break drain
				}
				m.applyChunk(c)
			default:
				break drain
			}
		}
		m.onResultsChanged()
		m.updateTableRows()
		if closed {
			m.done = true
			m.finishWatchSweep()
		} else {
			cmds = append(cmds, waitForChunk(m.chunks, m.scanGen))
		}
		return m, tea.Batch(cmds...)

	case doneMsg:
		if msg.gen != m.scanGen {
			return m, tea.Batch(cmds...)
		}
		m.loading = false
		m.done = true
		m.finishWatchSweep()

	case engineSwitchedMsg:
		m.switching = false
		if msg.err != nil {
			m.setToast("Switch failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(5*time.Second))
			return m, tea.Batch(cmds...)
		}
		m.engine = msg.eng
		cmds = append(cmds, m.restartScan())
		m.setToast(fmt.Sprintf("Switched to profile %q · %s — rescanning", msg.profile, msg.region))
		cmds = append(cmds, toastCmd(4*time.Second))
		return m, tea.Batch(cmds...)

	case clearToastMsg:
		m.toast = ""
		m.toastExp = time.Time{}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		// Keep the debug pane live while a scan runs: the spinner ticks
		// continuously during loading, so refresh the captured activity on
		// each tick rather than only when the user scrolls. Tail-follow only
		// when already at the bottom, so scrolling up to read earlier lines
		// isn't yanked back down by the next tick.
		if m.showDebug {
			atBottom := m.debugViewport.AtBottom()
			m.debugViewport.SetContent(m.debugBody())
			if atBottom {
				m.debugViewport.GotoBottom()
			}
		}

	case timelineMsg:
		if m.showDetail && m.detail != nil && m.detail.ID == msg.resourceID {
			m.timelineLoading = false
			m.timelineEvents = msg.events
			m.timelineErr = msg.err
			m.syncDetailViewport()
		}
		return m, tea.Batch(cmds...)

	case logsMsg:
		if m.showDetail && m.detail != nil && m.detail.ID == msg.resourceID {
			m.logsLoading = false
			m.logsLines = msg.lines
			m.logsErr = msg.err
			m.syncDetailViewport()
		}
		return m, tea.Batch(cmds...)

	case metricsMsg:
		if m.showDetail && m.detail != nil && m.detail.ID == msg.resourceID {
			m.metricsLoading = false
			m.metricsData = msg.data
			m.metricsErr = msg.err
			m.syncDetailViewport()
		}
		return m, tea.Batch(cmds...)

	case cwJumpDoneMsg:
		// Returned from the suspended cw Logs TUI. Surface a launch failure;
		// a clean exit just lands the user back where they were.
		if msg.err != nil {
			m.setToast("Could not open CloudWatch logs: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
		}
		return m, tea.Batch(cmds...)

	case watchTickMsg:
		if m.watchMode && msg.timerID == m.watchTimerID && m.engine != nil {
			m.snapshotStates()
			cmds = append(cmds, m.watchRefreshCmd(), m.watchTick(5*time.Second, m.watchTimerID))
		}
		return m, tea.Batch(cmds...)
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

// lessResource is the table's display order: by service, then name.
func lessResource(a, b model.Resource) bool {
	if a.Service != b.Service {
		return a.Service < b.Service
	}
	return a.Name < b.Name
}

// searchTextFor returns a resource's filterable cells joined and lower-cased
// once, so the quick filter never re-lowercases every cell on every keystroke.
// The NUL separator stops the filter text from matching across cell borders.
func searchTextFor(r model.Resource) string {
	return strings.ToLower(r.Service + "\x00" + r.Type + "\x00" + r.Region +
		"\x00" + r.ID + "\x00" + r.Name + "\x00" + r.State)
}

// applyChunk folds one streamed chunk into the model's data.
func (m *tuiModel) applyChunk(c model.ResultChunk) {
	m.mergeResources(c.Resources)
	m.errors = append(m.errors, c.Errors...)
	for _, e := range c.Errors {
		m.svcErrs[e.Service]++
	}
	if c.Progress != nil {
		key := c.Progress.Service + "@" + c.Progress.Region
		if m.tasksPending[key] {
			delete(m.tasksPending, key)
			m.tasksDone++
		}
	}
}

// resourceKey identifies a resource for dedupe and state tracking: the ARN
// when present, else a composite of the identifying fields (matching the
// fallback key used by awsutil.DiffScans).
func resourceKey(r model.Resource) string {
	if r.ARN != "" {
		return r.ARN
	}
	return r.Service + "\x00" + r.Type + "\x00" + r.Region + "\x00" + r.ID
}

// mergeResources folds newly arrived resources into the sorted view. It
// applies the same dedupe rule as summary.Dedupe (richer entry wins; an
// equally rich entry replaces in place so re-scans pick up state changes) but
// incrementally: the batch is sorted on its own (k log k) and merged into the
// already-sorted slice (O(n+k)), instead of re-deduping and re-sorting every
// collected resource on every chunk.
func (m *tuiModel) mergeResources(batch []model.Resource) {
	if len(batch) == 0 {
		return
	}

	fresh := make([]model.Resource, 0, len(batch))
	inBatch := make(map[string]int) // key -> index into fresh (same-batch dupes)
	var drops []int                 // indexes into m.sorted superseded by a richer entry

	for _, r := range batch {
		key := resourceKey(r)
		if m.watchSeen != nil {
			m.watchSeen[key] = true
		}
		if fi, ok := inBatch[key]; ok {
			if summary.Richness(r) > summary.Richness(fresh[fi]) {
				fresh[fi] = r
			}
			continue
		}
		if idx, seen := m.byARN[key]; seen {
			// ">=" so a re-scan of the same resource updates it in place,
			// surfacing state changes; only a strictly poorer entry is dropped.
			if summary.Richness(r) >= summary.Richness(m.sorted[idx]) {
				if m.sorted[idx].Service == r.Service && m.sorted[idx].Name == r.Name {
					// Same sort position: replace in place.
					m.sorted[idx] = r
					m.searchText[idx] = searchTextFor(r)
				} else {
					// Sort key changed (e.g. the typed entry carries a real
					// name the tag sweep lacked): drop and re-insert.
					drops = append(drops, idx)
					inBatch[key] = len(fresh)
					fresh = append(fresh, r)
				}
			}
			continue
		}
		inBatch[key] = len(fresh)
		fresh = append(fresh, r)
	}

	if len(drops) > 0 {
		sort.Ints(drops)
		kept := m.sorted[:0]
		keptText := m.searchText[:0]
		d := 0
		for i := range m.sorted {
			if d < len(drops) && drops[d] == i {
				d++
				continue
			}
			kept = append(kept, m.sorted[i])
			keptText = append(keptText, m.searchText[i])
		}
		m.sorted = kept
		m.searchText = keptText
	}

	if len(fresh) > 0 {
		sort.SliceStable(fresh, func(i, j int) bool { return lessResource(fresh[i], fresh[j]) })

		merged := make([]model.Resource, 0, len(m.sorted)+len(fresh))
		mergedText := make([]string, 0, len(m.sorted)+len(fresh))
		i, j := 0, 0
		for i < len(m.sorted) && j < len(fresh) {
			// Strict less keeps existing entries first on ties (stable).
			if lessResource(fresh[j], m.sorted[i]) {
				merged = append(merged, fresh[j])
				mergedText = append(mergedText, searchTextFor(fresh[j]))
				j++
			} else {
				merged = append(merged, m.sorted[i])
				mergedText = append(mergedText, m.searchText[i])
				i++
			}
		}
		for ; i < len(m.sorted); i++ {
			merged = append(merged, m.sorted[i])
			mergedText = append(mergedText, m.searchText[i])
		}
		for ; j < len(fresh); j++ {
			merged = append(merged, fresh[j])
			mergedText = append(mergedText, searchTextFor(fresh[j]))
		}
		m.sorted = merged
		m.searchText = mergedText
	}

	// Positions shifted: refresh the key index. (In-place replacements alone
	// don't shift, so the rebuild is skipped for richness-only updates.)
	if len(fresh) > 0 || len(drops) > 0 {
		for _, r := range fresh {
			m.svcSet[r.Service] = true
		}
		for i, r := range m.sorted {
			m.byARN[resourceKey(r)] = i
		}
	}

	if len(m.prevStates) > 0 {
		if m.changedRows == nil {
			m.changedRows = make(map[string]time.Time)
		}
		now := time.Now()
		for key, exp := range m.changedRows {
			if now.After(exp) {
				delete(m.changedRows, key)
			}
		}
		for _, r := range m.sorted {
			key := resourceKey(r)
			if oldState, ok := m.prevStates[key]; ok && oldState != r.State {
				m.changedRows[key] = now.Add(10 * time.Second)
			}
		}
	}
}

// finishWatchSweep removes rows that did not reappear in the just-completed
// watch refresh, so deleted resources drop out of the table instead of
// lingering forever.
func (m *tuiModel) finishWatchSweep() {
	if m.watchSeen == nil {
		return
	}
	seen := m.watchSeen
	m.watchSeen = nil

	kept := m.sorted[:0]
	keptText := m.searchText[:0]
	removed := false
	for i, r := range m.sorted {
		if seen[resourceKey(r)] {
			kept = append(kept, r)
			keptText = append(keptText, m.searchText[i])
		} else {
			removed = true
		}
	}
	if !removed {
		return
	}
	m.sorted = kept
	m.searchText = keptText
	m.byARN = make(map[string]int, len(m.sorted))
	m.svcSet = make(map[string]bool)
	for i, r := range m.sorted {
		m.byARN[resourceKey(r)] = i
		m.svcSet[r.Service] = true
	}
	m.onResultsChanged()
	m.updateTableRows()
}

// onResultsChanged re-derives the service list after new chunks were merged,
// then invalidates the cached rows. Filter changes alone only need
// invalidateRows, which reuses the sorted slice.
func (m *tuiModel) onResultsChanged() {
	// Unfiltered per-service totals for the "14/3201" filter indicator.
	totals := make(map[string]int, len(m.svcSet)+2)
	triageCount := 0
	for _, r := range m.sorted {
		totals[r.Service]++
		if isUnhealthy(r) {
			triageCount++
		}
	}
	totals["All"] = len(m.sorted)
	totals["Triage"] = triageCount
	m.svcTotals = totals

	names := make([]string, 0, len(m.svcSet)+2)
	if triageCount > 0 {
		names = append(names, "Triage")
	}
	names = append(names, "All")
	for svc := range m.svcSet {
		names = append(names, svc)
	}
	sort.Strings(names[len(names)-len(m.svcSet):])
	m.services = names

	// Clamp active service index.
	if m.activeService >= len(m.services) {
		m.activeService = 0
	}

	m.invalidateRows()
}

// invalidateRows drops the cached per-service row groups. They are rebuilt
// lazily by rowsFor for whichever service is actually displayed, so a filter
// keystroke costs one pass over the visible group instead of every group.
func (m *tuiModel) invalidateRows() {
	m.allRows = make(map[string][]table.Row)
	m.allRes = make(map[string][]model.Resource)
}

// isUnhealthy returns true if a resource is in an unhealthy/bad state.
func isUnhealthy(r model.Resource) bool {
	state := strings.ToLower(r.State)
	// CloudWatch Alarms
	if r.Service == "cloudwatch" && state == "alarm" {
		return true
	}
	// EC2 instances
	if r.Service == "ec2" {
		if state == "stopped" || state == "impaired" || state == "unhealthy" || strings.Contains(state, "fail") {
			return true
		}
	}
	// RDS DB Instances
	if r.Service == "rds" {
		if state == "stopped" || strings.Contains(state, "full") || state == "unhealthy" || state == "failed" {
			return true
		}
	}
	// ELB / Target Groups
	if r.Service == "elbv2" {
		if strings.Contains(state, "unhealthy") || state == "failed" {
			return true
		}
	}
	// EKS
	if r.Service == "eks" {
		if strings.Contains(state, "degraded") || state == "failed" || state == "unhealthy" {
			return true
		}
	}
	// Lambda
	if r.Service == "lambda" {
		if strings.Contains(state, "throttle") || state == "failed" || state == "unhealthy" {
			return true
		}
	}
	// General catch-all for any resource in explicit alarm/unhealthy/failed state
	if state == "alarm" || state == "unhealthy" || state == "failed" || state == "degraded" {
		return true
	}
	return false
}

// sortField returns the cell a table sort column maps to.
func (m tuiModel) sortField(r model.Resource, col int) string {
	cols := m.columns()
	if col < 0 || col >= len(cols) {
		return ""
	}
	title := cols[col].Title
	title = strings.TrimSuffix(title, table.SortAscArrow)
	title = strings.TrimSuffix(title, table.SortDescArrow)
	switch title {
	case "Account":
		return r.AccountID
	case "Service":
		return r.Service
	case "Type":
		return r.Type
	case "Region":
		return r.Region
	case "ID":
		return r.ID
	case "Name":
		return r.Name
	case "State":
		return r.State
	}
	return ""
}

// rowsFor returns the filtered (and, when active, column-sorted) rows for
// svc, building and caching the group on first access. Only mutates the
// (shared) cache maps, so it is safe to call from value-receiver render
// methods.
func (m *tuiModel) rowsFor(svc string) []table.Row {
	if rows, ok := m.allRows[svc]; ok {
		return rows
	}
	query := strings.ToLower(m.filterText)
	var res []model.Resource
	for i, r := range m.sorted {
		if svc == "Triage" {
			if !isUnhealthy(r) {
				continue
			}
		} else if svc != "All" && r.Service != svc {
			continue
		}
		if m.filterRegion != "" && r.Region != m.filterRegion {
			continue
		}
		if m.filterState != "" && r.State != m.filterState {
			continue
		}
		if query != "" && !strings.Contains(m.searchText[i], query) {
			continue
		}
		res = append(res, r)
	}

	// m.sorted is already in natural (service, name) order; a column sort
	// reorders the visible group only. Stable, so equal keys keep natural
	// order as the tie-break.
	if m.sortCol > 0 {
		col, asc := m.sortCol, m.sortAsc
		sort.SliceStable(res, func(i, j int) bool {
			a := strings.ToLower(m.sortField(res[i], col))
			b := strings.ToLower(m.sortField(res[j], col))
			if asc {
				return a < b
			}
			return a > b
		})
	}

	rows := []table.Row{}
	for _, r := range res {
		row := table.Row{fmt.Sprintf("%d", len(rows)+1)}
		if len(m.cfg.Accounts) > 0 {
			row = append(row, r.AccountID)
		}

		// Plain-text marker for recent state transitions: escape codes inside
		// cells would throw off the table's width/truncation accounting.
		stateStr := r.State
		if m.changedRows != nil {
			if exp, ok := m.changedRows[resourceKey(r)]; ok && time.Now().Before(exp) {
				stateStr = "* " + r.State
			}
		}

		row = append(row, r.Service, r.Type, r.Region, r.ID, r.Name, stateStr)
		rows = append(rows, row)
	}
	m.allRows[svc] = rows
	m.allRes[svc] = res
	return rows
}

func (m tuiModel) currentService() string {
	if len(m.services) == 0 || m.activeService >= len(m.services) {
		return "All"
	}
	return m.services[m.activeService]
}

// PageTitle names the current screen for the terminal window/tab title, so
// every page has a unique, shareable name (see ui.WithWindowTitle).
func (m tuiModel) PageTitle() string {
	title := "AWS Explorer › " + m.currentService()
	if m.showDetail && m.detail != nil {
		id := m.detail.Name
		if id == "" {
			id = m.detail.ID
		}
		title += " › " + id
	}
	return title
}

// selectedResource returns the resource under the table cursor.
func (m tuiModel) selectedResource() (model.Resource, bool) {
	m.rowsFor(m.currentService()) // ensure the group cache is built
	res := m.allRes[m.currentService()]
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(res) {
		return model.Resource{}, false
	}
	return res[idx], true
}

func (m *tuiModel) updateTableRows() {
	m.table.SetRows(m.rowsFor(m.currentService()))
}

// ── Table layout ──────────────────────────────────────────────────────────────

// columns returns the table columns sized for the current width. ID and Name
// flex to fill leftover space; when the panel is too narrow for everything,
// the table scrolls horizontally instead of truncating columns away.
func (m tuiModel) columns() []table.Column {
	// Fixed widths: #(4) Account(12 - optional) Service(10) Type(12) Region(13) State(10)
	var fixed int
	var numCols int
	if len(m.cfg.Accounts) > 0 {
		fixed = 4 + 12 + 10 + 12 + 13 + 10
		numCols = 8
	} else {
		fixed = 4 + 10 + 12 + 13 + 10
		numCols = 7
	}
	padding := 2 * numCols
	rem := m.tableInnerWidth() - tablePanelHPad - fixed - padding
	idW := rem * 2 / 5
	nameW := rem - idW
	if idW < 10 {
		idW = 10
	}
	if nameW < 10 {
		nameW = 10
	}

	var cols []table.Column
	cols = append(cols, table.Column{Title: "#", Width: 4})
	if len(m.cfg.Accounts) > 0 {
		cols = append(cols, table.Column{Title: "Account", Width: 12})
	}
	cols = append(cols, []table.Column{
		{Title: "Service", Width: 10},
		{Title: "Type", Width: 12},
		{Title: "Region", Width: 13},
		{Title: "ID", Width: idW},
		{Title: "Name", Width: nameW},
		{Title: "State", Width: 10},
	}...)

	// Column 0 ("#") is a positional counter, not a sortable field. The arrow
	// goes on the active column; every sortable column reserves room for it so
	// the table does not reflow when the sort moves.
	table.ApplySortHeader(cols, m.sortCol, m.sortAsc, func(i int) bool { return i > 0 })
	return cols
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
	// The table content sits inside the panel's horizontal padding, so it must
	// be narrower than the panel Width by that padding (see renderTablePanel).
	m.table.SetWidth(inner - tablePanelHPad)
	m.table.SetHeight(max(tableH, 4))
	// SetColumns resets the horizontal scroll; keep the row set in sync.
	m.updateTableRows()
}

// ── Filter form ───────────────────────────────────────────────────────────────

func (m tuiModel) buildFilterForm() *huh.Form {
	regionSet := map[string]bool{}
	stateSet := map[string]bool{}
	for _, r := range m.sorted {
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

// ── Profile / region switcher ─────────────────────────────────────────────────

// buildSwitcherForm builds the "P" overlay: pick a shared-config profile and
// a region scope, then rescan without restarting the binary.
func (m tuiModel) buildSwitcherForm() *huh.Form {
	profileOpts := []huh.Option[string]{huh.NewOption("— keep current —", "")}
	for _, p := range auth.ListProfiles() {
		profileOpts = append(profileOpts, huh.NewOption(p, p))
	}

	regionOpts := []huh.Option[string]{
		huh.NewOption("— keep current —", ""),
		huh.NewOption("All regions", "all"),
	}
	// Offer the engine's resolved regions first (already scanned), then the
	// full fallback list for anything else.
	seen := map[string]bool{"": true, "all": true}
	var regions []string
	if m.engine != nil {
		regions = append(regions, m.engine.ResolvedRegions...)
	}
	regions = append(regions, awsutil.FallbackRegions...)
	for _, r := range regions {
		if seen[r] {
			continue
		}
		seen[r] = true
		regionOpts = append(regionOpts, huh.NewOption(r, r))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("profile").
				Title("AWS profile").
				Options(profileOpts...),
			huh.NewSelect[string]().
				Key("region").
				Title("Region scope").
				Options(regionOpts...),
		),
	)
}

// applySwitcher mutates the config for the chosen profile/region and rebuilds
// the engine off-thread (STS/identity calls block), reporting back via
// engineSwitchedMsg.
func (m *tuiModel) applySwitcher(profile, region string) tea.Cmd {
	if profile == "" && region == "" {
		return nil // nothing to change
	}
	if profile != "" {
		m.cfg.AWS.Profile = profile
		// A named profile only takes effect through the profile/auto chains.
		if m.cfg.AWS.AuthMethod != "" && m.cfg.AWS.AuthMethod != "auto" && m.cfg.AWS.AuthMethod != "profile" {
			m.cfg.AWS.AuthMethod = "profile"
		}
	}
	switch region {
	case "":
		// keep current scope
	case "all":
		m.cfg.AWS.AllRegions = true
		m.cfg.AWS.Regions = nil
		m.cfg.Filters.Regions = nil
	default:
		m.cfg.AWS.AllRegions = false
		m.cfg.AWS.Regions = []string{region}
		m.cfg.Filters.Regions = nil
	}

	m.switching = true
	m.setToast("Switching — resolving credentials…")
	ctx, cfg := m.ctx, m.cfg
	shownProfile := m.cfg.AWS.Profile
	shownRegion := region
	if shownRegion == "" {
		shownRegion = "current regions"
	} else if shownRegion == "all" {
		shownRegion = "all regions"
	}
	return tea.Batch(toastCmd(4*time.Second), func() tea.Msg {
		eng, err := engine.NewEngine(ctx, cfg)
		return engineSwitchedMsg{eng: eng, profile: shownProfile, region: shownRegion, err: err}
	})
}

// restartScan cancels the running scan, clears all collected data, and starts
// a fresh StreamRun against the (possibly new) engine. The generation counter
// makes any stragglers from the old scan inert.
func (m *tuiModel) restartScan() tea.Cmd {
	if m.engine == nil {
		return nil
	}
	if m.scanCancel != nil {
		m.scanCancel()
	}
	m.scanCtx, m.scanCancel = context.WithCancel(m.ctx)
	m.scanGen++

	m.sorted = nil
	m.searchText = nil
	m.byARN = make(map[string]int)
	m.svcSet = make(map[string]bool)
	m.svcTotals = make(map[string]int)
	m.svcErrs = make(map[string]int)
	m.errors = nil
	m.services = nil
	m.activeService = 0
	m.showDetail = false
	m.detail = nil
	m.loading = true
	m.done = false
	m.resetTaskProgress()
	m.invalidateRows()
	m.updateTableRows()

	m.chunks = make(chan model.ResultChunk, 64)
	go m.engine.StreamRun(m.scanCtx, m.chunks)
	return tea.Batch(waitForChunk(m.chunks, m.scanGen), m.spinner.Tick)
}

// snapshotStates records the current state of every row so the next merge can
// flag transitions.
func (m *tuiModel) snapshotStates() {
	m.prevStates = make(map[string]string, len(m.sorted))
	for _, r := range m.sorted {
		m.prevStates[resourceKey(r)] = r.State
	}
}

func (m *tuiModel) watchRefreshCmd() tea.Cmd {
	if m.engine == nil {
		return nil
	}
	if m.scanCancel != nil {
		m.scanCancel()
	}
	m.scanCtx, m.scanCancel = context.WithCancel(m.ctx)
	m.scanGen++
	m.loading = true
	m.done = false
	m.watchSeen = make(map[string]bool)
	m.resetTaskProgress()
	m.chunks = make(chan model.ResultChunk, 64)
	go m.engine.StreamRun(m.scanCtx, m.chunks)
	return waitForChunk(m.chunks, m.scanGen)
}

// ── CSV export ────────────────────────────────────────────────────────────────

// inventoryCSV builds the header and rows for exporting resources — full,
// untruncated values rather than the on-screen cells.
func inventoryCSV(res []model.Resource) ([]string, [][]string) {
	header := []string{"Service", "Type", "Region", "AZ", "Account", "ID", "Name", "State", "ARN", "Created", "Tags"}
	rows := make([][]string, 0, len(res))
	for _, r := range res {
		created := ""
		if r.CreatedAt != nil {
			created = r.CreatedAt.Format("2006-01-02 15:04:05")
		}
		tags := make([]string, 0, len(r.Tags))
		for k, v := range r.Tags {
			tags = append(tags, k+"="+v)
		}
		sort.Strings(tags)
		rows = append(rows, []string{
			r.Service, r.Type, r.Region, r.AZ, r.AccountID,
			r.ID, r.Name, r.State, r.ARN, created, strings.Join(tags, "; "),
		})
	}
	return header, rows
}

// exportCurrentView writes the displayed service's filtered rows to a
// timestamped CSV under ~/.aws_explorer/exports and returns the path.
func (m *tuiModel) exportCurrentView() (string, error) {
	svc := m.currentService()
	m.rowsFor(svc) // ensure the group cache (and its order) is current
	header, rows := inventoryCSV(m.allRes[svc])
	dir, err := csvexport.DefaultDir()
	if err != nil {
		return "", err
	}
	return csvexport.Write(dir, "inventory-"+svc, header, rows)
}

// ── Detail viewport ───────────────────────────────────────────────────────────

func (m *tuiModel) syncDetailViewport() {
	if m.detail == nil || m.width == 0 {
		return
	}
	// Reserve two columns on the right for the scrollbar gutter (a spacer plus
	// the bar, drawn in renderDetailPanel). Reserved unconditionally so the
	// content does not reflow the moment the panel becomes scrollable.
	vpWidth := detailInner - 2 - 2
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

// helpBody returns the full keybinding reference as a single block. It is
// taller than most terminals, so it is shown inside a scrollable viewport
// rather than rendered as one fixed block (which would clip the bottom rows).
func (m tuiModel) helpBody() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		"Navigation",
		"  ↑/↓, [ ]           Move selection / scroll detail",
		"  < >                Scroll table columns (when more columns than fit)",
		"  Tab / Shift+Tab    Switch panel focus",
		"  Enter              Select service / open detail",
		"  Esc                Close detail or overlay",
		"",
		"Resources & Audit",
		"  Ctrl+P             Jump to any resource (fuzzy search across all services)",
		"  /                  Quick text filter (shows match count)",
		"  f                  Advanced filter (region / state)",
		"  r                  Reset all filters",
		"  s / R              Sort by next column / reverse sort order",
		"  y / Y              Copy ARN / ID of the selected resource",
		"  J                  Toggle raw JSON in the detail panel (y copies it)",
		"  C                  Export current view to CSV (~/.aws_explorer/exports)",
		"  w                  Save full scan snapshot JSON",
		"  W                  Toggle live watch mode (5s auto-refresh)",
		"  D                  What changed: baseline the account, then diff against it later",
		"",
		"Support Debugging (while in detail panel)",
		"  t                  Toggle CloudTrail resource mutation timeline",
		"  l                  Toggle CloudWatch recent ERROR logs (inline)",
		"  L                  Open the CloudWatch Logs explorer on this resource's log group (Lambda/RDS/EKS)",
		"  g                  Toggle CloudWatch key metric sparkline (1hr)",
		"  x                  Toggle cross-resource relationship xrefs",
		"  o                  Open in AWS Console (copies URL; opens browser when local)",
		"  k                  Copy AWS CLI reproduction command",
		"",
		"Utility",
		"  P                  Switch AWS profile / region and rescan",
		"  e                  View access / scan errors",
		"  ~                  Debug: live view of what the tool is doing",
		"  S                  Settings (theme & colors)",
		"  ?                  Toggle this help",
		"  q, Ctrl+C          Quit",
	)
}

// openHelpOverlay (re)builds the scrollable help overlay sized to the current
// terminal, so the full reference is reachable by scrolling even when it is
// taller than the screen.
func (m *tuiModel) openHelpOverlay() {
	w := m.width - 12
	if w > 72 {
		w = 72
	}
	if w < 32 {
		w = 32
	}
	// Reserve rows for the surrounding frame: border (2), padding (2), title
	// and its blank line (2), and the scroll hint and its blank line (2).
	h := m.height - 12
	if h < 6 {
		h = 6
	}
	m.helpViewport = viewport.New(w, h)
	m.helpViewport.SetContent(m.helpBody())
	m.helpViewport.GotoTop()
	m.showHelp = true
}

// helpView renders the scrollable help overlay (title + body + scroll hint)
// inside the shared themed frame so it matches the other browsers' help.
func (m tuiModel) helpView() string {
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll · ?/Esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, m.helpViewport.View(), "", hint)
	// HelpView pads 2 cols on each side inside its width, so add that back so
	// the viewport's lines fit exactly instead of wrapping.
	return ui.HelpView("AWS Explorer Help", body, m.helpViewport.Width+4)
}

// loadingView renders the initial scan screen as a centered card: the app
// name, a spinner with status text and — once the task plan is known — live
// collector progress. It replaces the bare one-line spinner so the first thing
// the user sees reads as a deliberate screen, not an unfinished frame.
func (m tuiModel) loadingView() string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true).
		Render("AWS Explorer")
	status := m.spinner.View() + "  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
			Render("Scanning your account…")

	parts := []string{title, "", status}
	if m.tasksTotal > 0 {
		parts = append(parts, "",
			ui.MutedStyle().Render(fmt.Sprintf("%d of %d collectors done", m.tasksDone, m.tasksTotal)))
	}

	card := ui.LoadingBoxStyle().Render(lipgloss.JoinVertical(lipgloss.Center, parts...))
	if m.width <= 0 || m.height <= 0 {
		return card
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func (m tuiModel) View() string {
	var output string

	if m.loading && len(m.sorted) == 0 {
		// The initial scan is exactly when the user is most likely to wonder
		// what the tool is doing, so honour the debug overlay here too rather
		// than only showing the bare spinner.
		base := m.loadingView()
		if m.showDebug {
			output = ui.OverlayCenter(base, m.debugOverlay(), m.width, m.height)
			return m.zones.Scan(output)
		}
		return m.zones.Scan(base)
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
	} else if m.showErrors {
		centered := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, m.errorsOverlay())
		output = lipgloss.JoinVertical(lipgloss.Left, header, centered, status)
	} else if m.showDebug {
		// Float the debug pane over the live frame (HUD-style, like the
		// settings panel) instead of replacing the body, so the table and the
		// header's scanning progress stay visible and refresh in the
		// background while the user watches the activity log.
		base := lipgloss.JoinVertical(lipgloss.Left, header, m.renderBody(), status)
		output = ui.OverlayCenter(base, m.debugOverlay(), m.width, m.height)
	} else if m.showAcctDiff {
		centered := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, m.acctDiffOverlay())
		output = lipgloss.JoinVertical(lipgloss.Left, header, centered, status)
	} else if m.showSettings {
		// The console floats over the live app (HUD-style): render the normal
		// frame and composite the fixed-size panel into its center, so the
		// theme changes are visible on the real UI around it.
		base := lipgloss.JoinVertical(lipgloss.Left, header, m.renderBody(), status)
		output = ui.OverlayCenter(base, m.settings.View(), m.width, m.height)
	} else if m.showFilter && m.filterForm != nil {
		formW := 52
		formH := 14
		formView := ui.ModalStyle(formW, formH).Render(m.filterForm.View())
		modal := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, formView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, modal, status)
	} else if m.showSwitcher && m.switcherForm != nil {
		formW := 56
		formH := 16
		formView := ui.ModalStyle(formW, formH).Render(m.switcherForm.View())
		modal := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, formView)
		output = lipgloss.JoinVertical(lipgloss.Left, header, modal, status)
	} else if m.showFinder {
		modal := lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, m.finderView())
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

// scanStatus describes the scan for the header/status bar: real done/total
// task progress while running (with the stragglers named when only a few
// remain), "complete" when finished.
func (m tuiModel) scanStatus() string {
	if m.done {
		return "complete"
	}
	if m.tasksTotal == 0 {
		return "streaming"
	}
	status := fmt.Sprintf("scanning %d/%d", m.tasksDone, m.tasksTotal)
	if rem := m.tasksTotal - m.tasksDone; rem > 0 && rem <= 3 {
		var waiting []string
		for k := range m.tasksPending {
			waiting = append(waiting, k)
		}
		sort.Strings(waiting)
		status += "  waiting: " + strings.Join(waiting, ", ")
	}
	return status
}

func (m tuiModel) renderHeader() string {
	status := m.scanStatus()
	if m.loading && len(m.sorted) == 0 {
		status = m.spinner.View() + " " + status
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

	title := fmt.Sprintf("  AWS Explorer › %s  ·  %s  ·  %d resources%s",
		m.currentService(), status, len(m.sorted), filterInfo)
	// Spotlight the active region scope when not in all-regions mode, so a
	// single-region scan can never be mistaken for the whole account. Offline
	// snapshot views (no engine) have no live region scope, so skip them.
	if m.engine != nil {
		if badge := ui.RegionBadge(m.engine.EffectiveRegions(), m.cfg.AWS.AllRegions); badge != "" {
			title += "  " + badge
		}
	}
	// The error badge goes last (in error color) so a failed scan is
	// impossible to miss; nothing follows it, so the embedded color reset
	// cannot bleed into other header text.
	if n := len(m.errors); n > 0 {
		title += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorError())).
			Bold(true).
			Render(fmt.Sprintf("⚠ %d errors — press e", n))
	}

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
	b.WriteString(ui.PanelTitleStyle().Render("Services") + "\n\n")

	// rowW is the uniform width every row is padded to.
	rowW := sidebarInner - 2

	hiBg := lipgloss.Color(ui.ColorHighlight())
	hiFg := lipgloss.Color(ui.ColorHighlightText())
	textFg := lipgloss.Color(ui.ColorText())
	warnFg := lipgloss.Color(ui.ColorWarning())
	mutedFg := lipgloss.Color(ui.ColorMuted())

	// Every row is exactly one line: a 2-column marker, the (possibly
	// truncated) name, an optional error badge, then a right-aligned live
	// resource count. Widths are computed in display columns so a long name
	// can never overflow rowW — one column over and lipgloss wraps the row,
	// shifting every entry below it.
	const markerW = 2 // "▶ " / "  "

	for i, svc := range m.services {
		zID := fmt.Sprintf("%s%d", zoneSvc, i)
		errCount := m.svcErrs[svc]
		if svc == "All" {
			errCount = len(m.errors)
		}
		badge := ""
		if errCount > 0 {
			badge = fmt.Sprintf(" ⚠%d", errCount)
		}

		// Right-aligned resource count (k9s-style), drawn as muted metadata. It
		// grows live as the scan streams in. Reserve its width plus a single
		// separating space; the name takes whatever remains.
		count := ""
		if n := m.svcTotals[svc]; n > 0 {
			count = fmt.Sprintf("%d", n)
		}
		countW := 0
		if count != "" {
			countW = ansi.StringWidth(count) + 1
		}

		active := i == m.activeService
		marker := "  "
		if active {
			marker = "▶ "
		}
		label := svc
		if avail := rowW - markerW - countW - ansi.StringWidth(badge); ansi.StringWidth(label) > avail {
			if avail < 1 {
				avail = 1
			}
			label = ansi.Truncate(label, avail, "…")
		}

		left := marker + label + badge
		gap := rowW - ansi.StringWidth(left) - countW
		if gap < 0 {
			gap = 0
		}

		// Pick the name ink: selected row wins, then an error badge, else body.
		nameFg := textFg
		switch {
		case active:
			nameFg = hiFg
		case errCount > 0:
			nameFg = warnFg
		}
		countFg := mutedFg
		if active {
			countFg = hiFg
		}

		// Compose the row from styled segments so name, badge and count keep
		// their own ink while a single highlight background (when selected)
		// spans the full row width.
		nameStyle := lipgloss.NewStyle().Foreground(nameFg)
		countStyle := lipgloss.NewStyle().Foreground(countFg)
		gapStyle := lipgloss.NewStyle()
		if active {
			nameStyle = nameStyle.Background(hiBg)
			countStyle = countStyle.Background(hiBg)
			gapStyle = gapStyle.Background(hiBg)
		}

		var line string
		if count != "" {
			line = nameStyle.Render(left) + gapStyle.Render(strings.Repeat(" ", gap+1)) + countStyle.Render(count)
		} else {
			line = nameStyle.Render(left) + gapStyle.Render(strings.Repeat(" ", gap))
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
	if filterActive && len(m.rowsFor(m.currentService())) == 0 {
		hint := lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("  No resources match current filter  •  press r to reset")
		tableView = lipgloss.JoinVertical(lipgloss.Left, tableView, hint)
	}

	parts := []string{tableView}
	if m.filtering || m.filterText != "" {
		matches := fmt.Sprintf("  ·  %d/%d match", len(m.rowsFor(m.currentService())), m.svcTotals[m.currentService()])
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorMuted())).Render("Filter: ")+
			m.filterInput.View()+
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(matches))
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
	// Pair the viewport with a vertical scrollbar gutter so the reader can see
	// at a glance how much detail is above/below the fold.
	bar := ui.VScrollbar(
		m.detailViewport.Height,
		m.detailViewport.TotalLineCount(),
		m.detailViewport.VisibleLineCount(),
		m.detailViewport.YOffset,
	)
	content := lipgloss.JoinHorizontal(lipgloss.Top, m.detailViewport.View(), " ", bar)
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

	shown := len(m.rowsFor(svc))
	total := m.svcTotals[svc]
	count := fmt.Sprintf("%d", shown)
	if shown != total {
		count = fmt.Sprintf("%d/%d", shown, total) // a filter is hiding rows
	}

	left := fmt.Sprintf("Service: %s  ·  Resources: %s  ·  Errors: %d  ·  %s",
		svc, count, len(m.errors), m.scanStatus())

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
		return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("?/Esc", "close help")}
	case m.showErrors:
		return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("Esc/e", "close")}
	case m.showDebug:
		return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("g/G", "top/bottom"), ui.H("Esc/~", "close")}
	case m.showAcctDiff:
		return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("b", "re-baseline"), ui.H("Esc/D", "close")}
	case m.showFilter, m.showSwitcher:
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
	case m.showFinder:
		return []ui.KeyHint{
			ui.H("type", "to search"),
			ui.H("↑/↓", "select"),
			ui.H("Enter", "jump"),
			ui.H("Esc", "close"),
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
			ui.H("^P", "jump"),
			ui.H("f", "adv filter"),
			ui.H("s", "sort"),
			ui.H("y", "copy ARN"),
			ui.H("C", "csv"),
			ui.H("w", "snapshot"),
			ui.H("W", "watch"),
		)
		if m.filterRegion != "" || m.filterState != "" || m.filterText != "" {
			hints = append(hints, ui.H("r", "reset filters"))
		}
		if len(m.errors) > 0 {
			hints = append(hints, ui.H("e", fmt.Sprintf("errors (%d)", len(m.errors))))
		}
		hints = append(hints, ui.H("~", "debug"))
		return append(hints,
			ui.H("P", "profile"),
			ui.H("Tab", "panel"),
			ui.H("S", "theme"),
			ui.H("q", "quit"),
			ui.H("?", "help"),
		)
	case focusDetail:
		return []ui.KeyHint{
			ui.H("Esc", "close"),
			ui.H("↑/↓ or [ ]", "scroll"),
			ui.H("J", "raw json"),
			ui.H("y", "copy"),
			ui.H("t/l/g/x", "debug info"),
			ui.H("L", "logs explorer"),
			ui.H("o/k", "copy console/cli"),
			ui.H("Tab", "panel"),
			ui.H("q", "quit"),
			ui.H("?", "help"),
		}
	}
	return []ui.KeyHint{ui.H("?", "help")}
}

// ── Detail renderer ───────────────────────────────────────────────────────────

func (m tuiModel) renderDetail(r model.Resource, width int) string {
	// Raw mode shows the resource exactly as the CLI's JSON output would —
	// every field the tool knows, ready to paste into a ticket.
	if m.detailRaw {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return "JSON error: " + err.Error()
		}
		header := detailSectionStyle().Render("RAW JSON") + "\n\n"
		// Soft-wrap long lines to the panel width; the viewport does not
		// scroll horizontally.
		return header + lipgloss.NewStyle().Width(width).Render(string(data))
	}

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

	if strings.ToLower(r.Service) == "diff" {
		b.WriteString(dSec.Render("DIFF FACT") + "\n\n")
		b.WriteString(dKey.Render(fmt.Sprintf("%-12s", "Diff Kind")) + " " + r.State + "\n")
		b.WriteString(dKey.Render(fmt.Sprintf("%-12s", "Resource ID")) + " " + r.ID + "\n")
		if r.Name != "" {
			b.WriteString(dKey.Render(fmt.Sprintf("%-12s", "Name")) + " " + r.Name + "\n")
		}
		b.WriteString(dKey.Render(fmt.Sprintf("%-12s", "Region")) + " " + r.Region + "\n")
		b.WriteString(dKey.Render(fmt.Sprintf("%-12s", "Type")) + " " + r.Type + "\n")

		if kind, ok := r.Details["diffKind"].(string); ok {
			switch kind {
			case "MODIFIED":
				if changes, ok := r.Details["changes"].([]string); ok && len(changes) > 0 {
					b.WriteString("\n" + dSec.Render("CHANGES DETECTED") + "\n\n")
					for _, c := range changes {
						b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Render("  ~ "+c) + "\n")
					}
				}
			case "ADDED":
				b.WriteString("\n" + dSec.Render("RESOURCE ADDED") + "\n")
				b.WriteString("  This resource exists in the new scan, but was not found in the old scan.\n")
			case "REMOVED":
				b.WriteString("\n" + dSec.Render("RESOURCE REMOVED") + "\n")
				b.WriteString("  This resource was found in the old scan, but is no longer present in the new scan.\n")
			}
		}
	} else {
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
	}

	// ── Cloud Support Toggleable Panes (Appended sections) ──

	if m.showTimeline {
		b.WriteString("\n" + dSec.Render("CLOUDTRAIL TIMELINE (LAST 90 DAYS)") + "\n\n")
		if m.timelineLoading {
			b.WriteString("  Loading events…\n")
		} else if m.timelineErr != nil {
			b.WriteString("  Error: " + m.timelineErr.Error() + "\n")
		} else if len(m.timelineEvents) == 0 {
			b.WriteString("  No recent CloudTrail mutations found.\n")
		} else {
			for _, ev := range m.timelineEvents {
				timeStr := ev.Time.Format("2006-01-02 15:04:05")
				b.WriteString(fmt.Sprintf("  %s · %s\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(timeStr),
					lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Render(ev.EventName)))
				b.WriteString(fmt.Sprintf("    %s %s · %s\n\n",
					dKey.Render("by"), ev.Principal, ev.SourceIP))
			}
		}
	}

	if m.showLogs {
		b.WriteString("\n" + dSec.Render("CLOUDWATCH RECENT ERROR LOGS") + "\n\n")
		if m.logsLoading {
			b.WriteString("  Loading logs…\n")
		} else if m.logsErr != nil {
			b.WriteString("  Error: " + m.logsErr.Error() + "\n")
		} else if len(m.logsLines) == 0 {
			b.WriteString("  No recent error logs found.\n")
		} else {
			for _, line := range m.logsLines {
				b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render(line) + "\n")
			}
		}
	}

	if m.showMetrics {
		ns, name, _, unit, hasMetric := metricParamsFor(r)
		header := "METRICS"
		if hasMetric {
			header = fmt.Sprintf("METRICS: %s (%s)", name, ns)
		}
		b.WriteString("\n" + dSec.Render(header) + "\n\n")
		if m.metricsLoading {
			b.WriteString("  Loading metric data…\n")
		} else if m.metricsErr != nil {
			b.WriteString("  Error: " + m.metricsErr.Error() + "\n")
		} else if m.metricsData == nil || len(m.metricsData.Values) == 0 {
			b.WriteString("  No datapoints in the last hour.\n")
		} else {
			vals := m.metricsData.Values
			spark := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(sparkline.Render(vals))
			stats, _ := sparkline.Summarize(vals)
			b.WriteString(fmt.Sprintf("  %s  (1h, 5m avg)\n", spark))
			b.WriteString(fmt.Sprintf("  now %s · max %s · min %s\n",
				sparkline.FormatValue(stats.Now, unit),
				sparkline.FormatValue(stats.Max, unit),
				sparkline.FormatValue(stats.Min, unit)))
		}
	}

	if m.showXref {
		b.WriteString("\n" + dSec.Render("CROSS-RESOURCE REFERENCES") + "\n\n")
		if len(m.xrefResources) == 0 {
			b.WriteString("  No references found in other resources.\n")
		} else {
			for _, xr := range m.xrefResources {
				b.WriteString(fmt.Sprintf("  - %s %s [%s]\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(xr.Service),
					xr.ID,
					xr.Type))
			}
		}
	}

	return b.String()
}

// ── Error renderer ────────────────────────────────────────────────────────────

// errorsBody formats the collected scan errors (access-denied first, then
// everything else) as a plain string, without any surrounding frame.
func (m tuiModel) errorsBody() string {
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
			msg := e.Message
			if e.Partial {
				msg += " Resources collected before the failure were kept."
			}
			b.WriteString(privilegeHintStyle().Render(msg) + "\n\n")
		}
		b.WriteString("Attach the missing permissions to your IAM user or role.\n")
	}
	if len(otherErrs) > 0 {
		if len(authErrs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Other errors:\n")
		for _, e := range otherErrs {
			code := e.Code
			if e.Partial {
				code += ", partial results kept"
			}
			b.WriteString(fmt.Sprintf("  [%s|%s] %s: %s\n", e.Service, e.Region, code, e.Message))
		}
	}
	return b.String()
}

// renderPrivilegeErrors renders the scan errors in a bordered box for the
// full-screen "nothing was returned" state.
func (m tuiModel) renderPrivilegeErrors() string {
	return privilegeErrorStyle().Render(m.errorsBody())
}

// openErrorsOverlay (re)builds the scrollable errors overlay sized to the
// current terminal so the access-denied details stay readable even when some
// resources were returned.
func (m *tuiModel) openErrorsOverlay() {
	w := m.width - 12
	if w > 80 {
		w = 80
	}
	if w < 32 {
		w = 32
	}
	h := m.height - 8
	if h < 6 {
		h = 6
	}
	m.errorsViewport = viewport.New(w, h)
	m.errorsViewport.SetContent(m.errorsBody())
	m.showErrors = true
}

// ── Debug activity overlay ("~") ───────────────────────────────────────────────

// openDebugOverlay (re)builds the scrollable debug overlay from the captured
// scan activity log and scrolls it to the latest line, so the most recent
// activity is visible the moment it opens.
func (m *tuiModel) openDebugOverlay() {
	// Leave a margin on every side: the pane floats over the live frame, so
	// the gap lets the table and the header's scanning progress show through
	// and visibly refresh in the background while the log is open.
	w := m.width - 20
	if w > 110 {
		w = 110
	}
	if w < 40 {
		w = 40
	}
	// A compact pane (about 15 lines) is enough — the user can scroll for the
	// rest — and keeping it short leaves more of the live table visible
	// behind it. Shrink further only when the terminal is too short to fit.
	h := 15
	if max := m.height - 12; h > max {
		h = max
	}
	if h < 6 {
		h = 6
	}
	m.debugViewport = viewport.New(w, h)
	m.debugViewport.SetContent(m.debugBody())
	m.debugViewport.GotoBottom()
	m.showDebug = true
}

// debugBody renders the captured activity log for the overlay viewport.
func (m tuiModel) debugBody() string {
	return ui.DebugBody(debuglog.Default.Entries(), debuglog.Default.Dropped())
}

// debugOverlay renders the debug activity overlay (title + scrollable body)
// inside a themed modal frame.
func (m tuiModel) debugOverlay() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf("DEBUG · TOOL ACTIVITY (%d lines)", debuglog.Default.Len()))
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll · g/G top/bottom · Esc/~ close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.debugViewport.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(0, 1).
		Render(body)
}

// ── Account snapshot diff ("d") ───────────────────────────────────────────────

// liveAcctSnapshot builds a snapshot of the current in-memory inventory. ok
// is false in the offline snapshot view, where there is no account identity
// to key the baseline by.
func (m tuiModel) liveAcctSnapshot() (acctsnap.Snapshot, bool) {
	if m.engine == nil {
		return acctsnap.Snapshot{}, false
	}
	return acctsnap.New(m.sorted, m.engine.AccountID, m.engine.EffectiveRegions()), true
}

// saveAcctBaseline saves the current inventory as the account baseline.
func (m *tuiModel) saveAcctBaseline() {
	live, ok := m.liveAcctSnapshot()
	if !ok {
		m.setToast("Account diff unavailable in offline snapshot view")
		return
	}
	if _, err := acctsnap.Save(live); err != nil {
		m.setToast("Failed to save baseline: " + err.Error())
		return
	}
	m.setToast("Account baseline saved — press D later to see what changed")
}

// openAcctDiff saves a baseline on first use, and diffs the live inventory
// against it on later uses, opening the scrollable overlay.
func (m *tuiModel) openAcctDiff() {
	live, ok := m.liveAcctSnapshot()
	if !ok {
		m.setToast("Account diff unavailable in offline snapshot view")
		return
	}
	baseline, found, err := acctsnap.Load(live.AccountID, live.Regions)
	if err != nil {
		m.setToast("Failed to load baseline: " + err.Error())
		return
	}
	if !found {
		m.saveAcctBaseline()
		return
	}

	m.acctDiffRep = acctsnap.NewReport(baseline, acctsnap.Diff(baseline, live))
	w := m.width - 12
	if w > 100 {
		w = 100
	}
	if w < 32 {
		w = 32
	}
	h := m.height - 8
	if h < 6 {
		h = 6
	}
	m.acctDiffVP = viewport.New(w, h)
	m.acctDiffVP.SetContent(m.acctDiffBody())
	m.showAcctDiff = true
}

// acctDiffBody renders the change list for the overlay viewport.
func (m tuiModel) acctDiffBody() string {
	if len(m.acctDiffRep.Changes) == 0 {
		return ui.SuccessStyle().Render("No changes since the baseline snapshot. ✓")
	}
	glyphStyles := map[string]lipgloss.Style{
		acctsnap.KindAdded:    lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess())).Bold(true),
		acctsnap.KindRemoved:  lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true),
		acctsnap.KindModified: lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Bold(true),
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	glyph := map[string]string{
		acctsnap.KindAdded: "+", acctsnap.KindRemoved: "-", acctsnap.KindModified: "~",
	}
	var b strings.Builder
	for _, c := range m.acctDiffRep.Changes {
		label := c.ID
		if c.Name != "" && c.Name != c.ID {
			label += " (" + c.Name + ")"
		}
		region := c.Region
		if region == "" {
			region = "global"
		}
		b.WriteString(fmt.Sprintf("%s %-22s %s  %s\n",
			glyphStyles[c.Kind].Render(glyph[c.Kind]), c.Type, label, muted.Render(region)))
		for _, d := range c.Deltas {
			b.WriteString("      " + d + "\n")
		}
	}
	return b.String()
}

// acctDiffOverlay renders the account-diff overlay (title + scrollable body)
// inside a themed modal frame.
func (m tuiModel) acctDiffOverlay() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf("WHAT CHANGED SINCE BASELINE %s — %d added, %d removed, %d modified",
			m.acctDiffRep.BaselineTakenAt, m.acctDiffRep.Added, m.acctDiffRep.Removed, m.acctDiffRep.Modified))
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll · b save current as new baseline · Esc/D close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.acctDiffVP.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(0, 1).
		Render(body)
}

// errorsOverlay renders the errors overlay (title + scrollable body) inside a
// themed modal frame.
func (m tuiModel) errorsOverlay() string {
	title := privilegeTitleStyle().Render(fmt.Sprintf("ACCESS / SCAN ERRORS (%d)", len(m.errors)))
	hint := privilegeHintStyle().Render("↑/↓ scroll · Esc/e close")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.errorsViewport.View(), "", hint)
	return privilegeErrorStyle().Render(body)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func waitForChunk(chunks <-chan model.ResultChunk, gen int) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-chunks
		if !ok {
			return doneMsg{gen: gen}
		}
		return chunkMsg{gen: gen, chunk: chunk}
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

// ── Cloud Support & Debugging Helpers ──────────────────────────────────────────

type timelineMsg struct {
	resourceID string
	events     []trail.Event
	err        error
}

type logsMsg struct {
	resourceID string
	lines      []string
	err        error
}

type metricsMsg struct {
	resourceID string
	data       *awsutil.SparklineMetric
	err        error
}

type watchTickMsg struct {
	timerID int
}

func logGroupFor(r model.Resource) string {
	switch strings.ToLower(r.Service) {
	case "lambda":
		return "/aws/lambda/" + r.Name
	case "ecs":
		return "/aws/ecs/" + r.Name
	case "rds":
		return "/aws/rds/instance/" + r.ID + "/error"
	}
	return ""
}

// metricParamsFor maps a resource to its headline CloudWatch metric: the one
// number that answers "is it healthy?" for that service. unit is a short
// display suffix ("%" for utilization, "" for plain counts). ok is false for
// services without a mapping.
func metricParamsFor(r model.Resource) (namespace, metricName string, dimensions map[string]string, unit string, ok bool) {
	dimensions = make(map[string]string)
	switch strings.ToLower(r.Service) {
	case "ec2":
		namespace = "AWS/EC2"
		metricName = "CPUUtilization"
		dimensions["InstanceId"] = r.ID
		unit = "%"
	case "rds":
		namespace = "AWS/RDS"
		metricName = "CPUUtilization"
		dimensions["DBInstanceIdentifier"] = r.ID
		unit = "%"
	case "lambda":
		namespace = "AWS/Lambda"
		metricName = "Errors"
		dimensions["FunctionName"] = r.Name
	case "sqs":
		namespace = "AWS/SQS"
		metricName = "ApproximateNumberOfMessagesVisible"
		dimensions["QueueName"] = r.Name
	case "dynamodb":
		namespace = "AWS/DynamoDB"
		metricName = "ThrottledRequests"
		dimensions["TableName"] = r.Name
	case "elbv2":
		// The LoadBalancer dimension is the ARN suffix after ":loadbalancer/"
		// (e.g. "app/my-lb/50dc6c495c0c9188").
		_, lbDim, found := strings.Cut(r.ARN, ":loadbalancer/")
		if !found {
			return "", "", nil, "", false
		}
		namespace = "AWS/ApplicationELB"
		metricName = "HTTPCode_Target_5XX_Count"
		dimensions["LoadBalancer"] = lbDim
	default:
		return "", "", nil, "", false
	}
	return namespace, metricName, dimensions, unit, true
}

func (m *tuiModel) saveSnapshot() {
	home, err := os.UserHomeDir()
	if err != nil {
		m.setToast("Failed to find home dir: " + err.Error())
		return
	}
	dir := home + "/.aws_explorer/snapshots"
	if err := os.MkdirAll(dir, 0755); err != nil {
		m.setToast("Failed to create snapshot dir: " + err.Error())
		return
	}
	profile := m.cfg.AWS.Profile
	if profile == "" {
		profile = "default"
	}
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s/snapshot-%s-%s.json", dir, profile, timestamp)

	data, err := json.MarshalIndent(m.sorted, "", "  ")
	if err != nil {
		m.setToast("Failed to marshal inventory: " + err.Error())
		return
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		m.setToast("Failed to write snapshot: " + err.Error())
		return
	}

	m.setToast("Saved snapshot to " + filename)
}

func (m *tuiModel) findReferences(resourceID string) []model.Resource {
	if resourceID == "" {
		return nil
	}
	var refs []model.Resource
	for _, r := range m.sorted {
		if r.ID == resourceID {
			continue // skip self
		}
		matched := false
		for _, v := range r.Summary {
			if strings.Contains(v, resourceID) {
				matched = true
				break
			}
		}
		if !matched {
			if data, err := json.Marshal(r.Details); err == nil {
				if strings.Contains(string(data), resourceID) {
					matched = true
				}
			}
		}
		if matched {
			refs = append(refs, r)
		}
	}
	return refs
}

// debugAWSConfig returns the credentials matching the resource's account so
// the debug panes query the right account during multi-account sweeps. ok is
// false when no engine is available (offline snapshot view).
func (m tuiModel) debugAWSConfig(r model.Resource) (aws.Config, bool) {
	if m.engine == nil {
		return aws.Config{}, false
	}
	return m.engine.AWSConfigFor(r.AccountID), true
}

func (m tuiModel) fetchTimelineCmd(res model.Resource) tea.Cmd {
	cfg, ok := m.debugAWSConfig(res)
	if !ok {
		return func() tea.Msg {
			return timelineMsg{resourceID: res.ID, err: fmt.Errorf("not available in offline snapshot view")}
		}
	}
	return func() tea.Msg {
		events, _, err := trail.Lookup(m.ctx, cfg, res.Region, res.ID, trail.Options{Limit: 20})
		return timelineMsg{resourceID: res.ID, events: events, err: err}
	}
}

// cwJumpDoneMsg is delivered after the suspended cw TUI exits, carrying any
// launch error so the parent can surface it.
type cwJumpDoneMsg struct{ err error }

// jumpToLogsCmd suspends the summary TUI and runs the cw Logs TUI as a child
// process of this same binary, pre-filtered to group in region. tea.ExecProcess
// hands the terminal to the child and restores it on exit. Credentials follow
// from the same --profile/--config the summary TUI is using.
func (m tuiModel) jumpToLogsCmd(region, group string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return cwJumpDoneMsg{err: err} }
	}
	args := []string{"cw", "--group", group}
	if region != "" && region != "global" {
		args = append(args, "--region", region)
	}
	if m.cfg != nil && m.cfg.AWS.Profile != "" {
		args = append(args, "--profile", m.cfg.AWS.Profile)
	}
	if m.configPath != "" {
		args = append(args, "--config", m.configPath)
	}
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return cwJumpDoneMsg{err: err}
	})
}

func (m tuiModel) fetchLogsCmd(res model.Resource, logGroupName string) tea.Cmd {
	cfg, ok := m.debugAWSConfig(res)
	if !ok {
		return func() tea.Msg {
			return logsMsg{resourceID: res.ID, err: fmt.Errorf("not available in offline snapshot view")}
		}
	}
	return func() tea.Msg {
		lines, err := awsutil.FetchRecentLogs(m.ctx, cfg, res.Region, logGroupName, "ERROR")
		return logsMsg{resourceID: res.ID, lines: lines, err: err}
	}
}

func (m tuiModel) fetchMetricsCmd(res model.Resource, namespace, metricName string, dimensions map[string]string) tea.Cmd {
	cfg, ok := m.debugAWSConfig(res)
	if !ok {
		return func() tea.Msg {
			return metricsMsg{resourceID: res.ID, err: fmt.Errorf("not available in offline snapshot view")}
		}
	}
	return func() tea.Msg {
		data, err := awsutil.FetchMetricData(m.ctx, cfg, res.Region, namespace, metricName, dimensions)
		return metricsMsg{resourceID: res.ID, data: data, err: err}
	}
}

func (m tuiModel) watchTick(d time.Duration, id int) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return watchTickMsg{timerID: id}
	})
}
