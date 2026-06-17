package lambdatui

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

type tab int

const (
	tabFunctions tab = iota
	tabLayers
	tabEventSources
	tabCount
)

var tabNames = [tabCount]string{"Functions", "Layers", "Event sources"}

// Fetch deadlines bound every load so a slow or hung AWS call surfaces a
// retryable error instead of spinning forever. The inventory fans out across
// every region; the per-function detail is a single call and gets less.
const (
	inventoryTimeout = 2 * time.Minute
	drillTimeout     = 45 * time.Second
)

type m struct {
	ctx        context.Context
	client     *Client
	regions    []string
	allRegions bool
	appCfg     *config.Config
	configPath string

	width, height int

	inv     Inventory
	loading bool
	err     error

	tab tab
	// sel remembers each tab's cursor so switching tabs restores the selection.
	sel  [tabCount]int
	tbl  table.Model
	view []rowT // active tab's filtered rows, parallel to the table's rows

	// Column sort for the active tab: sortCol -1 keeps the natural (name, region)
	// order; otherwise it indexes the tab's columns and sortAsc flips direction.
	// It resets to natural order on a tab switch (each tab has its own columns).
	sortCol int
	sortAsc bool

	filter       textinput.Model
	filterActive bool

	// Resource-detail overlay (Enter on a row). Functions fetch GetFunction on
	// demand (detailLoading true until the detailMsg lands); layers and event
	// sources render synchronously from the loaded inventory.
	detailActive  bool
	detailTitle   string
	detail        ResourceDetail
	detailLoading bool
	detailErr     error
	overlayVP     viewport.Model

	// Findings panel (f) — deterministic runtime/health checks over the loaded
	// functions, computed synchronously (no AWS call).
	findingsActive bool
	findingList    []findings.Finding
	findingsTbl    table.Model

	// loadGen tags each load so a refresh's stragglers can't patch a newer load.
	loadGen int

	spinner   spinner.Model
	toast     string
	toastExp  time.Time
	showAbout bool
}

type invMsg struct {
	gen int
	inv Inventory
	err error
}

type detailMsg struct {
	detail ResourceDetail
	err    error
}

// cwJumpDoneMsg is delivered after the suspended cw TUI exits.
type cwJumpDoneMsg struct{ err error }

type clearToastMsg struct{}

// NewModel builds the Lambda dashboard over one or more regions. configPath is
// passed through to the child cw process for the logs jump.
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool, appCfg *config.Config, configPath string) (tea.Model, error) {
	client, err := NewClient(ctx, awsCfg, regions, allRegions)
	if err != nil {
		return nil, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	f := textinput.New()
	f.Placeholder = "Filter…"
	f.Width = 30

	activeRegions := client.Regions()
	return &m{
		ctx:         ctx,
		client:      client,
		regions:     activeRegions,
		allRegions:  allRegions,
		appCfg:      appCfg,
		configPath:  configPath,
		filter:      f,
		spinner:     s,
		tbl:         newLambdaTable(tabColumns(tabFunctions, len(activeRegions) > 1)),
		findingsTbl: newLambdaTable(findingsColumns(len(activeRegions) > 1)),
		loading:     true,
		sortCol:     -1,
	}, nil
}

// rebuild recomputes the active tab's filtered rows and pushes them into the
// shared table, swapping in the tab's columns and restoring its cursor.
func (mm *m) rebuild() {
	mm.view = mm.buildView()
	cols := tabColumns(mm.tab, len(mm.regions) > 1)
	if mm.sortCol >= len(cols) {
		mm.sortCol = -1
	}
	table.ApplySortHeader(cols, mm.sortCol, mm.sortAsc, func(int) bool { return true })
	mm.tbl.SetColumns(cols)
	rows := make([]table.Row, 0, len(mm.view))
	for _, r := range mm.view {
		rows = append(rows, r.cells)
	}
	mm.tbl.SetRows(rows)
	if mm.sel[mm.tab] >= len(rows) {
		mm.sel[mm.tab] = max(0, len(rows)-1)
	}
	mm.tbl.SetCursor(mm.sel[mm.tab])
}

// switchTab moves to the next/previous tab, remembering the current cursor and
// restoring the destination tab's.
func (mm *m) switchTab(next bool) {
	mm.sel[mm.tab] = mm.tbl.Cursor()
	if next {
		mm.tab = (mm.tab + 1) % tabCount
	} else {
		mm.tab = (mm.tab + tabCount - 1) % tabCount
	}
	mm.filter.SetValue("")
	mm.filterActive = false
	mm.filter.Blur()
	mm.sortCol = -1 // each tab has its own columns; start in natural order
	mm.rebuild()
}

// cycleSort advances the active tab's sort: natural order → each column in turn
// → back to natural order. Each column starts ascending; press R to reverse.
func (mm *m) cycleSort() {
	mm.sortCol++
	if mm.sortCol >= len(tabColumns(mm.tab, len(mm.regions) > 1)) {
		mm.sortCol = -1
	}
	mm.sortAsc = true
	mm.sel[mm.tab] = 0
	mm.tbl.SetCursor(0)
	mm.rebuild()
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.beginLoad())
}

// beginLoad starts a fresh load: it bumps the generation, clears the inventory
// and returns the load command.
func (mm *m) beginLoad() tea.Cmd {
	mm.loadGen++
	mm.loading = true
	mm.inv = Inventory{}
	return mm.loadInventoryCmd(mm.loadGen)
}

func (mm *m) loadInventoryCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Listing Lambda resources", "regions", len(mm.regions))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		inv, err := mm.client.LoadInventory(ctx)
		return invMsg{gen: gen, inv: inv, err: err}
	}
}

func (mm *m) loadDetailCmd(region, name string) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Lambda function detail", "function", name, "region", region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		d, err := mm.client.FunctionDetail(ctx, region, name)
		return detailMsg{detail: d, err: err}
	}
}

// jumpToLogsCmd suspends the dashboard and runs the cw Logs TUI as a child of
// this same binary, pre-filtered to the function's CloudWatch log group.
func (mm *m) jumpToLogsCmd(group, region string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return cwJumpDoneMsg{err: err} }
	}
	var profile string
	if mm.appCfg != nil {
		profile = mm.appCfg.AWS.Profile
	}
	args := cwJumpArgs(group, region, profile, mm.configPath)
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return cwJumpDoneMsg{err: err}
	})
}

// cwJumpArgs builds the argv for the child `cw` invocation that opens a
// function's logs. Pure, so it is table-tested.
func cwJumpArgs(group, region, profile, configPath string) []string {
	args := []string{"cw", "--group", group}
	if region != "" && region != "global" {
		args = append(args, "--region", region)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return args
}

func toastCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func (mm *m) setToast(s string) {
	mm.toast = s
	mm.toastExp = time.Now().Add(3 * time.Second)
}

func (mm *m) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		mm.width = msg.Width
		mm.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		mm.spinner, cmd = mm.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		mm.toast = ""

	case cwJumpDoneMsg:
		if msg.err != nil {
			mm.setToast("Could not open logs: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
		}

	case invMsg:
		if msg.gen != mm.loadGen {
			break // a newer load superseded this one
		}
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.rebuild()
		}

	case detailMsg:
		mm.detailLoading = false
		mm.detailErr = msg.err
		mm.detail = msg.detail
		if msg.err == nil {
			mm.overlayVP.GotoTop()
		}

	case tea.KeyMsg:
		cmds = append(cmds, mm.handleKey(msg)...)
	}

	return mm, tea.Batch(cmds...)
}

func (mm *m) handleKey(msg tea.KeyMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Error screen: Enter/Esc retries, q quits.
	if mm.err != nil {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "enter", "esc":
			mm.err = nil
			cmds = append(cmds, mm.beginLoad(), mm.spinner.Tick)
		}
		return cmds
	}

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Resource-detail overlay: scrollable; Esc/Enter close, q quits. Checked
	// before the other guards since it floats over the dashboard.
	if mm.detailActive {
		if mm.closeOrScrollOverlay(msg, mm.detailLoading, &mm.detailActive) {
			return []tea.Cmd{tea.Quit}
		}
		return cmds
	}

	// Filter input captures keys while active.
	if mm.filterActive {
		switch msg.String() {
		case "enter", "esc":
			if msg.String() == "esc" {
				mm.filter.SetValue("")
			}
			mm.filterActive = false
			mm.filter.Blur()
			mm.rebuild()
		default:
			var cmd tea.Cmd
			mm.filter, cmd = mm.filter.Update(msg)
			cmds = append(cmds, cmd)
			mm.rebuild()
		}
		return cmds
	}

	// Findings panel sub-view.
	if mm.findingsActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left":
			mm.findingsActive = false
		case "up", "k":
			mm.findingsTbl.MoveUp(1)
		case "down", "j":
			mm.findingsTbl.MoveDown(1)
		case "g", "home":
			mm.findingsTbl.GotoTop()
		case "G", "end":
			mm.findingsTbl.GotoBottom()
		case "<", ",":
			mm.findingsTbl.ScrollLeft()
		case ">", ".":
			mm.findingsTbl.ScrollRight()
		case "y":
			if f, ok := mm.selectedFinding(); ok && f.Fix != "" {
				_ = clipboard.WriteAll(f.Fix)
				mm.setToast("Copied suggested fix")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Dashboard.
	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "tab", "right", "l":
		mm.switchTab(true)
	case "shift+tab", "left", "h":
		mm.switchTab(false)
	case "up", "k":
		mm.tbl.MoveUp(1)
	case "down", "j":
		mm.tbl.MoveDown(1)
	case "g", "home":
		mm.tbl.GotoTop()
	case "G", "end":
		mm.tbl.GotoBottom()
	case "<", ",":
		mm.tbl.ScrollLeft()
	case ">", ".":
		mm.tbl.ScrollRight()
	case "/":
		mm.filterActive = true
		mm.filter.Focus()
	case "r":
		mm.startReload(&cmds)
	case "enter":
		mm.openDetail(&cmds)
	case "f":
		mm.openFindings()
	case "S":
		mm.cycleSort()
	case "R":
		if mm.sortCol >= 0 {
			mm.sortAsc = !mm.sortAsc
			mm.tbl.SetCursor(0)
			mm.rebuild()
		}
	case "L":
		if fn, ok := mm.selectedFunction(); ok {
			cmds = append(cmds, mm.jumpToLogsCmd(fn.LogGroup, fn.Region))
		}
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
}

// openDetail opens the detail overlay for the selected row. Functions fetch
// GetFunction on demand; layers and event sources render straight from the
// loaded inventory.
func (mm *m) openDetail(cmds *[]tea.Cmd) {
	r, ok := mm.selectedRow()
	if !ok {
		return
	}
	switch r.typ {
	case "function":
		mm.detailActive = true
		mm.detailLoading = true
		mm.detail = ResourceDetail{}
		mm.detailErr = nil
		mm.detailTitle = "Function — " + r.name
		*cmds = append(*cmds, mm.loadDetailCmd(r.region, r.name), mm.spinner.Tick)
	case "layer":
		if r.layer == nil {
			return
		}
		mm.detailActive = true
		mm.detailLoading = false
		mm.detailErr = nil
		mm.detail = buildLayerDetail(*r.layer)
		mm.detailTitle = mm.detail.Title
		mm.overlayVP.GotoTop()
	case "event-source-mapping":
		if r.es == nil {
			return
		}
		mm.detailActive = true
		mm.detailLoading = false
		mm.detailErr = nil
		mm.detail = buildEventSourceDetail(*r.es)
		mm.detailTitle = mm.detail.Title
		mm.overlayVP.GotoTop()
	}
}

// startReload kicks off an inventory reload unless one is already running, so a
// double-press of r can't fire concurrent loads.
func (mm *m) startReload(cmds *[]tea.Cmd) {
	if mm.loading {
		return
	}
	*cmds = append(*cmds, mm.beginLoad(), mm.spinner.Tick)
}

// closeOrScrollOverlay handles keys for the scrollable detail overlay: q/ctrl+c
// signals quit (returns true); Esc/Enter close it; the rest scroll once content
// has loaded.
func (mm *m) closeOrScrollOverlay(msg tea.KeyMsg, loading bool, active *bool) bool {
	switch msg.String() {
	case "q", "ctrl+c":
		return true
	case "esc", "enter", "backspace", "left":
		*active = false
	case "up", "k":
		if !loading {
			mm.overlayVP.LineUp(1)
		}
	case "down", "j":
		if !loading {
			mm.overlayVP.LineDown(1)
		}
	case "pgup":
		if !loading {
			mm.overlayVP.ViewUp()
		}
	case "pgdown", "pgdn", " ":
		if !loading {
			mm.overlayVP.ViewDown()
		}
	case "g", "home":
		if !loading {
			mm.overlayVP.GotoTop()
		}
	case "G", "end":
		if !loading {
			mm.overlayVP.GotoBottom()
		}
	}
	return false
}

// layoutOverlayVP sizes the shared overlay viewport to the terminal, preserving
// the scroll offset, and wraps content to the viewport width so long values
// fold instead of running off the edge.
func (mm *m) layoutOverlayVP(content string) {
	w := ui.AboutWidth(mm.width) - 4 // the box pads 2 columns on each side
	if w < 28 {
		w = 28
	}
	h := mm.height - 12 // border + padding + title + hint + centering margins
	if h < 6 {
		h = 6
	}
	off := mm.overlayVP.YOffset
	mm.overlayVP = viewport.New(w, h)
	mm.overlayVP.SetContent(lipgloss.NewStyle().Width(w).Render(content))
	mm.overlayVP.SetYOffset(off)
}

// openFindings computes the deterministic findings over the loaded functions
// and opens the panel. Synchronous — no AWS call — so there is no loading state.
func (mm *m) openFindings() {
	mm.findingList = mm.computeFindings()
	mm.findingsActive = true
	multi := len(mm.regions) > 1
	mm.findingsTbl.SetColumns(findingsColumns(multi))
	rows := make([]table.Row, 0, len(mm.findingList))
	for _, f := range mm.findingList {
		rows = append(rows, findingRow(f, multi))
	}
	mm.findingsTbl.SetRows(rows)
	mm.findingsTbl.SetCursor(0)
}

// openConsole copies (and opens, when local) the console URL for the selected
// row.
func (mm *m) openConsole(cmds *[]tea.Cmd) {
	res, ok := mm.selectedResource()
	if !ok {
		return
	}
	url, _ := consolelink.URL(res)
	_ = clipboard.WriteAll(url)
	if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
		mm.setToast("Opened in browser · copied console URL")
	} else {
		mm.setToast("Copied console URL")
	}
	*cmds = append(*cmds, toastCmd(3*time.Second))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (mm *m) PageTitle() string {
	base := "AWS Lambda"
	if mm.findingsActive {
		return base + " › Findings"
	}
	return base + " › " + tabNames[mm.tab]
}
