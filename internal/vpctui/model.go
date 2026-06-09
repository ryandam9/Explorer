package vpctui

import (
	"context"
	"fmt"
	"strings"

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

type resourcesLoadedMsg struct {
	rt   resourceType
	maps []map[string]string
	err  error
}

type errMsg struct{ err error }

type findingsLoadedMsg struct {
	findings []Finding
	err      error
}

type traceDoneMsg struct {
	result traceResult
	err    error
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
	if cached, ok := m.resourceMaps[rt]; ok {
		return func() tea.Msg {
			return resourcesLoadedMsg{rt: rt, maps: cached}
		}
	}
	client := m.client
	vpcID := m.selectedVPC.ID
	return func() tea.Msg {
		maps, err := fetchResourceMaps(client, rt, vpcID)
		return resourcesLoadedMsg{rt: rt, maps: maps, err: err}
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
			return findingsLoadedMsg{err: err}
		}
		return findingsLoadedMsg{findings: analyzeVPC(snap)}
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
			return traceDoneMsg{err: err}
		}
		return traceDoneMsg{result: tracePath(snap, req)}
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
// engine. It is best-effort: a single resource type that fails (e.g. missing
// permissions) does not abort the rest. It only returns an error if every
// fetch failed.
func buildVPCSnapshot(c *VPCClient, vpcID string) (vpcSnapshot, error) {
	snap := vpcSnapshot{VPCID: vpcID}
	var firstErr error
	ok := 0
	record := func(err error) {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		ok++
	}

	subnets, err := c.ListSubnets(vpcID)
	record(err)
	snap.Subnets = subnets

	sgs, err := c.ListSecurityGroups(vpcID)
	record(err)
	snap.SecurityGroups = sgs

	rts, err := c.ListRouteTables(vpcID)
	record(err)
	snap.RouteTables = rts

	igws, err := c.ListInternetGateways(vpcID)
	record(err)
	snap.InternetGateways = igws

	nats, err := c.ListNatGateways(vpcID)
	record(err)
	snap.NatGateways = nats

	nacls, err := c.ListNetworkACLs(vpcID)
	record(err)
	snap.NetworkACLs = nacls

	peerings, err := c.ListPeeringConnections(vpcID)
	record(err)
	snap.Peerings = peerings

	endpoints, err := c.ListVPCEndpoints(vpcID)
	record(err)
	snap.Endpoints = endpoints

	enis, err := c.ListNetworkInterfaces(vpcID)
	record(err)
	snap.NetworkInterfaces = enis

	if ok == 0 && firstErr != nil {
		return snap, firstErr
	}
	return snap, nil
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
		m.findingsLoading = false
		m.findingsErr = msg.err
		m.findings = msg.findings
		m.findingsViewport.SetContent(m.renderFindings())
		m.findingsViewport.GotoTop()

	case traceDoneMsg:
		m.traceLoading = false
		m.traceErr = msg.err
		m.traceResult = msg.result
		m.traceViewport.SetContent(m.renderTraceResult())
		m.traceViewport.GotoTop()

	case errMsg:
		m.err = msg.err
		m.loading = false

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Detail overlay: capture scrolling keys.
	if m.showDetail {
		switch key {
		case "esc", "q", "enter":
			m.showDetail = false
		case "up", "k":
			m.detailViewport.LineUp(1)
		case "down", "j":
			m.detailViewport.LineDown(1)
		case "pgup":
			m.detailViewport.HalfViewUp()
		case "pgdown":
			m.detailViewport.HalfViewDown()
		}
		return m, nil
	}

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

	// Trace result overlay: capture scrolling keys.
	if m.showTraceResult {
		switch key {
		case "esc", "q", "t":
			m.showTraceResult = false
		case "up", "k":
			m.traceViewport.LineUp(1)
		case "down", "j":
			m.traceViewport.LineDown(1)
		}
		return m, nil
	}

	// Findings overlay: capture scrolling keys.
	if m.showFindings {
		switch key {
		case "esc", "q", "F":
			m.showFindings = false
		case "up", "k":
			m.findingsViewport.LineUp(1)
		case "down", "j":
			m.findingsViewport.LineDown(1)
		case "pgup":
			m.findingsViewport.HalfViewUp()
		case "pgdown", " ":
			m.findingsViewport.HalfViewDown()
		case "g", "home":
			m.findingsViewport.GotoTop()
		case "G", "end":
			m.findingsViewport.GotoBottom()
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

	// Findings overlay viewport: leave room for the border, title and footer.
	m.findingsViewport.Width = dvW
	m.findingsViewport.Height = dvH - 2
	if m.findingsViewport.Height < 3 {
		m.findingsViewport.Height = 3
	}
	// Re-wrap the findings content to the new width when already loaded.
	if len(m.findings) > 0 {
		m.findingsViewport.SetContent(m.renderFindings())
	}

	// Trace result viewport mirrors the findings overlay sizing.
	m.traceViewport.Width = dvW
	m.traceViewport.Height = dvH - 2
	if m.traceViewport.Height < 3 {
		m.traceViewport.Height = 3
	}
	if len(m.traceResult.Hops) > 0 {
		m.traceViewport.SetContent(m.renderTraceResult())
	}
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
		if len(label) > vpcPanelInner-2 {
			label = label[:vpcPanelInner-5] + "..."
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
			if len(label) > catPanelInner-2 {
				label = label[:catPanelInner-5] + "..."
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

func (m *Model) viewDetailOverlay(bg string) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(m.detailTitle)

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll  •  Esc close")

	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", m.detailViewport.View(), "", hint)

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

func (m *Model) viewFindingsOverlay(bg string) string {
	var titleText string
	if m.findingsLoading {
		titleText = "VPC Findings"
	} else {
		crit, warn, info := countBySeverity(m.findings)
		titleText = fmt.Sprintf("VPC Findings — %d critical, %d warning, %d info", crit, warn, info)
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(titleText)

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll  •  Esc/F close")

	var bodyView string
	switch {
	case m.findingsLoading:
		bodyView = m.spinner.View() + "  Analyzing VPC…"
	case m.findingsErr != nil:
		bodyView = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Error: " + m.findingsErr.Error())
	default:
		bodyView = m.findingsViewport.View()
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", bodyView, "", hint)

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
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("Trace connectivity from " + m.traceSourceID)

	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
		Render("Destination:")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("Enter an IP and port (e.g. 10.0.1.20:3306) or internet:443  •  Enter run  •  Esc cancel")

	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", prompt+" "+m.traceInput.View(), "", hint)

	overlay := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(1, 2).
		Render(inner)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	}
	return overlay
}

func (m *Model) viewTraceResultOverlay(bg string) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("Connectivity Trace")

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("↑/↓ scroll  •  Esc/t close")

	var body string
	switch {
	case m.traceLoading:
		body = m.spinner.View() + "  Tracing path…"
	case m.traceErr != nil:
		body = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Error: " + m.traceErr.Error())
	default:
		body = m.traceViewport.View()
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint)

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

func (m *Model) viewScanStatus() string {
	if m.loading && m.scanning {
		return m.spinner.View() + fmt.Sprintf("  Scanning %d/%d regions…", m.scanDone, m.scanTotal)
	}
	if m.loading {
		return m.spinner.View() + "  Loading…"
	}
	count := len(m.allVPCs)
	noun := "VPCs"
	if count == 1 {
		noun = "VPC"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render(fmt.Sprintf("%d %s", count, noun))
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
			hint = "↑↓=navigate  Enter=load  Tab=resource table  F=findings  Esc=back  ?=help  q=quit"
		case focusResourceTable:
			hint = "↑↓=nav  <>=cols  Enter=detail  c=copy  F=findings  t=trace  r=refresh  Tab=cats  Esc=back  q=quit"
		}
	}

	left := strings.Join(parts, "  │  ")
	barW := m.width
	if barW < len(left)+len(hint)+4 {
		barW = len(left) + len(hint) + 4
	}

	gap := barW - len(left) - len(hint) - 2
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
		"  t        Trace connectivity from the selected network interface",
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
