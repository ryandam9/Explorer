package emrtui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/emrconn"
	"github.com/ryandam9/aws_explorer/internal/findings"
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

	// Config browser sub-view (c on a cluster): the cluster's configuration
	// classifications flattened to one row per property (classification, key,
	// value), shown as the on-disk files they become. Loaded async (one
	// DescribeCluster) like the other drill-downs.
	configActive  bool
	configCluster Cluster
	configLoading bool
	configErr     error
	configRows    []ConfigRow
	configTbl     table.Model

	// Cluster describe view (d on a cluster): a full-screen, btop-style grid of
	// per-section panels (overview, config/OS, compute/memory/storage,
	// networking, instances, services). It is loaded asynchronously because it
	// makes extra AWS calls (instance groups, instance-type specs, VPC
	// networking) beyond the inventory enrichment. Each panel is independently
	// scrollable; descFocus is the focused panel and descPanels holds one
	// viewport per section (rebuilt on load / resize, scroll offsets preserved).
	detailActive  bool
	detailCluster Cluster
	descLoading   bool
	descErr       error
	desc          ClusterDescription
	descSections  []descSection
	descPanels    []viewport.Model
	descFocus     int

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

	// Findings panel (f) — deterministic posture/cost checks over the loaded
	// inventory. findingList is computed synchronously (no AWS call), parallel to
	// the table's rows.
	findingsActive bool
	findingList    []findings.Finding
	findingsTbl    table.Model

	// Oozie workflow/coordinator browser (z on a cluster).
	oozieActive  bool
	oozieCluster Cluster
	oozieWF      []OozieWorkflow
	oozieCoord   []OozieCoordinator
	oozieCoords  bool // false = workflows tab, true = coordinators tab
	oozieLoading bool
	oozieErr     error
	oozieTbl     table.Model

	// Progressive load: ListSkeleton fills the list fast (phase 1), then each
	// region's clusters are enriched in the background (phase 2), streaming
	// columns in as they complete. loadGen tags each load so a refresh's
	// stragglers can't patch a newer load; enrichPending counts the regions still
	// enriching.
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

// enrichMsg delivers one region's enriched clusters during phase 2.
type enrichMsg struct {
	gen      int
	region   string
	clusters []Cluster
	fails    int
}

type stepsMsg struct {
	cluster Cluster
	steps   []Step
	err     error
}

// configMsg delivers a cluster's flattened configuration for the config browser.
type configMsg struct {
	cluster Cluster
	rows    []ConfigRow
	err     error
}

// descMsg delivers a cluster's full description (or the error that aborted it)
// for the detail overlay.
type descMsg struct {
	cluster Cluster
	desc    ClusterDescription
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
		ctx:         ctx,
		client:      client,
		regions:     activeRegions,
		allRegions:  allRegions,
		appCfg:      appCfg,
		configPath:  configPath,
		dialer:      dialer,
		dialerErr:   dialerErr,
		filter:      f,
		spinner:     s,
		tbl:         tbl,
		stepsTbl:    newSubTable(stepColumns()),
		configTbl:   newSubTable(configColumns()),
		yarnTbl:     newSubTable(yarnColumns()),
		hbaseTbl:    newSubTable(hbaseColumns()),
		oozieTbl:    newSubTable(oozieWFColumns()),
		findingsTbl: newSubTable(findingsColumns(len(activeRegions) > 1)),
		loading:     true,
		sortCol:     -1,
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
	if mm.inv.EnrichFailures > 0 {
		chrome++ // the enrichment-gap warning line under the filter
	}
	h := mm.height - chrome
	if h < 3 {
		h = 3
	}
	mm.tbl.SetHeight(h)
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

// loadInventoryCmd is phase 1: list clusters across regions (cheap, no
// enrichment) so the table appears immediately.
func (mm *m) loadInventoryCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Listing EMR clusters", "regions", len(mm.regions))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		inv, err := mm.client.ListSkeleton(ctx, mm.showTerminated)
		return invMsg{gen: gen, inv: inv, err: err}
	}
}

// enrichCmds is phase 2: one bounded-concurrency enrichment command per region,
// each streaming its enriched clusters back via an enrichMsg.
func (mm *m) enrichCmds(gen int) []tea.Cmd {
	byRegion := map[string][]Cluster{}
	for _, c := range mm.inv.Clusters {
		byRegion[c.Region] = append(byRegion[c.Region], c)
	}
	cmds := make([]tea.Cmd, 0, len(byRegion))
	for region, clusters := range byRegion {
		cmds = append(cmds, mm.enrichRegionCmd(gen, region, clusters))
	}
	mm.enrichPending = len(cmds)
	return cmds
}

func (mm *m) enrichRegionCmd(gen int, region string, clusters []Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Enriching EMR clusters", "region", region, "count", len(clusters))
		ctx, cancel := context.WithTimeout(mm.ctx, inventoryTimeout)
		defer cancel()
		enriched, fails := mm.client.EnrichRegion(ctx, region, clusters)
		return enrichMsg{gen: gen, region: region, clusters: enriched, fails: fails}
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

func (mm *m) loadConfigCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR configuration", "cluster", cl.ID, "region", cl.Region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		cfgs, err := mm.client.Configurations(ctx, cl.Region, cl.ID)
		return configMsg{cluster: cl, rows: FlattenConfigRows(cfgs), err: err}
	}
}

// loadDescribeCmd fetches a cluster's full description (instance groups,
// instance-type specs, EC2 instances and VPC networking) for the detail overlay.
func (mm *m) loadDescribeCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Describing EMR cluster", "cluster", cl.ID, "region", cl.Region)
		ctx, cancel := context.WithTimeout(mm.ctx, drillTimeout)
		defer cancel()
		desc, err := mm.client.Describe(ctx, cl.Region, cl.ID)
		return descMsg{cluster: cl, desc: desc, err: err}
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
		if msg.gen != mm.loadGen {
			break // a newer load superseded this one
		}
		mm.loading = false
		if msg.err != nil {
			mm.err = msg.err
		} else {
			mm.inv = msg.inv
			mm.rebuild()
			// Phase 2: enrich each region's clusters in the background.
			cmds = append(cmds, mm.enrichCmds(msg.gen)...)
		}

	case enrichMsg:
		if msg.gen != mm.loadGen {
			break // stragglers from a superseded load
		}
		mm.applyEnrichment(msg)

	case stepsMsg:
		mm.stepsLoading = false
		mm.stepsErr = msg.err
		mm.steps = msg.steps
		mm.setRows(&mm.stepsTbl, len(msg.steps), func(i int) table.Row { return stepRow(msg.steps[i]) })

	case configMsg:
		mm.configLoading = false
		mm.configErr = msg.err
		mm.configRows = msg.rows
		mm.setRows(&mm.configTbl, len(msg.rows), func(i int) table.Row { return configTableRow(msg.rows[i]) })

	case descMsg:
		// Ignore a stale describe if the view was closed or moved to another
		// cluster while this one was loading.
		if mm.detailActive && mm.detailCluster.ID == msg.cluster.ID {
			mm.descLoading = false
			mm.descErr = msg.err
			mm.desc = msg.desc
			mm.descFocus = 0
			if msg.err == nil {
				mm.descSections = msg.desc.sections()
				mm.descPanels = make([]viewport.Model, len(mm.descSections))
			}
		}

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
			cmds = append(cmds, mm.beginLoad(), mm.spinner.Tick)
		}
		return cmds
	}

	if mm.showAbout {
		mm.showAbout = false
		return cmds
	}

	// Full-screen describe view: a grid of per-section panels. Tab/Shift+Tab (and
	// arrows) move focus between panels; the focused panel scrolls; Esc/d closes.
	if mm.detailActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "d", "backspace":
			mm.detailActive = false
		case "tab", "right", "l", "n":
			mm.focusPanel(mm.descFocus + 1)
		case "shift+tab", "left", "h", "p":
			mm.focusPanel(mm.descFocus - 1)
		case "up", "k":
			mm.scrollPanel(-1)
		case "down", "j":
			mm.scrollPanel(1)
		case "pgup":
			mm.scrollPanel(-panelPageStep)
		case "pgdown", "pgdn", " ":
			mm.scrollPanel(panelPageStep)
		case "g", "home":
			if p := mm.focusedPanel(); p != nil {
				p.GotoTop()
			}
		case "G", "end":
			if p := mm.focusedPanel(); p != nil {
				p.GotoBottom()
			}
		case ui.KeyAbout:
			mm.showAbout = true
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

	// Config browser sub-view.
	if mm.configActive {
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left", "h":
			mm.configActive = false
		case "up", "k":
			mm.configTbl.MoveUp(1)
		case "down", "j":
			mm.configTbl.MoveDown(1)
		case "pgup":
			mm.configTbl.MoveUp(10)
		case "pgdown", "pgdn", " ":
			mm.configTbl.MoveDown(10)
		case "g", "home":
			mm.configTbl.GotoTop()
		case "G", "end":
			mm.configTbl.GotoBottom()
		case "<", ",":
			mm.configTbl.ScrollLeft()
		case ">", ".":
			mm.configTbl.ScrollRight()
		case "y":
			if r, ok := mm.selectedConfigRow(); ok {
				_ = clipboard.WriteAll(r.Value)
				mm.setToast("Copied value")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case ui.KeyAbout:
			mm.showAbout = true
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
			if !mm.yarnLoading {
				mm.yarnLoading = true
				mm.yarnErr = nil
				cmds = append(cmds, mm.loadYarnCmd(mm.yarnCluster), mm.spinner.Tick)
			}
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
		case "esc", "backspace", "left", "h":
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
			if !mm.hbaseLoading {
				mm.hbaseLoading = true
				mm.hbaseErr = nil
				cmds = append(cmds, mm.loadHbaseCmd(mm.hbaseCluster), mm.spinner.Tick)
			}
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
		case "esc", "backspace", "left", "h":
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
			if !mm.oozieLoading {
				mm.oozieLoading = true
				mm.oozieErr = nil
				cmds = append(cmds, mm.loadOozieCmd(mm.oozieCluster), mm.spinner.Tick)
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
		case "esc", "backspace", "left", "h":
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
		mm.startReload(&cmds)
	case "t":
		// Toggle the terminated tail and reload (it changes what ListClusters
		// returns, so it can't be filtered client-side). Ignore while a load is
		// already running so the toggle and the in-flight scope stay in sync.
		if !mm.loading {
			mm.showTerminated = !mm.showTerminated
			mm.tbl.SetCursor(0)
			mm.startReload(&cmds)
		}
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
			mm.openDetail(cl, &cmds)
		}
	case "c":
		if cl, ok := mm.selectedCluster(); ok {
			mm.configActive = true
			mm.configCluster = cl
			mm.configLoading = true
			mm.configRows = nil
			mm.configErr = nil
			cmds = append(cmds, mm.loadConfigCmd(cl), mm.spinner.Tick)
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
	case "b":
		// HBase browser is bound to b, not h: in the sub-views h is vim-left /
		// back, so binding it here too would open HBase on a vim-left reflex.
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
	case "f":
		mm.openFindings()
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

// startReload kicks off an inventory reload, unless one is already running, so
// a double-press of r (or the t toggle) can't fire concurrent loads that race
// to overwrite the inventory and double the API traffic.
func (mm *m) startReload(cmds *[]tea.Cmd) {
	if mm.loading {
		return
	}
	*cmds = append(*cmds, mm.beginLoad(), mm.spinner.Tick)
}

// applyEnrichment patches one region's enriched clusters back into the inventory
// by ID, accumulates any enrichment failures and re-renders. Decrements the
// pending-region counter so the status bar can show when enrichment is done.
func (mm *m) applyEnrichment(msg enrichMsg) {
	byID := make(map[string]Cluster, len(msg.clusters))
	for _, c := range msg.clusters {
		byID[c.ID] = c
	}
	for i := range mm.inv.Clusters {
		if mm.inv.Clusters[i].Region != msg.region {
			continue
		}
		if e, ok := byID[mm.inv.Clusters[i].ID]; ok {
			mm.inv.Clusters[i] = e
		}
	}
	mm.inv.EnrichFailures += msg.fails
	if mm.enrichPending > 0 {
		mm.enrichPending--
	}
	mm.rebuild()
}

// openDetail opens the full-screen cluster-describe view for cl and kicks off
// the asynchronous describe load (the panels are built when the data arrives).
func (mm *m) openDetail(cl Cluster, cmds *[]tea.Cmd) {
	mm.detailActive = true
	mm.detailCluster = cl
	mm.desc = ClusterDescription{}
	mm.descSections = nil
	mm.descPanels = nil
	mm.descFocus = 0
	mm.descErr = nil
	mm.descLoading = true
	*cmds = append(*cmds, mm.loadDescribeCmd(cl), mm.spinner.Tick)
}

// openFindings computes the deterministic findings over the loaded inventory
// and opens the panel. Synchronous — no AWS call — so there is no loading state.
func (mm *m) openFindings() {
	mm.findingList = mm.computeFindings()
	mm.findingsActive = true
	multi := len(mm.regions) > 1
	mm.findingsTbl.SetColumns(findingsColumns(multi))
	mm.setRows(&mm.findingsTbl, len(mm.findingList), func(i int) table.Row { return findingRow(mm.findingList[i], multi) })
	mm.findingsTbl.SetCursor(0)
}

// cycleSort advances the cluster-list sort: natural order → each column in
// turn → back to natural order. Each column starts in its most useful
// direction (descending for the numeric HRS and AGE columns — biggest/oldest
// first — ascending otherwise); press R to flip it.
func (mm *m) cycleSort() {
	mm.sortCol++
	if mm.sortCol >= len(clusterColumns(len(mm.regions) > 1)) {
		mm.sortCol = -1
	}
	mm.sortAsc = mm.sortCol != colHRS && mm.sortCol != colAge
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
	if mm.detailActive {
		return base + " › " + mm.detailCluster.Name + " › describe"
	}
	if mm.configActive {
		return base + " › " + mm.configCluster.Name + " › config"
	}
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
	if mm.findingsActive {
		return base + " › Findings"
	}
	return base + " › Clusters"
}
