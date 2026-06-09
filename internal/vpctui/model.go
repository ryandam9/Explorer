package vpctui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/display"
	"github.com/user/aws_explorer/internal/table"
	"github.com/user/aws_explorer/internal/ui"
)

// ---------------------------------------------------------------------------
// Enumerations
// ---------------------------------------------------------------------------

type state int

const (
	stateVPCList state = iota
	stateResourceBrowser
)

type focus int

const (
	focusVPCList focus = iota
	focusVPCSearch
	focusCategory
	focusResourceTable
)

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

type regionsDiscoveredMsg struct{ regions []string }

type regionScannedMsg struct {
	region string
	vpcs   []VPCInfo
	err    error
}

// The vpcID on the messages below identifies the VPC the work was started
// for; replies that arrive after the user has navigated to a different VPC
// are dropped so a slow fetch can never populate the wrong VPC's view.

type resourcesLoadedMsg struct {
	vpcID string
	rt    resourceType
	maps  []map[string]string
	err   error
}

type errMsg struct{ err error }

type findingsLoadedMsg struct {
	vpcID    string
	findings []Finding
	err      error
}

type traceDoneMsg struct {
	vpcID  string
	result traceResult
	err    error
}

type xrefDoneMsg struct {
	vpcID  string
	title  string
	groups []xrefGroup
	err    error
}

type effRulesDoneMsg struct {
	vpcID  string
	result effectiveRuleSet
	err    error
}

type dnsDoneMsg struct {
	vpcID string
	info  VPCDNSInfo
	err   error
}

type diffDoneMsg struct {
	vpcID       string
	current     vpcSnapshot
	baseline    vpcSnapshot
	hasBaseline bool
	err         error
}

type exportDoneMsg struct {
	path string
	err  error
}

type exposureDoneMsg struct {
	vpcID  string
	groups []xrefGroup
	err    error
}

type analyzerListMsg struct {
	list []NetInsightsAnalysis
	err  error
}

type analyzerRunMsg struct {
	analysis NetInsightsAnalysis
	err      error
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

const (
	vpcPanelInner = 22 // inner width of the left VPC list panel
	catPanelInner = 20 // inner width of the middle category panel
)

type Model struct {
	client *VPCClient
	awsCfg *config.AWSConfig
	region string

	state state
	focus focus

	// VPC list (stateVPCList)
	vpcTable   table.Model
	allVPCRows []table.Row
	allVPCs    []VPCInfo

	// Multi-region scan
	scanTotal  int
	scanDone   int
	scanFailed int
	scanning   bool
	seenVPCs   map[string]bool
	allRegions bool

	// VPC search
	inVPCSearch bool
	vpcSearch   textinput.Model

	// Resource browser (stateResourceBrowser)
	selectedVPC      *VPCInfo
	sidebarItems     []sidebarItem
	activeSidebarIdx int
	activeResource   resourceType

	resourceTable   table.Model
	resourceMaps    map[resourceType][]map[string]string
	resourceLoading bool
	resourceErr     error

	// Detail overlay
	showDetail     bool
	detailViewport viewport.Model
	detailTitle    string

	// Findings overlay (VPC linter)
	showFindings     bool
	findings         []Finding
	findingsErr      error
	findingsLoading  bool
	findingsViewport viewport.Model

	// Connectivity path tracer
	showTraceInput  bool
	traceInput      textinput.Model
	traceSourceID   string
	showTraceResult bool
	traceLoading    bool
	traceErr        error
	traceResult     traceResult
	traceViewport   viewport.Model

	// Cross-reference ("where used") overlay
	showXref     bool
	xrefTitle    string
	xrefGroups   []xrefGroup
	xrefLoading  bool
	xrefErr      error
	xrefViewport viewport.Model

	// Effective security rules overlay
	showEffRules    bool
	effRules        effectiveRuleSet
	effRulesLoading bool
	effRulesErr     error
	effRulesVP      viewport.Model

	// DNS / VPC attributes overlay
	showDNS    bool
	dnsInfo    VPCDNSInfo
	dnsLoading bool
	dnsErr     error
	dnsVP      viewport.Model

	// Snapshot diff ("what changed") overlay
	showDiff    bool
	snapDiff    []snapshotChange
	currentSnap vpcSnapshot
	diffLoading bool
	diffErr     error
	diffVP      viewport.Model

	// Public exposure overlay
	showExposure    bool
	exposureGroups  []xrefGroup
	exposureLoading bool
	exposureErr     error
	exposureVP      viewport.Model

	// Reachability Analyzer overlay (AWS Network Insights)
	showAnalyzer      bool
	analyzerList      []NetInsightsAnalysis
	analyzerLoading   bool
	analyzerErr       error
	analyzerVP        viewport.Model
	analyzerInputMode bool
	analyzerInput     textinput.Model
	analyzerConfirm   bool
	analyzerRunning   bool
	analyzerPendSrc   string
	analyzerPendDst   string
	analyzerPendPort  int

	// UI dimensions
	width  int
	height int

	spinner   spinner.Model
	statusMsg string
	err       error
	loading   bool

	// Settings / help
	showHelp     bool
	showSettings bool
	settings     ui.SettingsModel
	configPath   string
	cfg          *config.Config
	themeIdx     int
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func NewModel(
	ctx context.Context,
	awsCfg *config.AWSConfig,
	region string,
	allRegions bool,
	themeName string,
	configPath string,
	cfg *config.Config,
) (*Model, error) {
	client, err := NewVPCClient(ctx, awsCfg, region)
	if err != nil {
		return nil, err
	}

	themeIdx := 0
	if idx, ok := ui.LookupTheme(themeName); ok {
		themeIdx = idx
	}
	ui.SetActiveTheme(themeIdx)

	items := buildSidebarItems()
	firstIdx := firstSelectableIdx(items)

	m := &Model{
		client:           client,
		awsCfg:           awsCfg,
		region:           region,
		allRegions:       allRegions,
		seenVPCs:         make(map[string]bool),
		resourceMaps:     make(map[resourceType][]map[string]string),
		themeIdx:         themeIdx,
		configPath:       configPath,
		cfg:              cfg,
		sidebarItems:     items,
		activeSidebarIdx: firstIdx,
		activeResource:   items[firstIdx].rt,
		state:            stateVPCList,
		focus:            focusVPCList,
	}

	m.settings = ui.NewSettingsModel(0, 0, configPath, cfg)

	m.initVPCTable()
	m.initResourceTable(m.activeResource)

	m.vpcSearch = textinput.New()
	m.vpcSearch.Placeholder = "Filter VPCs..."
	m.vpcSearch.CharLimit = 128
	m.vpcSearch.Width = 40
	m.vpcSearch.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.vpcSearch.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.vpcSearch.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.vpcSearch.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)),
	)

	m.detailViewport = viewport.New(80, 20)
	m.findingsViewport = viewport.New(80, 20)
	m.traceViewport = viewport.New(80, 20)
	m.xrefViewport = viewport.New(80, 20)
	m.effRulesVP = viewport.New(80, 20)
	m.dnsVP = viewport.New(80, 20)
	m.diffVP = viewport.New(80, 20)
	m.exposureVP = viewport.New(80, 20)
	m.analyzerVP = viewport.New(80, 20)

	m.analyzerInput = textinput.New()
	m.analyzerInput.Placeholder = "eni-src -> eni-dst:443  (or eni-src -> igw-xxxx)"
	m.analyzerInput.CharLimit = 96
	m.analyzerInput.Width = 48
	m.analyzerInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.analyzerInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.analyzerInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.traceInput = textinput.New()
	m.traceInput.Placeholder = "10.0.1.20:3306  (or internet:443)"
	m.traceInput.CharLimit = 64
	m.traceInput.Width = 40
	m.traceInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.traceInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.traceInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	return m, nil
}

func (m *Model) initVPCTable() {
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "VPC ID", Width: 22},
		{Title: "Name", Width: 20},
		{Title: "CIDR", Width: 18},
		{Title: "State", Width: 10},
		{Title: "Region", Width: 15},
		{Title: "Default", Width: 8},
	}
	m.vpcTable = table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(15))
	m.applyTableStyle(&m.vpcTable)
}

func (m *Model) initResourceTable(rt resourceType) {
	cols := display.Columns(m.colFields(rt))
	// Freeze the row-number column plus the first data column (the resource's
	// identifier) so it stays pinned while the wider columns scroll horizontally.
	m.resourceTable = table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(15),
		table.WithFrozenColumns(2),
	)
	m.applyTableStyle(&m.resourceTable)
}

// rebuildResourceTable refreshes columns and rows for m.activeResource from cached maps.
func (m *Model) rebuildResourceTable() {
	rt := m.activeResource
	maps := m.resourceMaps[rt]
	colFields := m.colFields(rt)
	m.resourceTable.SetColumns(display.Columns(colFields))
	rows := make([]table.Row, len(maps))
	for i, r := range maps {
		rows[i] = display.Row(colFields, r)
	}
	m.resourceTable.SetRows(seqRows(rows))
}

// colFields returns the resolved column FieldMeta list for rt, applying any
// user config overrides on top of the built-in defaults.
func (m *Model) colFields(rt resourceType) []display.FieldMeta {
	key := rtKey(rt)
	fields, ok := display.VPCFields[key]
	if !ok {
		return nil
	}
	var cfgCols []string
	if m.cfg != nil {
		if rd, ok := m.cfg.Display.VPC[key]; ok {
			cfgCols = rd.Columns
		}
	}
	return display.ResolveColumns(fields, cfgCols)
}

// detailFields returns the resolved detail FieldMeta list for rt.
func (m *Model) detailFields(rt resourceType) []display.FieldMeta {
	key := rtKey(rt)
	fields, ok := display.VPCFields[key]
	if !ok {
		return nil
	}
	var cfgDetail []string
	if m.cfg != nil {
		if rd, ok := m.cfg.Display.VPC[key]; ok {
			cfgDetail = rd.Detail
		}
	}
	return display.ResolveDetail(fields, cfgDetail)
}

func (m *Model) applyTableStyle(t *table.Model) {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(lipgloss.Color(ui.ColorTableHeader())).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ui.ColorTableHeaderLine())).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(ui.ColorText()))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Background(lipgloss.Color(ui.ColorHighlight())).
		Bold(true)
	t.SetStyles(s)
}

func (m *Model) restyleForTheme() {
	m.applyTableStyle(&m.vpcTable)
	m.applyTableStyle(&m.resourceTable)
	m.vpcSearch.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.vpcSearch.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.vpcSearch.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.vpcSearch.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m *Model) loadVPCs() tea.Cmd {
	m.loading = true
	m.scanning = false
	m.scanTotal = 0
	m.scanDone = 0
	m.scanFailed = 0
	m.seenVPCs = make(map[string]bool)
	awsCfg := m.awsCfg
	scanAll := m.allRegions
	region := m.region
	return func() tea.Msg {
		ctx := context.Background()
		if scanAll || region == "" {
			regions := ListRegions(ctx, awsCfg)
			return regionsDiscoveredMsg{regions: regions}
		}
		return regionsDiscoveredMsg{regions: []string{region}}
	}
}

func (m *Model) loadResources(rt resourceType) tea.Cmd {
	m.resourceLoading = true
	m.resourceErr = nil
	vpcID := m.selectedVPC.ID
	if cached, ok := m.resourceMaps[rt]; ok {
		return func() tea.Msg {
			return resourcesLoadedMsg{vpcID: vpcID, rt: rt, maps: cached}
		}
	}
	client := m.client
	return func() tea.Msg {
		maps, err := fetchResourceMaps(client, rt, vpcID)
		return resourcesLoadedMsg{vpcID: vpcID, rt: rt, maps: maps, err: err}
	}
}

func (m *Model) refreshResources() tea.Cmd {
	delete(m.resourceMaps, m.activeResource)
	return m.loadResources(m.activeResource)
}

// loadFindings gathers a networking snapshot of the selected VPC and runs the
// findings analyzer, opening the findings overlay with the result.
func (m *Model) loadFindings() tea.Cmd {
	if m.selectedVPC == nil {
		return nil
	}
	m.showFindings = true
	m.findingsLoading = true
	m.findingsErr = nil
	m.findings = nil
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return findingsLoadedMsg{vpcID: vpcID, err: err}
		}
		return findingsLoadedMsg{vpcID: vpcID, findings: analyzeVPC(snap)}
	}
}

// runTrace gathers a VPC snapshot and traces connectivity from the chosen
// source ENI to the requested destination, opening the result overlay.
func (m *Model) runTrace(req traceRequest) tea.Cmd {
	m.showTraceInput = false
	m.showTraceResult = true
	m.traceLoading = true
	m.traceErr = nil
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return traceDoneMsg{vpcID: vpcID, err: err}
		}
		return traceDoneMsg{vpcID: vpcID, result: tracePath(snap, req)}
	}
}

// loadXref gathers a VPC snapshot and cross-references the given resource,
// opening the "where used" overlay with the result.
func (m *Model) loadXref(resourceID string) tea.Cmd {
	if m.selectedVPC == nil || resourceID == "" {
		return nil
	}
	m.showXref = true
	m.xrefLoading = true
	m.xrefErr = nil
	m.xrefGroups = nil
	m.xrefTitle = resourceID
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return xrefDoneMsg{vpcID: vpcID, err: err}
		}
		return xrefDoneMsg{vpcID: vpcID, title: resourceID, groups: crossReference(snap, resourceID)}
	}
}

// loadEffectiveRules gathers a snapshot and computes the merged effective
// security rules for the given ENI, opening the overlay.
func (m *Model) loadEffectiveRules(eniID string) tea.Cmd {
	if m.selectedVPC == nil || eniID == "" {
		return nil
	}
	m.showEffRules = true
	m.effRulesLoading = true
	m.effRulesErr = nil
	m.effRules = effectiveRuleSet{ENIID: eniID}
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return effRulesDoneMsg{vpcID: vpcID, err: err}
		}
		return effRulesDoneMsg{vpcID: vpcID, result: computeEffectiveRules(snap, eniID)}
	}
}

// loadDiff builds the current VPC snapshot and loads the saved baseline (if
// any) so the update loop can either save a first baseline or show the diff.
func (m *Model) loadDiff() tea.Cmd {
	if m.selectedVPC == nil {
		return nil
	}
	m.diffLoading = true
	m.diffErr = nil
	client := m.client
	vpcID := m.selectedVPC.ID
	region := m.selectedVPC.Region
	owner := m.selectedVPC.OwnerId
	return func() tea.Msg {
		current, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return diffDoneMsg{vpcID: vpcID, err: err}
		}
		current.Region = region
		current.OwnerID = owner
		baseline, ok, err := loadSnapshot(vpcID, owner)
		return diffDoneMsg{vpcID: vpcID, current: current, baseline: baseline, hasBaseline: ok, err: err}
	}
}

// exportReport builds a snapshot, analyzes it, and writes a Markdown report to
// disk, returning the path in the status bar.
func (m *Model) exportReport() tea.Cmd {
	if m.selectedVPC == nil {
		return nil
	}
	m.statusMsg = "Exporting VPC report…"
	client := m.client
	vpcID := m.selectedVPC.ID
	region := m.selectedVPC.Region
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return exportDoneMsg{err: err}
		}
		path, err := writeExport(snap, analyzeVPC(snap), region, time.Now())
		return exportDoneMsg{path: path, err: err}
	}
}

// loadAnalyzerList lists existing Reachability Analyzer analyses (read-only).
func (m *Model) loadAnalyzerList() tea.Cmd {
	m.showAnalyzer = true
	m.analyzerInputMode = false
	m.analyzerConfirm = false
	m.analyzerLoading = true
	m.analyzerErr = nil
	client := m.client
	return func() tea.Msg {
		list, err := client.ListReachabilityAnalyses()
		return analyzerListMsg{list: list, err: err}
	}
}

// runAnalysis creates and runs a new (paid) Reachability Analyzer analysis.
func (m *Model) runAnalysis(source, dest string, port int) tea.Cmd {
	m.analyzerRunning = true
	client := m.client
	return func() tea.Msg {
		a, err := client.CreateStartWaitAnalysis(source, dest, port)
		return analyzerRunMsg{analysis: a, err: err}
	}
}

// loadExposure builds a snapshot and computes the VPC's internet-facing
// surface, opening the public-exposure overlay.
func (m *Model) loadExposure() tea.Cmd {
	if m.selectedVPC == nil {
		return nil
	}
	m.showExposure = true
	m.exposureLoading = true
	m.exposureErr = nil
	m.exposureGroups = nil
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		snap, err := buildVPCSnapshot(client, vpcID)
		if err != nil {
			return exposureDoneMsg{vpcID: vpcID, err: err}
		}
		return exposureDoneMsg{vpcID: vpcID, groups: exposureReport(snap)}
	}
}

// loadDNS fetches the selected VPC's DNS attributes and opens the DNS overlay.
func (m *Model) loadDNS() tea.Cmd {
	if m.selectedVPC == nil {
		return nil
	}
	m.showDNS = true
	m.dnsLoading = true
	m.dnsErr = nil
	m.dnsInfo = VPCDNSInfo{}
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		info, err := client.GetVPCDNSInfo(vpcID)
		return dnsDoneMsg{vpcID: vpcID, info: info, err: err}
	}
}

// parseTraceTarget parses a "host[:port]" destination. A missing port yields
// -1 (any port). "internet" is accepted as the host.
func parseTraceTarget(s string) (destIP string, port int) {
	s = strings.TrimSpace(s)
	port = -1
	if i := strings.LastIndex(s, ":"); i >= 0 {
		if p, ok := atoiPort(strings.TrimSpace(s[i+1:])); ok {
			port = p
			s = s[:i]
		}
	}
	return strings.TrimSpace(s), port
}

// buildVPCSnapshot fetches the networking resources analyzed by the findings
// engine, running the nine list calls concurrently. It is best-effort: a
// single resource type that fails (e.g. missing permissions) does not abort
// the rest. It only returns an error if every fetch failed.
func buildVPCSnapshot(c *VPCClient, vpcID string) (vpcSnapshot, error) {
	snap := vpcSnapshot{VPCID: vpcID}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		ok       int
	)
	// Each fetch writes a distinct snapshot field, so only the bookkeeping
	// needs the mutex.
	run := func(fetch func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fetch()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			ok++
		}()
	}

	run(func() (err error) { snap.Subnets, err = c.ListSubnets(vpcID); return })
	run(func() (err error) { snap.SecurityGroups, err = c.ListSecurityGroups(vpcID); return })
	run(func() (err error) { snap.RouteTables, err = c.ListRouteTables(vpcID); return })
	run(func() (err error) { snap.InternetGateways, err = c.ListInternetGateways(vpcID); return })
	run(func() (err error) { snap.NatGateways, err = c.ListNatGateways(vpcID); return })
	run(func() (err error) { snap.NetworkACLs, err = c.ListNetworkACLs(vpcID); return })
	run(func() (err error) { snap.Peerings, err = c.ListPeeringConnections(vpcID); return })
	run(func() (err error) { snap.Endpoints, err = c.ListVPCEndpoints(vpcID); return })
	run(func() (err error) { snap.NetworkInterfaces, err = c.ListNetworkInterfaces(vpcID); return })
	wg.Wait()

	if ok == 0 && firstErr != nil {
		return snap, firstErr
	}
	return snap, nil
}

// staleVPCMsg reports whether an async reply produced for vpcID no longer
// applies because the user has navigated to a different VPC (or back to the
// VPC list) since the work was started.
func (m *Model) staleVPCMsg(vpcID string) bool {
	return m.selectedVPC == nil || m.selectedVPC.ID != vpcID
}

func (m *Model) enterVPC(vpc VPCInfo) tea.Cmd {
	m.selectedVPC = &vpc
	if vpc.Region != "" && vpc.Region != m.region {
		client, err := NewVPCClient(m.client.ctx, m.awsCfg, vpc.Region)
		if err != nil {
			m.err = err
			return nil
		}
		m.client = client
	}
	m.resourceMaps = make(map[resourceType][]map[string]string)
	m.state = stateResourceBrowser
	m.focus = focusCategory
	return m.loadResources(m.activeResource)
}

// ---------------------------------------------------------------------------
// Init / Update / View
// ---------------------------------------------------------------------------

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadVPCs(), m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Settings panel absorbs all input when open.
	if m.showSettings {
		updated, cmd := m.settings.Update(msg)
		m.settings = updated
		cmds = append(cmds, cmd)
		switch msg := msg.(type) {
		case ui.SettingsSavedMsg:
			m.showSettings = false
			if idx, ok := ui.LookupTheme(msg.Theme); ok {
				m.themeIdx = idx
			}
			m.restyleForTheme()
		case tea.KeyMsg:
			if msg.String() == "esc" && !m.settings.EditMode() {
				m.showSettings = false
			}
		}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.settings = ui.NewSettingsModel(msg.Width, msg.Height, m.configPath, m.cfg)
		m.updateTableSizes()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case regionsDiscoveredMsg:
		m.scanning = true
		m.scanTotal = len(msg.regions)
		awsCfg := m.awsCfg
		scanCmds := make([]tea.Cmd, 0, len(msg.regions))
		for _, region := range msg.regions {
			r := region
			scanCmds = append(scanCmds, func() tea.Msg {
				vpcs, err := ListVPCsInRegion(context.Background(), awsCfg, r)
				return regionScannedMsg{region: r, vpcs: vpcs, err: err}
			})
		}
		cmds = append(cmds, tea.Batch(scanCmds...))

	case regionScannedMsg:
		m.scanDone++
		if msg.err != nil {
			m.scanFailed++
		}
		for _, vpc := range msg.vpcs {
			if !m.seenVPCs[vpc.ID] {
				m.seenVPCs[vpc.ID] = true
				m.allVPCs = append(m.allVPCs, vpc)
			}
		}
		m.rebuildVPCTable(m.allVPCs)
		if m.scanDone >= m.scanTotal {
			m.loading = false
			m.scanning = false
		}

	case resourcesLoadedMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.resourceLoading = false
		if msg.err != nil {
			m.resourceErr = msg.err
		} else {
			m.resourceMaps[msg.rt] = msg.maps
			if msg.rt == m.activeResource {
				m.rebuildResourceTable()
			}
		}

	case findingsLoadedMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.findingsLoading = false
		m.findingsErr = msg.err
		m.findings = msg.findings
		m.findingsViewport.SetContent(m.renderFindings())
		m.findingsViewport.GotoTop()

	case traceDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.traceLoading = false
		m.traceErr = msg.err
		m.traceResult = msg.result
		m.traceViewport.SetContent(m.renderTraceResult())
		m.traceViewport.GotoTop()

	case xrefDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.xrefLoading = false
		m.xrefErr = msg.err
		m.xrefTitle = msg.title
		m.xrefGroups = msg.groups
		m.xrefViewport.SetContent(m.renderXref())
		m.xrefViewport.GotoTop()

	case effRulesDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.effRulesLoading = false
		m.effRulesErr = msg.err
		m.effRules = msg.result
		m.effRulesVP.SetContent(m.renderEffRules())
		m.effRulesVP.GotoTop()

	case dnsDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.dnsLoading = false
		m.dnsErr = msg.err
		m.dnsInfo = msg.info
		m.dnsVP.SetContent(m.renderDNS())
		m.dnsVP.GotoTop()

	case exportDoneMsg:
		if msg.err != nil {
			m.statusMsg = "Export failed: " + msg.err.Error()
		} else {
			m.statusMsg = "Exported VPC report to " + msg.path
		}

	case exposureDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.exposureLoading = false
		m.exposureErr = msg.err
		m.exposureGroups = msg.groups
		m.exposureVP.SetContent(m.renderExposure())
		m.exposureVP.GotoTop()

	case analyzerListMsg:
		m.analyzerLoading = false
		m.analyzerErr = msg.err
		m.analyzerList = msg.list
		m.analyzerVP.SetContent(m.renderAnalyzerList())
		m.analyzerVP.GotoTop()

	case analyzerRunMsg:
		m.analyzerRunning = false
		if msg.err != nil {
			m.analyzerErr = msg.err
		} else {
			// Prepend the new analysis and refresh the list view.
			m.analyzerList = append([]NetInsightsAnalysis{msg.analysis}, m.analyzerList...)
			m.analyzerErr = nil
			m.statusMsg = "Analysis " + msg.analysis.AnalysisID + ": " + analysisVerdict(msg.analysis)
		}
		m.analyzerVP.SetContent(m.renderAnalyzerList())
		m.analyzerVP.GotoTop()

	case diffDoneMsg:
		if m.staleVPCMsg(msg.vpcID) {
			break
		}
		m.diffLoading = false
		m.currentSnap = msg.current
		if msg.err != nil {
			m.diffErr = msg.err
			m.showDiff = true
			m.diffVP.SetContent("")
			break
		}
		if !msg.hasBaseline {
			// First run for this VPC: save the baseline and tell the user.
			if err := saveSnapshot(msg.current); err != nil {
				m.statusMsg = "Failed to save baseline: " + err.Error()
			} else {
				m.statusMsg = "Baseline snapshot saved — press w later to see what changed"
			}
			break
		}
		m.snapDiff = diffSnapshots(msg.baseline, msg.current)
		m.diffErr = nil
		m.showDiff = true
		m.diffVP.SetContent(m.renderDiff())
		m.diffVP.GotoTop()

	case errMsg:
		m.err = msg.err
		m.loading = false

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, tea.Batch(cmds...)
}

// scrollKeys applies the shared overlay scrolling keys to vp, reporting
// whether the key was handled.
func scrollKeys(vp *viewport.Model, key string) bool {
	switch key {
	case "up", "k":
		vp.LineUp(1)
	case "down", "j":
		vp.LineDown(1)
	case "pgup":
		vp.HalfViewUp()
	case "pgdown", " ":
		vp.HalfViewDown()
	case "g", "home":
		vp.GotoTop()
	case "G", "end":
		vp.GotoBottom()
	default:
		return false
	}
	return true
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Trace destination input: capture typing.
	if m.showTraceInput {
		switch key {
		case "esc":
			m.showTraceInput = false
			m.traceInput.Blur()
			return m, nil
		case "enter":
			destIP, port := parseTraceTarget(m.traceInput.Value())
			if destIP == "" {
				m.showTraceInput = false
				return m, nil
			}
			m.traceInput.Blur()
			return m, m.runTrace(traceRequest{
				SourceENIID: m.traceSourceID,
				DestIP:      destIP,
				Protocol:    "tcp",
				Port:        port,
			})
		default:
			var cmd tea.Cmd
			m.traceInput, cmd = m.traceInput.Update(msg)
			return m, cmd
		}
	}

	// Simple scroll-and-close overlays share one key handler. Each closes on
	// Esc/q plus its own toggle key.
	for _, ov := range []struct {
		open     *bool
		vp       *viewport.Model
		closeKey string
	}{
		{&m.showDetail, &m.detailViewport, "enter"},
		{&m.showTraceResult, &m.traceViewport, "t"},
		{&m.showExposure, &m.exposureVP, "P"},
		{&m.showDNS, &m.dnsVP, "D"},
		{&m.showEffRules, &m.effRulesVP, "e"},
		{&m.showXref, &m.xrefViewport, "x"},
		{&m.showFindings, &m.findingsViewport, "F"},
	} {
		if !*ov.open {
			continue
		}
		switch key {
		case "esc", "q", ov.closeKey:
			*ov.open = false
		default:
			scrollKeys(ov.vp, key)
		}
		return m, nil
	}

	// Reachability Analyzer overlay: list / new-input / confirm / running.
	if m.showAnalyzer {
		switch {
		case m.analyzerRunning:
			// The analysis itself keeps running on AWS; only the overlay and
			// the app respond to keys.
			switch key {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.showAnalyzer = false
			}
			return m, nil
		case m.analyzerInputMode:
			switch key {
			case "esc":
				m.analyzerInputMode = false
				m.analyzerInput.Blur()
			case "enter":
				src, dst, port, ok := parseAnalyzerInput(m.analyzerInput.Value())
				if ok {
					m.analyzerPendSrc, m.analyzerPendDst, m.analyzerPendPort = src, dst, port
					m.analyzerInputMode = false
					m.analyzerInput.Blur()
					m.analyzerConfirm = true
				}
			default:
				var cmd tea.Cmd
				m.analyzerInput, cmd = m.analyzerInput.Update(msg)
				return m, cmd
			}
			return m, nil
		case m.analyzerConfirm:
			switch key {
			case "y", "Y":
				m.analyzerConfirm = false
				return m, m.runAnalysis(m.analyzerPendSrc, m.analyzerPendDst, m.analyzerPendPort)
			case "n", "N", "esc":
				m.analyzerConfirm = false
			}
			return m, nil
		default:
			switch key {
			case "esc", "q", "A":
				m.showAnalyzer = false
			case "n":
				m.analyzerInputMode = true
				prefill := ""
				if m.activeResource == rtNetworkInterfaces {
					if id := m.selectedResourceID(); id != "" {
						prefill = id + " -> "
					}
				}
				m.analyzerInput.SetValue(prefill)
				m.analyzerInput.CursorEnd()
				return m, m.analyzerInput.Focus()
			default:
				scrollKeys(&m.analyzerVP, key)
			}
			return m, nil
		}
	}

	// Snapshot-diff overlay: scroll-and-close plus b to re-baseline.
	if m.showDiff {
		switch key {
		case "esc", "q", "w":
			m.showDiff = false
		case "b":
			if err := saveSnapshot(m.currentSnap); err != nil {
				m.statusMsg = "Failed to update baseline: " + err.Error()
			} else {
				m.statusMsg = "Baseline updated to the current state"
			}
			m.showDiff = false
		default:
			scrollKeys(&m.diffVP, key)
		}
		return m, nil
	}

	// Help overlay.
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Global keys.
	switch key {
	case ui.KeyQuit, "ctrl+c":
		return m, tea.Quit
	case ui.KeyHelp:
		m.showHelp = true
		return m, nil
	case ui.KeySettings:
		m.showSettings = true
		return m, nil
	case "F":
		// Run the VPC findings linter (resource browser only).
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.loadFindings()
		}
	case "D":
		// Show the VPC's DNS configuration (resource browser only).
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.loadDNS()
		}
	case "w":
		// Snapshot diff: baseline on first use, "what changed" thereafter.
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.loadDiff()
		}
	case "E":
		// Export a Markdown report of the VPC + findings.
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.exportReport()
		}
	case "P":
		// Public exposure: what is reachable from the internet.
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.loadExposure()
		}
	case "A":
		// AWS Reachability Analyzer: list existing analyses (read-only).
		if m.state == stateResourceBrowser && m.selectedVPC != nil {
			return m, m.loadAnalyzerList()
		}
	}

	switch m.focus {
	case focusVPCSearch:
		return m.handleVPCSearchKey(msg)
	case focusVPCList:
		return m.handleVPCListKey(msg)
	case focusCategory:
		return m.handleCategoryKey(msg)
	case focusResourceTable:
		return m.handleResourceTableKey(msg)
	}
	return m, nil
}

func (m *Model) handleVPCListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "/":
		m.inVPCSearch = true
		m.focus = focusVPCSearch
		m.vpcSearch.SetValue("")
		m.vpcSearch.Focus()
		return m, nil
	case "r":
		m.allVPCs = nil
		m.allVPCRows = nil
		return m, m.loadVPCs()
	case "enter":
		row := m.vpcTable.SelectedRow()
		if len(row) < 2 {
			return m, nil
		}
		vpcID := row[1]
		for _, vpc := range m.allVPCs {
			if vpc.ID == vpcID {
				return m, m.enterVPC(vpc)
			}
		}
	default:
		var cmd tea.Cmd
		m.vpcTable, cmd = m.vpcTable.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleVPCSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.inVPCSearch = false
		m.focus = focusVPCList
		m.vpcSearch.Blur()
		m.rebuildVPCTable(m.allVPCs)
		return m, nil
	case "enter":
		m.inVPCSearch = false
		m.focus = focusVPCList
		m.vpcSearch.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.vpcSearch, cmd = m.vpcSearch.Update(msg)
		m.filterVPCTable(m.vpcSearch.Value())
		return m, cmd
	}
}

func (m *Model) handleCategoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.state = stateVPCList
		m.focus = focusVPCList
		m.selectedVPC = nil
		m.resourceTable.Blur()
		m.vpcTable.Focus()
		return m, nil
	case "tab", "right", "l":
		m.focus = focusResourceTable
		m.vpcTable.Blur()
		m.resourceTable.Focus()
		return m, nil
	case "up", "k":
		m.activeSidebarIdx = nextSelectableIdx(m.sidebarItems, m.activeSidebarIdx, -1)
		return m, nil
	case "down", "j":
		m.activeSidebarIdx = nextSelectableIdx(m.sidebarItems, m.activeSidebarIdx, 1)
		return m, nil
	case "enter":
		item := m.sidebarItems[m.activeSidebarIdx]
		if item.isHeader {
			return m, nil
		}
		m.activeResource = item.rt
		m.initResourceTable(m.activeResource)
		m.updateTableSizes()
		if _, cached := m.resourceMaps[m.activeResource]; cached {
			m.rebuildResourceTable()
		}
		return m, m.loadResources(m.activeResource)
	}
	return m, nil
}

func (m *Model) handleResourceTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.state = stateVPCList
		m.focus = focusVPCList
		m.selectedVPC = nil
		m.resourceTable.Blur()
		m.vpcTable.Focus()
		return m, nil
	case "tab", "left", "h":
		m.focus = focusCategory
		m.resourceTable.Blur()
		return m, nil
	case ">", ".":
		m.resourceTable.ScrollRight()
		return m, nil
	case "<", ",":
		m.resourceTable.ScrollLeft()
		return m, nil
	case "r":
		return m, m.refreshResources()
	case "c":
		row := m.resourceTable.SelectedRow()
		if len(row) >= 2 {
			_ = clipboard.WriteAll(row[1])
			m.statusMsg = "Copied: " + row[1]
		}
		return m, nil
	case "t":
		// Start a connectivity trace from the selected network interface.
		if m.activeResource == rtNetworkInterfaces {
			row := m.resourceTable.SelectedRow()
			if len(row) >= 2 && row[1] != "" {
				m.traceSourceID = row[1]
				m.showTraceInput = true
				m.traceInput.SetValue("")
				return m, m.traceInput.Focus()
			}
		}
		return m, nil
	case "x":
		// Cross-reference ("where used") for the selected resource.
		if id := m.selectedResourceID(); id != "" {
			return m, m.loadXref(id)
		}
		return m, nil
	case "e":
		// Effective merged security rules for the selected network interface.
		if m.activeResource == rtNetworkInterfaces {
			if id := m.selectedResourceID(); id != "" {
				return m, m.loadEffectiveRules(id)
			}
		}
		return m, nil
	case "enter":
		maps := m.resourceMaps[m.activeResource]
		idx := m.resourceTable.Cursor()
		if idx >= 0 && idx < len(maps) {
			r := maps[idx]
			detFields := m.detailFields(m.activeResource)
			lines := display.Detail(detFields, r)
			// Use the first non-empty value among common ID keys for the title.
			idVal := firstID(r)
			m.detailTitle = rtLabel(m.activeResource) + ": " + idVal
			m.detailViewport.SetContent(strings.Join(lines, "\n"))
			m.detailViewport.GotoTop()
			m.showDetail = true
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.resourceTable, cmd = m.resourceTable.Update(msg)
		return m, cmd
	}
}

// ---------------------------------------------------------------------------
// Table helpers
// ---------------------------------------------------------------------------

func (m *Model) rebuildVPCTable(vpcs []VPCInfo) {
	rows := make([]table.Row, 0, len(vpcs))
	for _, vpc := range vpcs {
		rows = append(rows, table.Row{
			"",
			vpc.ID,
			orDash(vpc.Name),
			vpc.CIDR,
			vpc.State,
			vpc.Region,
			boolStr(vpc.IsDefault),
		})
	}
	rows = seqRows(rows)
	m.allVPCRows = rows
	m.vpcTable.SetRows(rows)
}

func (m *Model) filterVPCTable(query string) {
	if query == "" {
		m.vpcTable.SetRows(m.allVPCRows)
		return
	}
	q := strings.ToLower(query)
	var filtered []table.Row
	for _, r := range m.allVPCRows {
		for _, cell := range r {
			if strings.Contains(strings.ToLower(cell), q) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	m.vpcTable.SetRows(filtered)
}

func (m *Model) updateTableSizes() {
	if m.width == 0 || m.height == 0 {
		return
	}
	statusH := 1
	tableH := m.height - statusH - 4 // borders + header
	if tableH < 1 {
		tableH = 1
	}

	// VPC list table uses full width in stateVPCList.
	m.vpcTable.SetHeight(tableH)

	// Resource table: right panel = total - left panel - middle panel - borders.
	// The panel adds 2 for borders; the content area has title + separator (2 lines)
	// already accounted for in the same tableH base, so use tableH directly.
	rightWidth := m.width - (vpcPanelInner + 4) - (catPanelInner + 4)
	if rightWidth < 20 {
		rightWidth = 20
	}
	m.resourceTable.SetHeight(tableH)
	// Inner content area = panel width minus its horizontal padding (0,1). The
	// table uses this to decide how many columns fit before scrolling kicks in.
	m.resourceTable.SetWidth(rightWidth - 2)

	// Resize detail viewport.
	dvW := m.width - 8
	dvH := m.height - 8
	if dvW < 20 {
		dvW = 20
	}
	if dvH < 5 {
		dvH = 5
	}
	m.detailViewport.Width = dvW
	m.detailViewport.Height = dvH

	// All other overlay viewports share one size: the detail size minus room
	// for the border, title and footer.
	for _, vp := range []*viewport.Model{
		&m.findingsViewport, &m.traceViewport, &m.xrefViewport, &m.effRulesVP,
		&m.dnsVP, &m.diffVP, &m.exposureVP, &m.analyzerVP,
	} {
		vp.Width = dvW
		vp.Height = max(dvH-2, 3)
	}

	// Re-wrap already-loaded overlay content to the new width.
	if len(m.findings) > 0 {
		m.findingsViewport.SetContent(m.renderFindings())
	}
	if len(m.traceResult.Hops) > 0 {
		m.traceViewport.SetContent(m.renderTraceResult())
	}
	if len(m.xrefGroups) > 0 {
		m.xrefViewport.SetContent(m.renderXref())
	}
	if m.effRules.Found {
		m.effRulesVP.SetContent(m.renderEffRules())
	}
	if m.dnsInfo.VPCID != "" {
		m.dnsVP.SetContent(m.renderDNS())
	}
	if m.showDiff && m.diffErr == nil {
		m.diffVP.SetContent(m.renderDiff())
	}
	if len(m.exposureGroups) > 0 {
		m.exposureVP.SetContent(m.renderExposure())
	}
	if m.showAnalyzer {
		m.analyzerVP.SetContent(m.renderAnalyzerList())
	}
}

// selectedResourceID returns the primary ID of the resource currently selected
// in the resource table, resolved from its map row (so it works regardless of
// which column is shown first).
func (m *Model) selectedResourceID() string {
	maps := m.resourceMaps[m.activeResource]
	idx := m.resourceTable.Cursor()
	if idx < 0 || idx >= len(maps) {
		return ""
	}
	return firstID(maps[idx])
}

// firstID returns a human-readable identifier from a resource map, trying
// common primary-key field names in order.
func firstID(r map[string]string) string {
	for _, k := range []string{"instance_id", "eni_id", "subnet_id", "sg_id", "rt_id", "igw_id",
		"nat_id", "endpoint_id", "nacl_id", "peering_id", "log_id",
		"name", "db_id"} {
		if v := r[k]; v != "" && v != "-" {
			return v
		}
	}
	return "detail"
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m *Model) View() string {
	if m.showSettings {
		return m.settings.View()
	}

	var content string
	switch m.state {
	case stateVPCList:
		content = m.viewVPCListState()
	case stateResourceBrowser:
		content = m.viewResourceBrowserState()
	}

	if m.showHelp {
		helpW := 60
		if m.width > 0 {
			helpW = min(m.width-4, 70)
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, ui.HelpView("VPC Explorer — Help", m.helpText(), helpW))
	}

	if m.showDetail {
		return m.viewDetailOverlay(content)
	}

	if m.showFindings {
		return m.viewFindingsOverlay(content)
	}

	if m.showXref {
		return m.viewXrefOverlay(content)
	}

	if m.showEffRules {
		return m.viewEffRulesOverlay(content)
	}

	if m.showDNS {
		return m.viewDNSOverlay(content)
	}

	if m.showDiff {
		return m.viewDiffOverlay(content)
	}

	if m.showExposure {
		return m.viewExposureOverlay(content)
	}

	if m.showAnalyzer {
		return m.viewAnalyzerOverlay(content)
	}

	if m.showTraceInput {
		return m.viewTraceInputOverlay(content)
	}

	if m.showTraceResult {
		return m.viewTraceResultOverlay(content)
	}

	return content
}

func (m *Model) viewVPCListState() string {
	borderColor := ui.ColorBorderFocus()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("VPC Explorer")

	header := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", m.viewScanStatus())

	tableView := m.vpcTable.View()

	var searchBar string
	if m.inVPCSearch {
		searchBar = "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color(ui.ColorMuted())).Render("Search: ") +
			m.vpcSearch.View()
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, header, tableView+searchBar)

	panel := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Render(inner)

	status := m.viewStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, panel, status)
}

func (m *Model) viewResourceBrowserState() string {
	// Each panel is wrapped in a rounded border (+2 rows) and the status bar
	// sits below (+1 row). Reserve all 3 so the panels — including their top
	// border and title row — stay on screen instead of scrolling off the top.
	panelH := m.height - 3
	if panelH < 5 {
		panelH = 5
	}

	left := m.viewVPCPanel(panelH)
	middle := m.viewCategoryPanel(panelH)
	right := m.viewResourcePanel(panelH)

	main := lipgloss.JoinHorizontal(lipgloss.Top, left, middle, right)
	status := m.viewStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, main, status)
}

func (m *Model) viewVPCPanel(height int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("VPCs")

	var lines []string
	for _, vpc := range m.allVPCs {
		label := vpc.ID
		if vpc.Name != "" {
			label = vpc.Name
		}
		if ansi.StringWidth(label) > vpcPanelInner-2 {
			label = ansi.Truncate(label, vpcPanelInner-2, "...")
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
		if m.selectedVPC != nil && vpc.ID == m.selectedVPC.ID {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ui.ColorHighlightText())).
				Background(lipgloss.Color(ui.ColorHighlight())).
				Bold(true)
		}
		lines = append(lines, style.Width(vpcPanelInner).Render(label))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, append([]string{title, ""}, lines...)...)

	borderColor := ui.ColorBorder()
	return lipgloss.NewStyle().
		Width(vpcPanelInner+2).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Render(content)
}

func (m *Model) viewCategoryPanel(height int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("Resources")

	var lines []string
	for i, item := range m.sidebarItems {
		if item.isHeader {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color(ui.ColorMuted())).
				Bold(true).
				Width(catPanelInner).
				Render("▸ "+item.label))
		} else {
			label := item.label
			if ansi.StringWidth(label) > catPanelInner-2 {
				label = ansi.Truncate(label, catPanelInner-2, "...")
			}
			var style lipgloss.Style
			if i == m.activeSidebarIdx {
				style = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorHighlightText())).
					Background(lipgloss.Color(ui.ColorHighlight())).
					Bold(true).
					Width(catPanelInner)
			} else if item.rt == m.activeResource {
				style = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorAccent())).
					Width(catPanelInner)
			} else {
				style = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorText())).
					Width(catPanelInner)
			}
			lines = append(lines, style.Render("  "+label))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, append([]string{title, ""}, lines...)...)

	borderColor := ui.ColorBorder()
	if m.focus == focusCategory {
		borderColor = ui.ColorBorderFocus()
	}
	return lipgloss.NewStyle().
		Width(catPanelInner+2).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Render(content)
}

func (m *Model) viewResourcePanel(height int) string {
	rightWidth := m.width - (vpcPanelInner + 4) - (catPanelInner + 4)
	if rightWidth < 20 {
		rightWidth = 20
	}

	title := rtLabel(m.activeResource)
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading()))

	// Column-scroll indicator: shows arrows when columns are hidden off either
	// edge so the user knows there is more to scroll to with < / >.
	var scrollHint string
	if hiddenLeft, hiddenRight := m.resourceTable.ColScrollInfo(); hiddenLeft+hiddenRight > 0 {
		left, right := " ", " "
		if hiddenLeft > 0 {
			left = "◀"
		}
		if hiddenRight > 0 {
			right = "▶"
		}
		scrollHint = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render(fmt.Sprintf("  %s %d more cols %s", left, hiddenLeft+hiddenRight, right))
	}

	var body string
	switch {
	case m.resourceLoading:
		body = m.spinner.View() + "  Loading " + rtLabel(m.activeResource) + "..."
	case m.resourceErr != nil:
		body = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Error: " + m.resourceErr.Error())
	case len(m.resourceMaps[m.activeResource]) == 0:
		body = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No " + rtLabel(m.activeResource) + " found in this VPC.")
	default:
		body = m.resourceTable.View()
	}

	header := lipgloss.JoinHorizontal(lipgloss.Left, titleStyle.Render(title), scrollHint)
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body)

	// A resource table can be wider than the panel on narrow terminals. Clip
	// each line to the panel's inner width so the rightmost column truncates
	// instead of the line wrapping — wrapping mangles the ANSI-styled selected
	// row and breaks column alignment. Inner width = panel width minus the two
	// padding columns added below.
	content = clipLines(content, rightWidth-2)

	borderColor := ui.ColorBorder()
	if m.focus == focusResourceTable {
		borderColor = ui.ColorBorderFocus()
	}

	return lipgloss.NewStyle().
		Width(rightWidth).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Render(content)
}

// overlayFrame renders the shared overlay chrome: title, body, and footer
// hint inside a centered, focus-bordered box.
func (m *Model) overlayFrame(title, body, hint string) string {
	titleView := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(title)

	hintView := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorMuted())).
		Render(hint)

	inner := lipgloss.JoinVertical(lipgloss.Left, titleView, "", body, "", hintView)

	overlay := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Foreground(lipgloss.Color(ui.ColorText())).
		Padding(1, 2).
		Render(inner)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	}
	return overlay
}

// overlayBody picks the spinner, error, or viewport content for an overlay.
func (m *Model) overlayBody(loading bool, loadingText string, err error, vp *viewport.Model) string {
	switch {
	case loading:
		return m.spinner.View() + "  " + loadingText
	case err != nil:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Error: " + err.Error())
	default:
		return vp.View()
	}
}

func (m *Model) viewDetailOverlay(bg string) string {
	return m.overlayFrame(m.detailTitle, m.detailViewport.View(), "↑/↓ scroll  •  Esc close")
}

func (m *Model) viewFindingsOverlay(bg string) string {
	titleText := "VPC Findings"
	if !m.findingsLoading {
		crit, warn, info := countBySeverity(m.findings)
		titleText = fmt.Sprintf("VPC Findings — %d critical, %d warning, %d info", crit, warn, info)
	}
	body := m.overlayBody(m.findingsLoading, "Analyzing VPC…", m.findingsErr, &m.findingsViewport)
	return m.overlayFrame(titleText, body, "↑/↓ scroll  •  Esc/F close")
}

// renderFindings builds the scrollable body of the findings overlay: a coloured,
// numbered list grouped from most to least severe.
func (m *Model) renderFindings() string {
	if len(m.findings) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No issues detected. ✓")
	}

	sevStyle := func(s Severity) lipgloss.Style {
		switch s {
		case SevCritical:
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorError()))
		case SevWarning:
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorWarning()))
		default:
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorMuted()))
		}
	}
	label := map[Severity]string{SevCritical: "🔴 CRITICAL", SevWarning: "🟡 WARNING", SevInfo: "🔵 INFO"}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	heading := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))

	// Wrap the detail/fix paragraphs to the overlay width and indent them, so
	// long sentences wrap instead of being truncated by the viewport.
	wrapW := m.findingsViewport.Width
	if wrapW <= 0 {
		wrapW = 80
	}
	indented := func(text, prefix string) string {
		wrapped := lipgloss.NewStyle().Width(wrapW - len(prefix)).Render(text)
		lines := strings.Split(wrapped, "\n")
		for i, ln := range lines {
			lines[i] = prefix + ln
		}
		return strings.Join(lines, "\n")
	}

	var b strings.Builder
	for i, f := range m.findings {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(sevStyle(f.Severity).Render(label[f.Severity]) + "  " +
			heading.Render(f.Title) + muted.Render("  ["+f.Resource+"]"))
		b.WriteString("\n" + indented(f.Detail, "  "))
		if f.Fix != "" {
			b.WriteString("\n" + muted.Render(indented("Fix: "+f.Fix, "  ")))
		}
	}
	return b.String()
}

func (m *Model) viewTraceInputOverlay(bg string) string {
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
		Render("Destination:")
	return m.overlayFrame(
		"Trace connectivity from "+m.traceSourceID,
		prompt+" "+m.traceInput.View(),
		"Enter an IP and port (e.g. 10.0.1.20:3306) or internet:443  •  Enter run  •  Esc cancel")
}

func (m *Model) viewTraceResultOverlay(bg string) string {
	body := m.overlayBody(m.traceLoading, "Tracing path…", m.traceErr, &m.traceViewport)
	return m.overlayFrame("Connectivity Trace", body, "↑/↓ scroll  •  Esc/t close")
}

// renderTraceResult builds the scrollable body of the trace result overlay: a
// summary line followed by each evaluated hop.
func (m *Model) renderTraceResult() string {
	summaryColor := ui.ColorWarning()
	if m.traceResult.Reachable {
		summaryColor = ui.ColorAccent()
	}
	if strings.HasPrefix(m.traceResult.Summary, "❌") {
		summaryColor = ui.ColorError()
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(summaryColor)).
		Render(m.traceResult.Summary))
	b.WriteString("\n")

	glyph := map[hopStatus]string{hopPass: "✓", hopFail: "✗", hopNote: "•"}
	style := func(s hopStatus) lipgloss.Style {
		switch s {
		case hopFail:
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
		case hopPass:
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
		default:
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
		}
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	for _, h := range m.traceResult.Hops {
		b.WriteString("\n" + style(h.Status).Render(glyph[h.Status]+" "+h.Name))
		if h.Detail != "" {
			b.WriteString("\n    " + muted.Render(h.Detail))
		}
	}
	return b.String()
}

func (m *Model) viewXrefOverlay(bg string) string {
	body := m.overlayBody(m.xrefLoading, "Resolving relationships…", m.xrefErr, &m.xrefViewport)
	return m.overlayFrame("Where used: "+m.xrefTitle, body, "↑/↓ scroll  •  Esc/x close")
}

// renderXref builds the scrollable body of the cross-reference overlay: each
// relationship group with its members.
func (m *Model) renderXref() string {
	return renderResourceGroups(m.xrefGroups, "No related resources found in this VPC.")
}

// renderResourceGroups renders labelled resource groups, shared by the
// cross-reference and public-exposure overlays.
func renderResourceGroups(groups []xrefGroup, emptyMsg string) string {
	if len(groups) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(emptyMsg)
	}
	groupStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent()))
	itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(groupStyle.Render(g.Label) + countStyle.Render(fmt.Sprintf("  (%d)", len(g.Items))))
		for _, it := range g.Items {
			b.WriteString("\n  • " + itemStyle.Render(it))
		}
	}
	return b.String()
}

func (m *Model) viewEffRulesOverlay(bg string) string {
	body := m.overlayBody(m.effRulesLoading, "Merging security group rules…", m.effRulesErr, &m.effRulesVP)
	return m.overlayFrame("Effective rules: "+m.effRules.ENIID, body, "↑/↓ scroll  •  Esc/e close")
}

// renderEffRules builds the scrollable body of the effective-rules overlay: the
// merged inbound and outbound rules in plain English, annotated with the
// contributing security groups, plus the applicable NACL.
func (m *Model) renderEffRules() string {
	if !m.effRules.Found {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("Network interface not found.")
	}
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	wrapW := m.effRulesVP.Width
	if wrapW <= 0 {
		wrapW = 80
	}
	// Wrap a rule explanation, hanging-indented under its bullet.
	bullet := func(text string) string {
		wrapped := lipgloss.NewStyle().Width(wrapW - 4).Render(text)
		lines := strings.Split(wrapped, "\n")
		for i := range lines {
			if i == 0 {
				lines[i] = "  • " + lines[i]
			} else {
				lines[i] = "    " + lines[i]
			}
		}
		return strings.Join(lines, "\n")
	}

	section := func(b *strings.Builder, label string, rules []mergedRule) {
		b.WriteString(heading.Render(label) + muted.Render(fmt.Sprintf("  (%d)", len(rules))))
		if len(rules) == 0 {
			b.WriteString("\n  " + muted.Render("(none)"))
		}
		for _, mr := range rules {
			b.WriteString("\n" + bullet(explainSGRule(mr.Rule)))
			b.WriteString("\n      " + muted.Render("via "+strings.Join(mr.SGs, ", ")))
		}
	}

	var b strings.Builder
	b.WriteString(muted.Render("Security groups: " + strings.Join(m.effRules.SGIDs, ", ")))
	b.WriteString("\n\n")
	section(&b, "Inbound", m.effRules.Inbound)
	b.WriteString("\n\n")
	section(&b, "Outbound", m.effRules.Outbound)
	if m.effRules.NACLID != "" {
		b.WriteString("\n\n" + muted.Render("Network ACL "+m.effRules.NACLID+
			" also applies to this subnet (stateless, evaluated separately)."))
	}
	return b.String()
}

func (m *Model) viewDNSOverlay(bg string) string {
	body := m.overlayBody(m.dnsLoading, "Reading VPC DNS attributes…", m.dnsErr, &m.dnsVP)
	return m.overlayFrame("DNS & VPC attributes: "+m.dnsInfo.VPCID, body, "↑/↓ scroll  •  Esc/D close")
}

// renderDNS builds the scrollable body of the DNS overlay: the VPC's DNS
// attributes followed by plain-English notes.
func (m *Model) renderDNS() string {
	info := m.dnsInfo
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	good := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))

	onOff := func(b bool) string {
		if b {
			return good.Render("Enabled")
		}
		return bad.Render("Disabled")
	}
	dns := "AmazonProvidedDNS (Route 53 Resolver)"
	if usesCustomDNS(info.DomainNameServers) {
		dns = strings.Join(info.DomainNameServers, ", ")
	}
	row := func(label, val string) string {
		return muted.Render(fmt.Sprintf("%-28s", label)) + val
	}

	wrapW := m.dnsVP.Width
	if wrapW <= 0 {
		wrapW = 80
	}

	var b strings.Builder
	b.WriteString(row("DNS resolution", onOff(info.EnableDnsSupport)))
	b.WriteString("\n" + row("DNS hostnames", onOff(info.EnableDnsHostnames)))
	b.WriteString("\n" + row("DHCP options set", text.Render(orDash(info.DhcpOptionsID))))
	b.WriteString("\n" + row("Domain name servers", text.Render(dns)))
	b.WriteString("\n" + row("Domain name", text.Render(orDash(info.DomainName))))

	b.WriteString("\n\n" + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent())).Render("Notes"))
	for _, n := range dnsNotes(info) {
		glyph, style := "•", muted
		switch n.Severity {
		case SevCritical:
			glyph, style = "🔴", bad
		case SevWarning:
			glyph, style = "🟡", lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
		}
		wrapped := lipgloss.NewStyle().Width(wrapW - 4).Render(n.Text)
		lines := strings.Split(wrapped, "\n")
		for i := range lines {
			if i == 0 {
				lines[i] = "  " + style.Render(glyph) + " " + lines[i]
			} else {
				lines[i] = "    " + lines[i]
			}
		}
		b.WriteString("\n" + strings.Join(lines, "\n"))
	}
	return b.String()
}

func (m *Model) viewDiffOverlay(bg string) string {
	added, removed, modified := diffCounts(m.snapDiff)
	titleText := fmt.Sprintf("Changes since baseline — %d added, %d removed, %d modified", added, removed, modified)
	body := m.overlayBody(m.diffLoading, "Comparing with baseline…", m.diffErr, &m.diffVP)
	return m.overlayFrame(titleText, body, "↑/↓ scroll  •  b save current as new baseline  •  Esc/w close")
}

// renderDiff builds the scrollable body of the snapshot-diff overlay.
func (m *Model) renderDiff() string {
	if len(m.snapDiff) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No changes since the baseline snapshot. ✓")
	}
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	remStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
	modStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))

	style := func(k changeKind) lipgloss.Style {
		switch k {
		case changeAdded:
			return addStyle
		case changeRemoved:
			return remStyle
		default:
			return modStyle
		}
	}

	var b strings.Builder
	for i, c := range m.snapDiff {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(style(c.Kind).Render(c.Kind.glyph()+" "+c.Type) + " " + c.ID)
		for _, f := range c.Added {
			b.WriteString("\n    " + addStyle.Render("+ "+f))
		}
		for _, f := range c.Removed {
			b.WriteString("\n    " + remStyle.Render("- "+f))
		}
	}
	return b.String()
}

func (m *Model) viewExposureOverlay(bg string) string {
	body := m.overlayBody(m.exposureLoading, "Computing exposure…", m.exposureErr, &m.exposureVP)
	return m.overlayFrame("Public exposure — internet-facing surface", body, "↑/↓ scroll  •  Esc/P close")
}

// renderExposure builds the scrollable body of the public-exposure overlay.
func (m *Model) renderExposure() string {
	return renderResourceGroups(m.exposureGroups, "Nothing in this VPC is reachable from the internet. ✓")
}

func (m *Model) viewAnalyzerOverlay(bg string) string {
	var body, hint string
	switch {
	case m.analyzerRunning:
		body = m.spinner.View() + "  Running analysis — this can take up to a minute…"
		hint = "Esc close (the analysis keeps running)"
	case m.analyzerInputMode:
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Render("New analysis:")
		body = prompt + " " + m.analyzerInput.View()
		hint = "format: source -> destination[:port]  •  Enter continue  •  Esc cancel"
	case m.analyzerConfirm:
		dst := m.analyzerPendDst
		if m.analyzerPendPort > 0 {
			dst = fmt.Sprintf("%s:%d", dst, m.analyzerPendPort)
		}
		warn := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorWarning()))
		body = warn.Render("⚠ This creates AWS resources and incurs a per-analysis charge (~$0.10).") +
			"\n\n  " + m.analyzerPendSrc + " → " + dst
		hint = "y = create and run  •  n/Esc = cancel"
	case m.analyzerLoading:
		body = m.spinner.View() + "  Loading existing analyses…"
		hint = "Esc close"
	case m.analyzerErr != nil:
		body = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render("Error: " + m.analyzerErr.Error())
		hint = "n new analysis  •  Esc close"
	default:
		body = m.analyzerVP.View()
		hint = "↑/↓ scroll  •  n new analysis (paid)  •  Esc/A close"
	}

	return m.overlayFrame("Reachability Analyzer", body, hint)
}

// renderAnalyzerList renders the existing analyses as a scrollable list.
func (m *Model) renderAnalyzerList() string {
	if len(m.analyzerList) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No Reachability Analyzer analyses found. Press n to create one (paid).")
	}
	glyphStyle := func(a NetInsightsAnalysis) lipgloss.Style {
		switch analysisVerdict(a) {
		case "reachable":
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
		case "not reachable", "failed":
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
		default:
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
		}
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	var b strings.Builder
	for i, a := range m.analyzerList {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(glyphStyle(a).Render(analysisLine(a)))
		if a.StartDate != "" {
			b.WriteString(muted.Render("  " + a.StartDate))
		}
	}
	return b.String()
}

func (m *Model) viewScanStatus() string {
	if m.loading && m.scanning {
		s := m.spinner.View() + fmt.Sprintf("  Scanning %d/%d regions…", m.scanDone, m.scanTotal)
		if m.scanFailed > 0 {
			s += lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).
				Render(fmt.Sprintf("  (%d failed)", m.scanFailed))
		}
		return s
	}
	if m.loading {
		return m.spinner.View() + "  Loading…"
	}
	count := len(m.allVPCs)
	noun := "VPCs"
	if count == 1 {
		noun = "VPC"
	}
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render(fmt.Sprintf("%d %s", count, noun))
	if m.scanFailed > 0 {
		s += lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).
			Render(fmt.Sprintf("  ⚠ %d regions failed — results may be incomplete", m.scanFailed))
	}
	return s
}

func (m *Model) viewStatusBar() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorStatusBarBg())).
		Foreground(lipgloss.Color(ui.ColorStatusBarText())).
		Padding(0, 1)

	var parts []string
	if m.selectedVPC != nil {
		parts = append(parts, m.selectedVPC.ID)
		if m.selectedVPC.Region != "" {
			parts = append(parts, m.selectedVPC.Region)
		}
	} else if m.region != "" {
		parts = append(parts, m.region)
	}

	if m.statusMsg != "" {
		parts = append(parts, m.statusMsg)
	}

	hint := "?=help  S=settings  q=quit"
	switch m.state {
	case stateVPCList:
		hint = "Enter=select  /=search  r=refresh  ?=help  S=settings  q=quit"
	case stateResourceBrowser:
		switch m.focus {
		case focusCategory:
			hint = "↑↓=nav  Enter=load  F=findings  P=exposure  A=analyzer  D=dns  w=changes  Tab=table  Esc=back  q=quit"
		case focusResourceTable:
			hint = "↑↓=nav  Enter=detail  x=where-used  e=eff-rules  F=findings  D=dns  w=changes  t=trace  Esc=back  q=quit"
		}
	}

	left := strings.Join(parts, "  │  ")
	leftW := ansi.StringWidth(left)
	hintW := ansi.StringWidth(hint)
	barW := m.width
	if barW < leftW+hintW+4 {
		barW = leftW + hintW + 4
	}

	gap := barW - leftW - hintW - 2
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + hint
	return style.Width(barW).Render(bar)
}

func (m *Model) helpText() string {
	lines := []string{
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("VPC List"),
		"  Enter    Open resource browser for selected VPC",
		"  /        Filter VPCs by name/ID",
		"  r        Refresh VPC list",
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("Resource Browser"),
		"  ↑ ↓      Navigate category sidebar or resource table",
		"  < >      Scroll table columns left/right (when wider than panel)",
		"  Tab      Switch focus between sidebar and resource table",
		"  Enter    Load resource type (sidebar) / open detail (table)",
		"  c        Copy resource ID to clipboard",
		"  F        Run the VPC findings linter (security/routing/capacity issues)",
		"  P        Public exposure: what is reachable from the internet",
		"  A        AWS Reachability Analyzer: list analyses; n creates one (paid)",
		"  D        Show the VPC's DNS configuration (resolution, hostnames, DHCP)",
		"  w        What changed: baseline the VPC, then diff against it later",
		"  E        Export a Markdown report (resources + findings) to a file",
		"  t        Trace connectivity from the selected network interface",
		"  x        Cross-reference the selected resource (where used)",
		"  e        Effective merged security rules (network interface)",
		"  r        Refresh current resource list",
		"  Esc      Go back to VPC list",
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("Detail Overlay"),
		"  ↑ ↓      Scroll",
		"  Esc      Close",
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("Global"),
		"  S        Settings (theme & colors)",
		"  ?        Toggle help",
		"  q        Quit",
	}
	return strings.Join(lines, "\n")
}

// clipLines truncates every line of s to at most w display columns, in an
// ANSI-aware way so styled (e.g. highlighted) lines keep their escape codes
// intact. It prevents width-constrained containers from wrapping over-wide
// table rows, which would otherwise break column alignment.
func clipLines(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ansi.StringWidth(ln) > w {
			lines[i] = ansi.Truncate(ln, w, "")
		}
	}
	return strings.Join(lines, "\n")
}
