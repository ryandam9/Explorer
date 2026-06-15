package emrtui

import (
	"context"
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
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// stepWindow caps how many steps the history view fetches per cluster.
const stepWindow = 50

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

	sel int

	filter       textinput.Model
	filterActive bool

	// Step-history sub-view (Enter on a cluster).
	stepsActive  bool
	stepsCluster Cluster
	steps        []Step
	stepsLoading bool
	stepsErr     error
	stepsSel     int

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
	yarnSel     int

	// HBase table browser (h on a cluster).
	hbaseActive  bool
	hbaseCluster Cluster
	hbaseTables  []HBaseTable
	hbaseLoading bool
	hbaseErr     error
	hbaseSel     int

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
		regions:    client.Regions(),
		allRegions: allRegions,
		appCfg:     appCfg,
		configPath: configPath,
		dialer:     dialer,
		dialerErr:  dialerErr,
		filter:     f,
		spinner:    s,
		loading:    true,
	}, nil
}

func (mm *m) Init() tea.Cmd {
	return tea.Batch(mm.spinner.Tick, mm.loadInventoryCmd())
}

func (mm *m) loadInventoryCmd() tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR inventory", "regions", len(mm.regions))
		inv, err := mm.client.LoadInventory(mm.ctx)
		return invMsg{inv: inv, err: err}
	}
}

func (mm *m) loadStepsCmd(cl Cluster) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Loading EMR steps", "cluster", cl.ID, "region", cl.Region)
		steps, err := mm.client.Steps(mm.ctx, cl.Region, cl.ID, stepWindow)
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
		apps, metrics, err := FetchYARN(mm.ctx, mm.dialer, cl.MasterDNS)
		return yarnMsg{cluster: cl, apps: apps, metrics: metrics, err: err}
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
		tables, err := FetchHBase(mm.ctx, mm.dialer, cl.MasterDNS)
		return hbaseMsg{cluster: cl, tables: tables, err: err}
	}
}

func (mm *m) loadAppUICmd(cl Cluster, opt appUIOption) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Generating EMR persistent app UI link", "cluster", cl.ID, "type", opt.UIType)
		url, err := mm.client.PersistentAppUIURL(mm.ctx, cl.Region, cl.ARN, opt.UIType)
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
			mm.clamp()
		}

	case stepsMsg:
		mm.stepsLoading = false
		mm.stepsErr = msg.err
		mm.steps = msg.steps
		mm.stepsSel = 0

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
		mm.yarnSel = 0

	case hbaseMsg:
		mm.hbaseLoading = false
		mm.hbaseErr = msg.err
		mm.hbaseTables = msg.tables
		mm.hbaseSel = 0

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
			mm.clamp()
		default:
			var cmd tea.Cmd
			mm.filter, cmd = mm.filter.Update(msg)
			cmds = append(cmds, cmd)
			mm.clamp()
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
			if mm.stepsSel > 0 {
				mm.stepsSel--
			}
		case "down", "j":
			if mm.stepsSel < len(mm.steps)-1 {
				mm.stepsSel++
			}
		case "y":
			if mm.stepsSel < len(mm.steps) && mm.steps[mm.stepsSel].FailureReason != "" {
				_ = clipboard.WriteAll(mm.steps[mm.stepsSel].FailureReason)
				mm.setToast("Copied failure reason")
				cmds = append(cmds, toastCmd(3*time.Second))
			}
		case "L":
			if mm.stepsSel < len(mm.steps) {
				mm.jumpToStepLogs(mm.steps[mm.stepsSel], &cmds)
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
			if mm.yarnSel > 0 {
				mm.yarnSel--
			}
		case "down", "j":
			if mm.yarnSel < len(mm.yarnApps)-1 {
				mm.yarnSel++
			}
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
		switch msg.String() {
		case "q", "ctrl+c":
			return []tea.Cmd{tea.Quit}
		case "esc", "backspace", "left":
			mm.hbaseActive = false
		case "up", "k":
			if mm.hbaseSel > 0 {
				mm.hbaseSel--
			}
		case "down", "j":
			if mm.hbaseSel < len(mm.hbaseTables)-1 {
				mm.hbaseSel++
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

	// Cluster list.
	switch msg.String() {
	case "q", "ctrl+c":
		return []tea.Cmd{tea.Quit}
	case "up", "k":
		if mm.sel > 0 {
			mm.sel--
		}
	case "down", "j":
		if mm.sel < mm.rowCount()-1 {
			mm.sel++
		}
	case "/":
		mm.filterActive = true
		mm.filter.Focus()
	case "r":
		mm.loading = true
		mm.inv = Inventory{}
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
	case "o":
		mm.openConsole(&cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
	return cmds
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

func (mm *m) clamp() {
	if mm.sel >= mm.rowCount() {
		mm.sel = max(0, mm.rowCount()-1)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	return base + " › Clusters"
}
