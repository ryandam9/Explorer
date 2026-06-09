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
	scanTotal int
	scanDone  int
	scanning  bool
	seenVPCs  map[string]bool
	allRegions bool

	// VPC search
	inVPCSearch bool
	vpcSearch   textinput.Model

	// Resource browser (stateResourceBrowser)
	selectedVPC     *VPCInfo
	sidebarItems    []sidebarItem
	activeSidebarIdx int
	activeResource  resourceType

	resourceTable   table.Model
	resourceMaps    map[resourceType][]map[string]string
	resourceLoading bool
	resourceErr     error

	// Detail overlay
	showDetail     bool
	detailViewport viewport.Model
	detailTitle    string

	// UI dimensions
	width  int
	height int

	spinner  spinner.Model
	statusMsg string
	err      error
	loading  bool

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
	m.resourceTable = table.New(table.WithColumns(cols), table.WithFocused(false), table.WithHeight(15))
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
	case "r":
		return m, m.refreshResources()
	case "c":
		row := m.resourceTable.SelectedRow()
		if len(row) >= 2 {
			_ = clipboard.WriteAll(row[1])
			m.statusMsg = "Copied: " + row[1]
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
}

// firstID returns a human-readable identifier from a resource map, trying
// common primary-key field names in order.
func firstID(r map[string]string) string {
	for _, k := range []string{"instance_id", "subnet_id", "sg_id", "rt_id", "igw_id",
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
	panelH := m.height - 1 // reserve 1 for status bar
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

	content := lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), "", body)

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
			hint = "↑↓=navigate  Enter=load  Tab=resource table  Esc=back  ?=help  q=quit"
		case focusResourceTable:
			hint = "↑↓=navigate  Enter=detail  c=copy  r=refresh  Tab=categories  Esc=back  ?=help  q=quit"
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
		"  Tab      Switch focus between sidebar and resource table",
		"  Enter    Load resource type (sidebar) / open detail (table)",
		"  c        Copy resource ID to clipboard",
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
