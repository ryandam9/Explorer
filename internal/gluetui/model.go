package gluetui

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// defaultGlueLogGroup is the base CloudWatch log group Glue jobs write to when
// a run reports no explicit group; as a cw --group prefix it matches the
// output/error/logs-v2 children.
const defaultGlueLogGroup = "/aws-glue/jobs"

type tab int

const (
	tabJobs tab = iota
	tabCrawlers
	tabTriggers
	tabWorkflows
	tabConnections
	tabDatabases
	tabCount
)

var tabNames = [tabCount]string{"Jobs", "Crawlers", "Triggers", "Workflows", "Connections", "Catalog"}

// runWindow caps how many runs the history view fetches per job.
const runWindow = 20

// Fetch deadlines bound every load so a slow or hung AWS call surfaces a
// retryable error instead of spinning forever. Inventory fans out across every
// region with per-job enrichment, so it gets generous headroom; the per-job
// drill-downs are single calls and get less.
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

	filter       textinput.Model
	filterActive bool

	// Run-history sub-view (Enter on a job).
	runsActive  bool
	runsJob     Job
	runs        []JobRun
	runsLoading bool
	runsErr     error
	runsTbl     table.Model

	// Job-definition overlay (d on a job).
	defActive  bool
	def        JobDef
	defLoading bool
	defErr     error

	spinner   spinner.Model
	toast     string
	toastExp  time.Time
	showAbout bool
}

type invMsg struct {
	inv Inventory
	err error
}

type runsMsg struct {
	job  Job
	runs []JobRun
	err  error
}

type defMsg struct {
	def JobDef
	err error
}

// cwJumpDoneMsg is delivered after the suspended cw TUI exits.
type cwJumpDoneMsg struct{ err error }

type clearToastMsg struct{}

// NewModel builds the Glue dashboard over one or more regions. configPath is
// passed through to the child cw process for the run-logs jump (AXE-028).
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
		ctx:        ctx,
		client:     client,
		regions:    activeRegions,
		allRegions: allRegions,
		appCfg:     appCfg,
		configPath: configPath,
		filter:     f,
		spinner:    s,
		tbl:        newGlueTable(tabColumns(tabJobs, len(activeRegions) > 1)),
		runsTbl:    newGlueTable(runColumns()),
		loading:    true,
	}, nil
}

// rebuild recomputes the active tab's filtered rows and pushes them into the
// shared table, swapping in the tab's columns and restoring its remembered
// cursor.
func (mm *m) rebuild() {
	mm.view = mm.buildView()
	mm.tbl.SetColumns(tabColumns(mm.tab, len(mm.regions) > 1))
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
	mm.rebuild()
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.loadInventoryCmd())
}

func (mm *m) loadInventoryCmd() tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue inventory", "regions", len(mm.regions))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		inv, err := mm.client.LoadInventory(ctx)
		return invMsg{inv: inv, err: err}
	}
}

func (mm *m) loadRunsCmd(job Job) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue job runs", "job", job.Name, "region", job.Region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		runs, err := mm.client.JobRuns(ctx, job.Region, job.Name, runWindow)
		return runsMsg{job: job, runs: runs, err: err}
	}
}

func (mm *m) loadDefCmd(job Job) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue job definition", "job", job.Name, "region", job.Region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		def, err := mm.client.JobDefinition(ctx, job.Region, job.Name)
		return defMsg{def: def, err: err}
	}
}

// jumpToRunLogsCmd suspends the dashboard and runs the cw Logs TUI as a child
// of this same binary, pre-filtered to the run's Glue log group and its
// JobRunId stream (AXE-028). Continuous logging writes "<runId>" and
// "<runId>-driver" streams under /aws-glue/jobs/logs-v2; legacy logging writes
// the run ID under /aws-glue/jobs/{output,error}. The group prefix + run-ID
// stream filter match either layout.
func (mm *m) jumpToRunLogsCmd(run JobRun) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return cwJumpDoneMsg{err: err} }
	}
	var profile string
	if mm.appCfg != nil {
		profile = mm.appCfg.AWS.Profile
	}
	args := cwJumpArgs(run.LogGroup, run.ID, mm.runsJob.Region, profile, mm.configPath)
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return cwJumpDoneMsg{err: err}
	})
}

// cwJumpArgs builds the argv for the child `cw` invocation that opens a Glue
// run's logs. Pure, so it is table-tested. An empty group falls back to the
// Glue base group (a cw --group prefix matching output/error/logs-v2).
func cwJumpArgs(group, runID, region, profile, configPath string) []string {
	if group == "" {
		group = defaultGlueLogGroup
	}
	args := []string{"cw", "--group", group}
	if runID != "" {
		args = append(args, "--stream", runID)
	}
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
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.rebuild()
		}

	case runsMsg:
		mm.runsLoading = false
		mm.runsErr = msg.err
		mm.runs = msg.runs
		rows := make([]table.Row, 0, len(msg.runs))
		for _, r := range msg.runs {
			rows = append(rows, runRow(r))
		}
		mm.runsTbl.SetRows(rows)
		mm.runsTbl.SetCursor(0)

	case defMsg:
		mm.defLoading = false
		mm.defErr = msg.err
		mm.def = msg.def

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
			mm.loading = true
			cmds = append(cmds, mm.loadInventoryCmd())
		}
		return cmds
	}

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Job-definition overlay: any key closes it (q still quits). Checked before
	// the other guards since it floats over the dashboard.
	if mm.defActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		default:
			mm.defActive = false
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

	// Run-history sub-view.
	if mm.runsActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.runsActive = false
		case "up", "k":
			mm.runsTbl.MoveUp(1)
		case "down", "j":
			mm.runsTbl.MoveDown(1)
		case "g", "home":
			mm.runsTbl.GotoTop()
		case "G", "end":
			mm.runsTbl.GotoBottom()
		case "<", ",":
			mm.runsTbl.ScrollLeft()
		case ">", ".":
			mm.runsTbl.ScrollRight()
		case "y":
			if r, ok := mm.selectedRun(); ok && r.Error != "" {
				_ = clipboard.WriteAll(r.Error)
				mm.setToast("Copied error message")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case "L":
			if r, ok := mm.selectedRun(); ok {
				cmds = append(cmds, mm.jumpToRunLogsCmd(r))
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
		mm.loading = true
		mm.inv = Inventory{}
		cmds = append(cmds, mm.loadInventoryCmd(), mm.spinner.Tick)
	case "enter":
		if mm.tab == tabJobs {
			if job, ok := mm.selectedJob(); ok {
				mm.runsActive = true
				mm.runsJob = job
				mm.runsLoading = true
				mm.runs = nil
				mm.runsErr = nil
				cmds = append(cmds, mm.loadRunsCmd(job), mm.spinner.Tick)
			}
		}
	case "d":
		if mm.tab == tabJobs {
			if job, ok := mm.selectedJob(); ok {
				mm.defActive = true
				mm.defLoading = true
				mm.def = JobDef{}
				mm.defErr = nil
				cmds = append(cmds, mm.loadDefCmd(job), mm.spinner.Tick)
			}
		}
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
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
	base := "AWS Glue"
	if mm.runsActive {
		return base + " › " + mm.runsJob.Name + " › runs"
	}
	return base + " › " + tabNames[mm.tab]
}
