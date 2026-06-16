package emrtui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/emrconn"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// stepWindow caps how many steps the history view fetches per cluster.
const stepWindow = 50

// Fetch deadlines bound every load so a slow or hung AWS/on-cluster call
// surfaces a retryable error instead of spinning forever. Inventory fans out
// across every region with per-cluster enrichment, so it gets generous
// headroom; an on-cluster full-table row count can legitimately run long, so it
// gets the most.
const (
	inventoryTimeout = 2 * time.Minute
	drillTimeout     = 45 * time.Second
	scanTimeout      = 10 * time.Minute
)

type m struct {
	ctx        context.Context
	client     *Client
	regions    []string
	allRegions bool
	appCfg     *config.Config
	configPath string

	// On-cluster connection layer (AXE-039); dialer is nil when off/misconfigured.
	dialer    *emrconn.Dialer
	dialerErr error

	width, height int

	inv     Inventory
	loading bool
	err     error

	// Cluster list backed by the shared table widget. view holds the clusters
	// currently shown (filtered + sorted), parallel to the table's rows, so the
	// cursor maps straight back to a Cluster.
	tbl  table.Model
	view []Cluster

	// Column sort for the cluster list: sortCol -1 keeps the natural (name,
	// region) order; otherwise it is an index into the cluster columns and
	// sortAsc flips the direction.
	sortCol int
	sortAsc bool

	// showTerminated includes the terminated cluster tail in the inventory. Off
	// by default so the list shows only live clusters; the "t" key toggles it and
	// reloads.
	showTerminated bool

	filter       textinput.Model
	filterActive bool

	// Step-history sub-view (Enter on a cluster).
	stepsActive  bool
	stepsCluster Cluster
	steps        []Step
	stepsLoading bool
	stepsErr     error
	stepsTbl     table.Model

	// Cluster-detail overlay (d on a cluster).
	detailActive  bool
	detailCluster Cluster

	// Persistent application-UI picker (u on a cluster).
	appUIActive  bool
	appUICluster Cluster
	appUISel     int
	appUILoading bool

	// Live YARN application browser (y on a cluster).
	yarnActive  bool
	yarnCluster Cluster
	yarnApps    []YarnApp
	yarnMetrics ClusterMetrics
	yarnLoading bool
	yarnErr     error
	yarnTbl     table.Model

	// HBase table browser (h on a cluster).
	hbaseActive   bool
	hbaseCluster  Cluster
	hbaseTables   []HBaseTable
	hbaseLoading  bool
	hbaseErr      error
	hbaseTbl      table.Model
	hbaseConfirm  bool // row-count scan confirmation prompt
	hbaseCounting bool

	// Oozie workflow/coordinator browser (z on a cluster).
	oozieActive  bool
	oozieCluster Cluster
	oozieWF      []OozieWorkflow
	oozieCoord   []OozieCoordinator
	oozieCoords  bool // false = workflows tab, true = coordinators tab
	oozieLoading bool
	oozieErr     error
	oozieTbl     table.Model

	spinner   spinner.Model
	toast     string
	toastExp  time.Time
	showAbout bool
}

type invMsg struct {
	inv Inventory
	err error
}

type stepsMsg struct {
	cluster Cluster
	steps   []Step
	err     error
}

type appUIMsg struct {
	label string
	url   string
	err   error
}

type yarnMsg struct {
	cluster Cluster
	apps    []YarnApp
	metrics ClusterMetrics
	err     error
}

type hbaseMsg struct {
	cluster Cluster
	tables  []HBaseTable
	err     error
}

type oozieMsg struct {
	cluster   Cluster
	workflows []OozieWorkflow
	coords    []OozieCoordinator
	err       error
}

type hbaseCountMsg struct {
	qualified string
	count     int
	capped    bool
	err       error
}

type clearToastMsg struct{}

// NewModel builds the EMR dashboard over one or more regions. configPath is
// passed through to the child s3 process for the log-location jump (AXE-036).
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
	tbl := table.New(
		table.WithColumns(clusterColumns(len(activeRegions) > 1)),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1), // pin NAME while scrolling columns
	)

	// Build the opt-in on-cluster dialer; a nil dialer (off/misconfigured) makes
	// the live browsers render the connect helper rather than failing.
	var dialer *emrconn.Dialer
	var dialerErr error
	if appCfg != nil {
		dialer, dialerErr = emrconn.New(appCfg.EMR.OnCluster)
	} else {
		dialerErr = emrconn.ErrDisabled
	}

	return &m{
		ctx:        ctx,
		client:     client,
		regions:    activeRegions,
		allRegions: allRegions,
		appCfg:     appCfg,
		configPath: configPath,
		dialer:     dialer,
		dialerErr:  dialerErr,
		filter:     f,
		spinner:    s,
		tbl:        tbl,
		stepsTbl:   newSubTable(stepColumns()),
		yarnTbl:    newSubTable(yarnColumns()),
		hbaseTbl:   newSubTable(hbaseColumns()),
		oozieTbl:   newSubTable(oozieWFColumns()),
		loading:    true,
		sortCol:    -1,
	}, nil
}

// rebuild recomputes the filtered+sorted cluster view and pushes it into the
// shared table, refreshing the sort-arrow header and preserving the cursor.
func (mm *m) rebuild() {
	mm.view = mm.buildView()

	cols := clusterColumns(len(mm.regions) > 1)
	table.ApplySortHeader(cols, mm.sortCol, mm.sortAsc, func(int) bool { return true })
	mm.tbl.SetColumns(cols)

	multi := len(mm.regions) > 1
	rows := make([]table.Row, 0, len(mm.view))
	for _, c := range mm.view {
		rows = append(rows, clusterRow(c, multi))
	}
	mm.tbl.SetRows(rows)
	mm.layoutTable()
}

// layoutTable sizes the cluster table to the current terminal. The cluster
// list's chrome is: an optional region badge, the title and filter lines above,
// and the panel border plus the column-scroll hint and status bar below.
func (mm *m) layoutTable() {
	if mm.width <= 0 || mm.height <= 0 {
		return
	}
	mm.tbl.SetWidth(mm.width - 4) // panel border + padding
	// 1 title + 1 filter + 2 panel border + 1 scroll hint + 1 status bar, plus a
	// badge line whenever the region scope is spotlighted.
	chrome := 6
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		chrome++
	}
	h := mm.height - chrome
	if h < 3 {
		h = 3
	}
	mm.tbl.SetHeight(h)
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.loadInventoryCmd())
}

func (mm *m) loadInventoryCmd() tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR inventory", "regions", len(mm.regions))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		inv, err := mm.client.LoadInventory(ctx, mm.showTerminated)
		return invMsg{inv: inv, err: err}
	}
}

func (mm *m) loadStepsCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR steps", "cluster", cl.ID, "region", cl.Region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		steps, err := mm.client.Steps(ctx, cl.Region, cl.ID, stepWindow)
		return stepsMsg{cluster: cl, steps: steps, err: err}
	}
}

func (mm *m) loadYarnCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		if mm.dialer == nil {
			err := mm.dialerErr
			if err == nil {
				err = emrconn.ErrDisabled
			}
			return yarnMsg{cluster: cl, err: err}
		}
		slog.Info("Loading YARN applications", "cluster", cl.ID)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		apps, metrics, err := FetchYARN(ctx, mm.dialer, cl.MasterDNS)
		return yarnMsg{cluster: cl, apps: apps, metrics: metrics, err: err}
	}
}

func (mm *m) loadOozieCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		if mm.dialer == nil {
			err := mm.dialerErr
			if err == nil {
				err = emrconn.ErrDisabled
			}
			return oozieMsg{cluster: cl, err: err}
		}
		slog.Info("Loading Oozie jobs", "cluster", cl.ID)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		wf, coords, err := FetchOozie(ctx, mm.dialer, cl.MasterDNS)
		return oozieMsg{cluster: cl, workflows: wf, coords: coords, err: err}
	}
}

func (mm *m) countHbaseRowsCmd(t HBaseTable) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Counting HBase rows (full scan)", "table", t.Qualified)
		ctx, cancel := context.WithTimeout(mm.ctx, scanTimeout)
		defer cancel()
		count, capped, err := CountHBaseRows(ctx, mm.dialer, mm.hbaseCluster.MasterDNS, t.Qualified)
		return hbaseCountMsg{qualified: t.Qualified, count: count, capped: capped, err: err}
	}
}

func (mm *m) loadHbaseCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		if mm.dialer == nil {
			err := mm.dialerErr
			if err == nil {
				err = emrconn.ErrDisabled
			}
			return hbaseMsg{cluster: cl, err: err}
		}
		slog.Info("Loading HBase tables", "cluster", cl.ID)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		tables, err := FetchHBase(ctx, mm.dialer, cl.MasterDNS)
		return hbaseMsg{cluster: cl, tables: tables, err: err}
	}
}

func (mm *m) loadAppUICmd(cl Cluster, opt appUIOption) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Generating EMR persistent app UI link", "cluster", cl.ID, "type", opt.UIType)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		url, err := mm.client.PersistentAppUIURL(ctx, cl.Region, cl.ARN, opt.UIType)
		return appUIMsg{label: opt.Label, url: url, err: err}
	}
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
		mm.layoutTable()

	case spinner.TickMsg:
		var cmd tea.Cmd
		mm.spinner, cmd = mm.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		mm.toast = ""

	case s3JumpDoneMsg:
		if msg.err != nil {
			mm.setToast("Could not open S3 logs: " + msg.err.Error())
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

	case stepsMsg:
		mm.stepsLoading = false
		mm.stepsErr = msg.err
		mm.steps = msg.steps
		mm.setRows(&mm.stepsTbl, len(msg.steps), func(i int) table.Row { return stepRow(msg.steps[i]) })

	case appUIMsg:
		mm.appUILoading = false
		mm.appUIActive = false
		if msg.err != nil {
			mm.setToast(msg.label + ": " + msg.err.Error())
			cmds = append(cmds, toastCmd(5*time.Second))
		} else {
			mm.openURL(msg.url, msg.label+" link copied", &cmds)
		}

	case yarnMsg:
		mm.yarnLoading = false
		mm.yarnErr = msg.err
		mm.yarnApps = msg.apps
		mm.yarnMetrics = msg.metrics
		mm.setRows(&mm.yarnTbl, len(msg.apps), func(i int) table.Row { return yarnRow(msg.apps[i]) })

	case hbaseMsg:
		mm.hbaseLoading = false
		mm.hbaseErr = msg.err
		mm.hbaseTables = msg.tables
		mm.setRows(&mm.hbaseTbl, len(msg.tables), func(i int) table.Row { return hbaseRow(msg.tables[i]) })

	case oozieMsg:
		mm.oozieLoading = false
		mm.oozieErr = msg.err
		mm.oozieWF = msg.workflows
		mm.oozieCoord = msg.coords
		mm.setOozieRows()

	case hbaseCountMsg:
		mm.hbaseCounting = false
		if msg.err != nil {
			mm.setToast("Row count failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(5*time.Second))
		} else {
			for i := range mm.hbaseTables {
				if mm.hbaseTables[i].Qualified == msg.qualified {
					mm.hbaseTables[i].RowCount = msg.count
					mm.hbaseTables[i].Counted = true
					mm.hbaseTables[i].CountCapped = msg.capped
					break
				}
			}
			// Re-render the ROWS column for the affected table, keeping the cursor.
			cur := mm.hbaseTbl.Cursor()
			mm.setRows(&mm.hbaseTbl, len(mm.hbaseTables), func(i int) table.Row { return hbaseRow(mm.hbaseTables[i]) })
			mm.hbaseTbl.SetCursor(cur)

			suffix := ""
			if msg.capped {
				suffix = "+ (capped)"
			}
			mm.setToast(fmt.Sprintf("Counted %d%s rows", msg.count, suffix))
			cmds = append(cmds, toastCmd(4*time.Second))
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
			mm.loading = true
			cmds = append(cmds, mm.loadInventoryCmd())
		}
		return cmds
	}

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Cluster-detail overlay: any key closes it (q still quits).
	if mm.detailActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		default:
			mm.detailActive = false
		}
		return cmds
	}

	// Persistent application-UI picker.
	if mm.appUIActive {
		if mm.appUILoading {
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return []tea.Cmd{tea.Quit}
			}
			return cmds // ignore keys while the link is being generated
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc":
			mm.appUIActive = false
		case "up", "k":
			if mm.appUISel > 0 {
				mm.appUISel--
			}
		case "down", "j":
			if mm.appUISel < len(appUIOptions)-1 {
				mm.appUISel++
			}
		case "enter":
			mm.appUILoading = true
			mm.setToast("Generating " + appUIOptions[mm.appUISel].Label + " link…")
			cmds = append(cmds, mm.loadAppUICmd(mm.appUICluster, appUIOptions[mm.appUISel]), mm.spinner.Tick, toastCmd(5*time.Second))
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

	// Step-history sub-view.
	if mm.stepsActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.stepsActive = false
		case "up", "k":
			mm.stepsTbl.MoveUp(1)
		case "down", "j":
			mm.stepsTbl.MoveDown(1)
		case "g", "home":
			mm.stepsTbl.GotoTop()
		case "G", "end":
			mm.stepsTbl.GotoBottom()
		case "<", ",":
			mm.stepsTbl.ScrollLeft()
		case ">", ".":
			mm.stepsTbl.ScrollRight()
		case "y":
			if s, ok := mm.selectedStep(); ok && s.FailureReason != "" {
				_ = clipboard.WriteAll(s.FailureReason)
				mm.setToast("Copied failure reason")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case "L":
			if s, ok := mm.selectedStep(); ok {
				mm.jumpToStepLogs(s, &cmds)
			}
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Live YARN browser sub-view.
	if mm.yarnActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.yarnActive = false
		case "up", "k":
			mm.yarnTbl.MoveUp(1)
		case "down", "j":
			mm.yarnTbl.MoveDown(1)
		case "g", "home":
			mm.yarnTbl.GotoTop()
		case "G", "end":
			mm.yarnTbl.GotoBottom()
		case "<", ",":
			mm.yarnTbl.ScrollLeft()
		case ">", ".":
			mm.yarnTbl.ScrollRight()
		case "r":
			mm.yarnLoading = true
			mm.yarnErr = nil
			cmds = append(cmds, mm.loadYarnCmd(mm.yarnCluster), mm.spinner.Tick)
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// HBase table browser sub-view.
	if mm.hbaseActive {
		// Row-count confirmation prompt takes precedence (cost-stating gate).
		if mm.hbaseConfirm {
			switch msg.String() {
			case "y", "Y", "enter":
				mm.hbaseConfirm = false
				if t, ok := mm.selectedHbaseTable(); ok {
					mm.hbaseCounting = true
					cmds = append(cmds, mm.countHbaseRowsCmd(t), mm.spinner.Tick)
				}
			default:
				mm.hbaseConfirm = false
			}
			return cmds
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left":
			mm.hbaseActive = false
		case "up", "k":
			mm.hbaseTbl.MoveUp(1)
		case "down", "j":
			mm.hbaseTbl.MoveDown(1)
		case "g", "home":
			mm.hbaseTbl.GotoTop()
		case "G", "end":
			mm.hbaseTbl.GotoBottom()
		case "<", ",":
			mm.hbaseTbl.ScrollLeft()
		case ">", ".":
			mm.hbaseTbl.ScrollRight()
		case "c":
			// Ask before scanning — a full-table read is read-only but not free.
			if _, ok := mm.selectedHbaseTable(); ok && !mm.hbaseCounting {
				mm.hbaseConfirm = true
			}
		case "r":
			mm.hbaseLoading = true
			mm.hbaseErr = nil
			cmds = append(cmds, mm.loadHbaseCmd(mm.hbaseCluster), mm.spinner.Tick)
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Oozie workflow/coordinator browser sub-view.
	if mm.oozieActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left":
			mm.oozieActive = false
		case "tab", "right":
			mm.oozieCoords = !mm.oozieCoords
			mm.setOozieRows()
		case "up", "k":
			mm.oozieTbl.MoveUp(1)
		case "down", "j":
			mm.oozieTbl.MoveDown(1)
		case "g", "home":
			mm.oozieTbl.GotoTop()
		case "G", "end":
			mm.oozieTbl.GotoBottom()
		case "<", ",":
			mm.oozieTbl.ScrollLeft()
		case ">", ".":
			mm.oozieTbl.ScrollRight()
		case "r":
			mm.oozieLoading = true
			mm.oozieErr = nil
			cmds = append(cmds, mm.loadOozieCmd(mm.oozieCluster), mm.spinner.Tick)
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return cmds
	}

	// Cluster list.
	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
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
	case "t":
		// Toggle the terminated tail and reload (it changes what ListClusters
		// returns, so it can't be filtered client-side).
		mm.showTerminated = !mm.showTerminated
		mm.loading = true
		mm.inv = Inventory{}
		mm.tbl.SetCursor(0)
		cmds = append(cmds, mm.loadInventoryCmd(), mm.spinner.Tick)
	case "enter", "s":
		if cl, ok := mm.selectedCluster(); ok {
			mm.stepsActive = true
			mm.stepsCluster = cl
			mm.stepsLoading = true
			mm.steps = nil
			mm.stepsErr = nil
			cmds = append(cmds, mm.loadStepsCmd(cl), mm.spinner.Tick)
		}
	case "d":
		if cl, ok := mm.selectedCluster(); ok {
			mm.detailActive = true
			mm.detailCluster = cl
		}
	case "L":
		if cl, ok := mm.selectedCluster(); ok {
			mm.jumpToClusterLogs(cl, &cmds)
		}
	case "u":
		if cl, ok := mm.selectedCluster(); ok {
			if cl.ARN == "" {
				mm.setToast("Cluster has no ARN for a persistent UI")
				cmds = append(cmds, toastCmd(3*time.Second))
			} else {
				mm.appUIActive = true
				mm.appUICluster = cl
				mm.appUISel = 0
				mm.appUILoading = false
			}
		}
	case "y":
		if cl, ok := mm.selectedCluster(); ok {
			mm.yarnActive = true
			mm.yarnCluster = cl
			mm.yarnLoading = true
			mm.yarnApps = nil
			mm.yarnErr = nil
			cmds = append(cmds, mm.loadYarnCmd(cl), mm.spinner.Tick)
		}
	case "h":
		if cl, ok := mm.selectedCluster(); ok {
			mm.hbaseActive = true
			mm.hbaseCluster = cl
			mm.hbaseLoading = true
			mm.hbaseTables = nil
			mm.hbaseErr = nil
			cmds = append(cmds, mm.loadHbaseCmd(cl), mm.spinner.Tick)
		}
	case "z":
		if cl, ok := mm.selectedCluster(); ok {
			mm.oozieActive = true
			mm.oozieCluster = cl
			mm.oozieCoords = false
			mm.oozieLoading = true
			mm.oozieWF = nil
			mm.oozieCoord = nil
			mm.oozieErr = nil
			cmds = append(cmds, mm.loadOozieCmd(cl), mm.spinner.Tick)
		}
	case "o":
		mm.openConsole(&cmds)
	case "S":
		mm.cycleSort()
	case "R":
		if mm.sortCol >= 0 {
			mm.sortAsc = !mm.sortAsc
			mm.tbl.SetCursor(0)
			mm.rebuild()
		}
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
}

// cycleSort advances the cluster-list sort: natural order → each column in
// turn → back to natural order. Each column starts in its most useful
// direction (descending for the numeric HRS column, ascending otherwise);
// press R to flip it.
func (mm *m) cycleSort() {
	mm.sortCol++
	if mm.sortCol >= len(clusterColumns(len(mm.regions) > 1)) {
		mm.sortCol = -1
	}
	mm.sortAsc = mm.sortCol != colHRS
	mm.tbl.SetCursor(0)
	mm.rebuild()
}

// openConsole copies (and opens, when local) the console URL for the selected
// cluster.
func (mm *m) openConsole(cmds *[]tea.Cmd) {
	res, ok := mm.selectedResource()
	if !ok {
		return
	}
	url, _ := consolelink.URL(res)
	mm.openURL(url, "console URL", cmds)
}

// openURL copies the URL and opens it in a browser when running locally,
// toasting "<what> copied" (or "Opened in browser · …" when it launched).
func (mm *m) openURL(url, what string, cmds *[]tea.Cmd) {
	if url == "" {
		return
	}
	_ = clipboard.WriteAll(url)
	if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
		mm.setToast("Opened in browser · " + what)
	} else {
		mm.setToast(what + " copied")
	}
	*cmds = append(*cmds, toastCmd(3*time.Second))
}

// oozieRowCount returns the row count of the active Oozie tab.
func (mm *m) oozieRowCount() int {
	if mm.oozieCoords {
		return len(mm.oozieCoord)
	}
	return len(mm.oozieWF)
}

func (mm *m) PageTitle() string {
	base := "Amazon EMR"
	if mm.stepsActive {
		return base + " › " + mm.stepsCluster.Name + " › steps"
	}
	if mm.yarnActive {
		return base + " › " + mm.yarnCluster.Name + " › YARN"
	}
	if mm.hbaseActive {
		return base + " › " + mm.hbaseCluster.Name + " › HBase"
	}
	if mm.oozieActive {
		return base + " › " + mm.oozieCluster.Name + " › Oozie"
	}
	return base + " › Clusters"
}
