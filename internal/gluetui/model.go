package gluetui

import (
	"context"
	"fmt"
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

	// Column sort for the active tab: sortCol -1 keeps the natural (name, region)
	// order; otherwise it indexes the tab's columns and sortAsc flips direction.
	// It resets to natural order on a tab switch (each tab has its own columns).
	sortCol int
	sortAsc bool

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

	// Resource-detail overlay (Enter on a crawler/trigger/workflow/connection/
	// database). Fetched on demand, one Get* call per resource type.
	detailActive  bool
	detailTitle   string
	detail        ResourceDetail
	detailLoading bool
	detailErr     error

	// overlayVP scrolls whichever detail overlay (job definition or resource
	// detail) is open, so long content (default arguments, connection rows) is
	// reachable instead of overflowing. The two overlays are never open at once.
	overlayVP viewport.Model

	// Findings panel (f) — deterministic posture/cost checks over the loaded
	// inventory. findingList is computed synchronously (no AWS call), parallel to
	// the table's rows.
	findingsActive bool
	findingList    []findings.Finding
	findingsTbl    table.Model

	// Progressive load: ListSkeleton fills the tabs fast (phase 1, jobs without
	// their last-run state), then each region's jobs are enriched in the
	// background (phase 2). loadGen tags each load so a refresh's stragglers can't
	// patch a newer load; enrichPending counts the regions still enriching.
	loadGen       int
	enrichPending int

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

// enrichMsg delivers one region's enriched jobs (last-run state) during phase 2.
type enrichMsg struct {
	gen    int
	region string
	jobs   []Job
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

type detailMsg struct {
	detail ResourceDetail
	err    error
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
		ctx:         ctx,
		client:      client,
		regions:     activeRegions,
		allRegions:  allRegions,
		appCfg:      appCfg,
		configPath:  configPath,
		filter:      f,
		spinner:     s,
		tbl:         newGlueTable(tabColumns(tabJobs, len(activeRegions) > 1)),
		runsTbl:     newGlueTable(runColumns()),
		findingsTbl: newGlueTable(findingsColumns(len(activeRegions) > 1)),
		loading:     true,
		sortCol:     -1,
	}, nil
}

// rebuild recomputes the active tab's filtered rows and pushes them into the
// shared table, swapping in the tab's columns and restoring its remembered
// cursor.
func (mm *m) rebuild() {
	mm.view = mm.buildView()
	cols := tabColumns(mm.tab, len(mm.regions) > 1)
	if mm.sortCol >= len(cols) {
		mm.sortCol = -1 // a narrower tab can't keep a wider tab's sort column
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

// beginLoad starts a fresh progressive load: it bumps the generation, clears the
// inventory and returns the phase-1 (list skeleton) command.
func (mm *m) beginLoad() tea.Cmd {
	mm.loadGen++
	mm.loading = true
	mm.inv = Inventory{}
	mm.enrichPending = 0
	return mm.loadInventoryCmd(mm.loadGen)
}

// loadInventoryCmd is phase 1: list every resource (jobs without their last-run
// state) so the tabs appear immediately.
func (mm *m) loadInventoryCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Listing Glue resources", "regions", len(mm.regions))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		inv, err := mm.client.ListSkeleton(ctx)
		return invMsg{gen: gen, inv: inv, err: err}
	}
}

// enrichCmds is phase 2: one enrichment command per region with jobs, each
// streaming that region's enriched jobs back via an enrichMsg.
func (mm *m) enrichCmds(gen int) []tea.Cmd {
	byRegion := map[string][]Job{}
	for _, j := range mm.inv.Jobs {
		byRegion[j.Region] = append(byRegion[j.Region], j)
	}
	cmds := make([]tea.Cmd, 0, len(byRegion))
	for region, jobs := range byRegion {
		cmds = append(cmds, mm.enrichRegionCmd(gen, region, jobs))
	}
	mm.enrichPending = len(cmds)
	return cmds
}

func (mm *m) enrichRegionCmd(gen int, region string, jobs []Job) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Enriching Glue jobs", "region", region, "count", len(jobs))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		return enrichMsg{gen: gen, region: region, jobs: mm.client.EnrichRegion(ctx, region, jobs)}
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

// loadDetailCmd fetches the on-demand detail for the selected non-job resource,
// dispatching to the per-type Get* call. Best-effort: a denied call surfaces as
// an error inside the overlay rather than aborting the dashboard.
func (mm *m) loadDetailCmd(typ, region, name string) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading Glue resource detail", "type", typ, "name", name, "region", region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		var (
			d   ResourceDetail
			err error
		)
		switch typ {
		case "crawler":
			d, err = mm.client.CrawlerDetail(ctx, region, name)
		case "trigger":
			d, err = mm.client.TriggerDetail(ctx, region, name)
		case "workflow":
			d, err = mm.client.WorkflowDetail(ctx, region, name)
		case "connection":
			d, err = mm.client.ConnectionDetail(ctx, region, name)
		case "database":
			d, err = mm.client.DatabaseDetail(ctx, region, name)
		default:
			err = fmt.Errorf("no detail available for %q", typ)
		}
		return detailMsg{detail: d, err: err}
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
	args := cwJumpArgs(run.LogGroup, run.ID, mm.runsJob.Region, profile, ui.ConfigArgPath(mm.configPath))
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
		if msg.gen != mm.loadGen {
			break // a newer load superseded this one
		}
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.rebuild()
			// Phase 2: enrich each region's jobs in the background.
			cmds = append(cmds, mm.enrichCmds(msg.gen)...)
		}

	case enrichMsg:
		if msg.gen != mm.loadGen {
			break // stragglers from a superseded load
		}
		mm.applyEnrichment(msg)

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
		if msg.err == nil {
			mm.overlayVP.GotoTop() // new content starts at the top; render fills it
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

	// Job-definition overlay: scrollable; Esc/d/Enter close, q quits. Checked
	// before the other guards since it floats over the dashboard.
	if mm.defActive {
		if mm.closeOrScrollOverlay(msg, mm.defLoading, &mm.defActive) {
			return []tea.Cmd{tea.Quit}
		}
		return cmds
	}

	// Resource-detail overlay: scrollable; Esc/Enter close, q quits.
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
		if mm.tab == tabJobs {
			if job, ok := mm.selectedJob(); ok {
				mm.runsActive = true
				mm.runsJob = job
				mm.runsLoading = true
				mm.runs = nil
				mm.runsErr = nil
				cmds = append(cmds, mm.loadRunsCmd(job), mm.spinner.Tick)
			}
		} else if r, ok := mm.selectedRow(); ok {
			// Crawlers, triggers, workflows, connections and databases drill into a
			// detail overlay fetched on demand (issue #238).
			mm.detailActive = true
			mm.detailLoading = true
			mm.detail = ResourceDetail{}
			mm.detailErr = nil
			mm.detailTitle = detailTitleFor(r.typ, r.name)
			cmds = append(cmds, mm.loadDetailCmd(r.typ, r.region, r.name), mm.spinner.Tick)
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
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
}

// startReload kicks off an inventory reload, unless one is already running, so
// a double-press of r can't fire concurrent loads that race to overwrite the
// inventory and double the API traffic.
func (mm *m) startReload(cmds *[]tea.Cmd) {
	if mm.loading {
		return
	}
	*cmds = append(*cmds, mm.beginLoad(), mm.spinner.Tick)
}

// applyEnrichment patches one region's enriched jobs back into the inventory by
// (region, name), decrements the pending-region counter and re-renders.
func (mm *m) applyEnrichment(msg enrichMsg) {
	byName := make(map[string]Job, len(msg.jobs))
	for _, j := range msg.jobs {
		byName[j.Name] = j
	}
	for i := range mm.inv.Jobs {
		if mm.inv.Jobs[i].Region != msg.region {
			continue
		}
		if e, ok := byName[mm.inv.Jobs[i].Name]; ok {
			mm.inv.Jobs[i] = e
		}
	}
	if mm.enrichPending > 0 {
		mm.enrichPending--
	}
	mm.rebuild()
}

// closeOrScrollOverlay handles keys for a scrollable detail overlay: q/ctrl+c
// signals quit (returns true); Esc/Enter close it; the rest scroll the shared
// viewport once content has loaded. Shared by the job-definition and
// resource-detail overlays.
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

// openFindings computes the deterministic findings over the loaded inventory
// (jobs and crawlers, across every loaded tab/region) and opens the panel.
// Synchronous — no AWS call — so there is no loading state.
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
	base := "AWS Glue"
	if mm.runsActive {
		return base + " › " + mm.runsJob.Name + " › runs"
	}
	if mm.findingsActive {
		return base + " › Findings"
	}
	return base + " › " + tabNames[mm.tab]
}
