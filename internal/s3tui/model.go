package s3tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Name   string
	Colors []string
}

var themes = []Theme{
	{Name: "theme1", Colors: []string{"#6260FF", "#E4E4FF"}},
	{Name: "theme2", Colors: []string{"#9FE870", "#163300"}},
	{Name: "theme3", Colors: []string{"#BDD9D7", "#03363D"}},
	{Name: "theme4", Colors: []string{"#3447AA", "#FBEAEB"}},
	{Name: "theme5", Colors: []string{"#FCDB32", "#141D38"}},
	{Name: "theme6", Colors: []string{"#34E0A1", "#000000"}},
}

var activeTheme int

func themeNames() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	return names
}

func lookupTheme(name string) (int, bool) {
	for i, t := range themes {
		if strings.EqualFold(t.Name, name) {
			return i, true
		}
	}
	return 0, false
}

func featherColor(shade int) string {
	colors := themes[activeTheme].Colors
	return colors[shade%len(colors)]
}

func appStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Margin(1, 2).
		Foreground(lipgloss.Color(featherColor(0)))
}

func headerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(featherColor(0))).
		Bold(true).
		Padding(0, 1).
		MarginBottom(1)
}

func panelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(featherColor(1))).
		Foreground(lipgloss.Color(featherColor(0))).
		Padding(0, 1)
}

func selectedPanelStyle() lipgloss.Style {
	return panelStyle().
		BorderForeground(lipgloss.Color(featherColor(1))).
		Foreground(lipgloss.Color(featherColor(0)))
}

func panelTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(featherColor(1))).
		Bold(true)
}

func badgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(featherColor(0))).
		Bold(true).
		Padding(0, 1)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(1)))
}

func errorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(0)))
}

func infoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(1)))
}

func loadingBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(featherColor(1))).
		Foreground(lipgloss.Color(featherColor(0))).
		Padding(1, 3).
		Align(lipgloss.Center)
}

type state int

const (
	stateBucketList state = iota
	stateObjectList
)

type focus int

const (
	focusBuckets focus = iota
	focusObjects
	focusPrefixInput
)

type BucketDetails struct {
	Versioning        string
	Encryption        string
	Tags              map[string]string
	Policy            string
	LifecycleRules    int
	PublicAccessBlock string
}

type Model struct {
	client *S3Client
	state  state
	focus  focus

	profile string
	region  string
	bucket  string
	prefix  string

	width   int
	height  int
	err     error
	loading bool

	sortCol int
	sortAsc bool

	bucketTable table.Model
	objectTable table.Model
	prefixInput textinput.Model
	spinner     spinner.Model

	// Stats
	objCount  int
	totalSize int64

	lastSelectedKey       string
	selectedDetails       *ObjectDetails
	selectedBucketDetails *BucketDetails
	detailsLoading        bool
	showHelp              bool
	showPreview           bool
	previewKey            string
	previewContent        string
	previewLoading        bool
	previewErr            error
	bucketRegionCache     map[string]string
	themeIdx              int
}

func NewModel(ctx context.Context, profile, region, bucket, prefix, themeName string) (*Model, error) {
	client, err := NewS3Client(ctx, profile, region)
	if err != nil {
		return nil, err
	}

	themeIdx := 0
	if idx, ok := lookupTheme(themeName); ok {
		themeIdx = idx
	}
	activeTheme = themeIdx

	m := &Model{
		client:            client,
		profile:           profile,
		region:            region,
		bucket:            bucket,
		prefix:            prefix,
		sortAsc:           true,
		bucketRegionCache: make(map[string]string),
		themeIdx:          themeIdx,
	}

	m.initBucketTable()
	m.initObjectTable()

	m.prefixInput = textinput.New()
	m.prefixInput.Placeholder = "Enter prefix (e.g. photos/2024/)"
	m.prefixInput.CharLimit = 256
	m.prefixInput.Width = 50
	m.prefixInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(1))).Bold(true)
	m.prefixInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(0)))
	m.prefixInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(1)))
	m.prefixInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(0)))

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(0))).Bold(true)),
	)

	if bucket != "" {
		m.state = stateObjectList
		m.focus = focusObjects
	} else {
		m.state = stateBucketList
		m.focus = focusBuckets
	}

	return m, nil
}

func (m *Model) sortObjects(rows []table.Row) {
	if len(rows) <= 1 {
		return
	}

	var dirs []table.Row
	var objs []table.Row
	for _, r := range rows {
		if len(r) > 3 && r[3] == "DIR" {
			dirs = append(dirs, r)
		} else {
			objs = append(objs, r)
		}
	}

	sort.SliceStable(objs, func(i, j int) bool {
		if m.sortCol == 1 {
			left := parseSize(objs[i][1])
			right := parseSize(objs[j][1])
			if m.sortAsc {
				return left < right
			}
			return left > right
		}

		left := objs[i][m.sortCol]
		right := objs[j][m.sortCol]
		if m.sortCol == 0 {
			left = strings.ToLower(left)
			right = strings.ToLower(right)
		}
		if m.sortAsc {
			return left < right
		}
		return left > right
	})

	copy(rows, dirs)
	copy(rows[len(dirs):], objs)
}

func (m *Model) updateObjectColumns() {
	objectWidth := max(40, m.width-10)
	nameWidth := max(18, objectWidth-89)
	m.objectTable.SetColumns([]table.Column{
		{Title: sortTitle("Name", 0, m.sortCol, m.sortAsc), Width: nameWidth},
		{Title: sortTitle("Size", 1, m.sortCol, m.sortAsc), Width: 10},
		{Title: sortTitle("Last Modified", 2, m.sortCol, m.sortAsc), Width: 19},
		{Title: sortTitle("Storage Class", 3, m.sortCol, m.sortAsc), Width: 14},
		{Title: sortTitle("ETag", 4, m.sortCol, m.sortAsc), Width: 32},
	})
}

func (m *Model) initBucketTable() {
	columns := []table.Column{
		{Title: "Name", Width: 40},
		{Title: "Region", Width: 20},
		{Title: "Creation Date", Width: 25},
	}

	m.bucketTable = table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(lipgloss.Color(featherColor(1))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(featherColor(1))).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(featherColor(0)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(featherColor(0))).
		Background(lipgloss.Color(featherColor(1))).
		Bold(true)
	m.bucketTable.SetStyles(s)
}

func (m *Model) initObjectTable() {
	columns := []table.Column{
		{Title: sortTitle("Name", 0, m.sortCol, m.sortAsc), Width: 40},
		{Title: sortTitle("Size", 1, m.sortCol, m.sortAsc), Width: 10},
		{Title: sortTitle("Last Modified", 2, m.sortCol, m.sortAsc), Width: 20},
		{Title: sortTitle("Storage Class", 3, m.sortCol, m.sortAsc), Width: 15},
		{Title: sortTitle("ETag", 4, m.sortCol, m.sortAsc), Width: 34},
	}

	m.objectTable = table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(lipgloss.Color(featherColor(1))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(featherColor(1))).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(featherColor(0)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(featherColor(0))).
		Background(lipgloss.Color(featherColor(1))).
		Bold(true)
	m.objectTable.SetStyles(s)
}

type bucketsLoadedMsg struct {
	rows []table.Row
}

type objectsLoadedMsg struct {
	rows  []table.Row
	count int
	size  int64
}

type objectDetailsMsg struct {
	key     string
	details *ObjectDetails
	err     error
}

type objectPreviewMsg struct {
	key     string
	content string
	err     error
}

type bucketDetailsMsg struct {
	bucket  string
	details *BucketDetails
	err     error
}

type bucketRegionMsg struct {
	idx    int
	region string
}

func (m *Model) fetchBucketDetails(bucket string) tea.Cmd {
	m.detailsLoading = true
	return func() tea.Msg {
		details := m.client.FetchBucketDetails(bucket)
		return bucketDetailsMsg{
			bucket:  bucket,
			details: details,
		}
	}
}

func (m *Model) fetchObjectDetails(key string) tea.Cmd {
	m.detailsLoading = true
	bucket := m.bucket
	return func() tea.Msg {
		details, err := m.client.GetObjectDetails(bucket, key)
		return objectDetailsMsg{
			key:     key,
			details: details,
			err:     err,
		}
	}
}

func (m *Model) fetchObjectPreview(key string) tea.Cmd {
	m.previewLoading = true
	m.previewErr = nil
	m.previewContent = ""
	bucket := m.bucket
	return func() tea.Msg {
		content, err := m.client.GetObjectPreview(bucket, key, 64*1024)
		return objectPreviewMsg{key: key, content: content, err: err}
	}
}

type errMsg struct{ err error }

func (m *Model) loadBuckets() tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		buckets, err := m.client.ListBuckets()
		if err != nil {
			return errMsg{err}
		}

		rows := make([]table.Row, len(buckets))
		for i, b := range buckets {
			dateStr := ""
			if b.CreationDate != nil {
				dateStr = b.CreationDate.Format("2006-01-02 15:04:05")
			}
			name := aws.ToString(b.Name)

			region, ok := m.bucketRegionCache[name]
			if !ok {
				region = "..."
			}

			rows[i] = table.Row{name, region, dateStr}
		}

		return bucketsLoadedMsg{rows}
	}
}

func (m *Model) fetchBucketRegions() tea.Cmd {
	rows := m.bucketTable.Rows()
	if len(rows) == 0 {
		return nil
	}

	sem := make(chan struct{}, 20)

	cmds := make([]tea.Cmd, 0, len(rows))
	for i, row := range rows {
		name := row[0]
		if row[1] != "..." {
			continue
		}
		if _, ok := m.bucketRegionCache[name]; ok {
			continue
		}
		idx := i
		bucketName := name
		cmds = append(cmds, func() tea.Msg {
			// Double-check cache in case another goroutine resolved it
			if region, ok := m.bucketRegionCache[bucketName]; ok {
				return bucketRegionMsg{idx: idx, region: region}
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			region := m.client.GetBucketRegion(bucketName, m.region)
			m.bucketRegionCache[bucketName] = region
			return bucketRegionMsg{idx: idx, region: region}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) loadObjects() tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		res, err := m.client.ListObjects(m.bucket, m.prefix)
		if err != nil {
			return errMsg{fmt.Errorf("access denied or region mismatch for bucket '%s': %w", m.bucket, err)}
		}

		var rows []table.Row
		var count int
		var totalSize int64

		// Add ".." navigation if we are inside a prefix
		if m.prefix != "" {
			rows = append(rows, table.Row{"..", "-", "-", "DIR", "-"})
		}

		for _, p := range res.Prefixes {
			name := aws.ToString(p.Prefix)
			// Remove the current prefix from the displayed name
			if m.prefix != "" && strings.HasPrefix(name, m.prefix) {
				name = strings.TrimPrefix(name, m.prefix)
			}
			rows = append(rows, table.Row{name, "-", "-", "DIR", "-"})
		}

		for _, o := range res.Objects {
			name := aws.ToString(o.Key)
			if m.prefix != "" && strings.HasPrefix(name, m.prefix) {
				name = strings.TrimPrefix(name, m.prefix)
			}
			if name == "" { // Ignore the prefix folder object itself
				continue
			}

			count++
			sizeBytes := aws.ToInt64(o.Size)
			totalSize += sizeBytes

			size := formatSize(sizeBytes)

			date := ""
			if o.LastModified != nil {
				date = o.LastModified.Format("2006-01-02 15:04:05")
			}

			class := string(o.StorageClass)
			if class == "" {
				class = "STANDARD"
			}

			etag := aws.ToString(o.ETag)
			etag = strings.Trim(etag, "\"")

			rows = append(rows, table.Row{name, size, date, class, etag})
		}

		m.sortObjects(rows)
		return objectsLoadedMsg{rows, count, totalSize}
	}
}

func (m *Model) Init() tea.Cmd {
	if m.state == stateBucketList {
		return tea.Batch(m.loadBuckets(), m.startSpinner())
	}
	return tea.Batch(m.loadObjects(), m.startSpinner())
}

func (m *Model) isWaiting() bool {
	return m.loading || m.detailsLoading || m.previewLoading
}

func (m *Model) startSpinner() tea.Cmd {
	return func() tea.Msg {
		return m.spinner.Tick()
	}
}

func (m *Model) loadingLine(message string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center, m.spinner.View(), " ", message)
}

func (m *Model) loadingBox(message, detail string) string {
	lines := []string{m.loadingLine(message)}
	if detail != "" {
		lines = append(lines, "", mutedStyle().Render(detail))
	}
	return loadingBoxStyle().Render(lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func featherRail(width int) string {
	if width < 1 {
		return ""
	}

	var b strings.Builder
	cellCount := 0
	for _, color := range themes[activeTheme].Colors {
		if cellCount > 0 && cellCount%width == 0 {
			b.WriteString("\n")
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("━"))
		cellCount++
	}
	return b.String()
}

func (m *Model) selectedObjectKey() (string, bool) {
	row := m.objectTable.SelectedRow()
	if len(row) == 0 || row[3] == "DIR" {
		return "", false
	}
	return m.prefix + row[0], true
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	spinnerTickScheduled := false

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.isWaiting() {
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
				spinnerTickScheduled = true
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		bucketTableHeight := m.height - 18
		if bucketTableHeight < 5 {
			bucketTableHeight = 5
		}
		m.bucketTable.SetHeight(bucketTableHeight)
		bucketWidth := max(30, m.width-10)
		m.bucketTable.SetColumns([]table.Column{
			{Title: "Name", Width: max(20, bucketWidth-50)},
			{Title: "Region", Width: 16},
			{Title: "Creation Date", Width: 22},
		})

		// Adjust table height
		tableHeight := (m.height / 2) - 4
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.objectTable.SetHeight(tableHeight)
		m.updateObjectColumns()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "s":
			if m.state == stateObjectList && m.focus != focusPrefixInput {
				m.sortCol = (m.sortCol + 1) % 5
				rows := m.objectTable.Rows()
				m.sortObjects(rows)
				m.objectTable.SetRows(rows)
				m.updateObjectColumns()
				return m, nil
			}
		case "r":
			if m.state == stateObjectList && m.focus != focusPrefixInput {
				m.sortAsc = !m.sortAsc
				rows := m.objectTable.Rows()
				m.sortObjects(rows)
				m.objectTable.SetRows(rows)
				m.updateObjectColumns()
				return m, nil
			}
		case "esc":
			m.err = nil // Clear any existing errors
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			if m.showPreview {
				m.showPreview = false
				m.previewLoading = false
				return m, nil
			}
			if m.focus == focusPrefixInput {
				m.focus = focusObjects
				m.prefixInput.Blur()
			} else if m.state == stateObjectList {
				m.state = stateBucketList
				m.focus = focusBuckets
				m.bucket = ""
				m.prefix = ""
				cmds = append(cmds, m.loadBuckets())
			}
		case "/":
			if m.state == stateObjectList && m.focus != focusPrefixInput {
				m.focus = focusPrefixInput
				m.prefixInput.Focus()
				return m, nil
			}
		case "p":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					m.showPreview = true
					m.previewKey = key
					cmds = append(cmds, m.fetchObjectPreview(key))
				}
			}
		case "enter":
			if m.focus == focusPrefixInput {
				m.focus = focusObjects
				m.prefixInput.Blur()
				m.prefix = m.prefixInput.Value()
				if m.prefix != "" && !strings.HasSuffix(m.prefix, "/") {
					m.prefix += "/"
					m.prefixInput.SetValue(m.prefix)
				}
				cmds = append(cmds, m.loadObjects())
			} else if m.state == stateBucketList {
				row := m.bucketTable.SelectedRow()
				if len(row) > 0 {
					m.bucket = row[0]
					m.region = row[1]

					// Re-initialize client for the correct bucket region to avoid ListObjectsV2 InvalidRegion errors
					newClient, err := NewS3Client(m.client.ctx, m.profile, m.region)
					if err == nil {
						m.client = newClient
					}

					m.state = stateObjectList
					m.focus = focusObjects
					cmds = append(cmds, m.loadObjects())
				}
			} else if m.state == stateObjectList && m.focus == focusObjects {
				row := m.objectTable.SelectedRow()
				if len(row) > 0 {
					name := row[0]
					class := row[3]
					if class == "DIR" {
						if name == ".." {
							m.prefix = parentPrefix(m.prefix)
						} else {
							m.prefix += name
						}
						m.prefixInput.SetValue(m.prefix)
						cmds = append(cmds, m.loadObjects())
					} else if key, ok := m.selectedObjectKey(); ok {
						m.showPreview = true
						m.previewKey = key
						cmds = append(cmds, m.fetchObjectPreview(key))
					}
				}
			}
		}

	case bucketsLoadedMsg:
		m.loading = false
		m.err = nil
		m.bucketTable.SetRows(msg.rows)
		if len(msg.rows) > 0 {
			m.bucket = msg.rows[0][0]
			cmds = append(cmds, m.fetchBucketDetails(msg.rows[0][0]))
		}
		cmds = append(cmds, m.fetchBucketRegions())

	case bucketRegionMsg:
		rows := m.bucketTable.Rows()
		if msg.idx < len(rows) {
			rows[msg.idx][1] = msg.region
			m.bucketTable.SetRows(rows)
		}

	case objectsLoadedMsg:
		m.loading = false
		m.err = nil
		m.objCount = msg.count
		m.totalSize = msg.size
		m.objectTable.SetRows(msg.rows)

		// Reset details when loading new objects
		m.lastSelectedKey = ""
		m.selectedDetails = nil

		if len(msg.rows) > 0 && msg.rows[0][3] != "DIR" {
			m.lastSelectedKey = m.prefix + msg.rows[0][0]
			cmds = append(cmds, m.fetchObjectDetails(m.lastSelectedKey))
		}

	case objectDetailsMsg:
		m.detailsLoading = false
		// Only apply if it's still the selected object
		if msg.key == m.lastSelectedKey {
			if msg.err == nil {
				m.selectedDetails = msg.details
			}
		}

	case bucketDetailsMsg:
		if msg.bucket == m.bucket {
			m.detailsLoading = false
			if msg.err == nil {
				m.selectedBucketDetails = msg.details
			}
		}

	case objectPreviewMsg:
		if msg.key == m.previewKey {
			m.previewLoading = false
			m.previewErr = msg.err
			m.previewContent = msg.content
		}

	case errMsg:
		m.loading = false
		m.detailsLoading = false
		m.previewLoading = false
		m.err = msg.err
	}

	// Route updates
	if m.focus == focusPrefixInput {
		m.prefixInput, cmd = m.prefixInput.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.state == stateBucketList {
		m.bucketTable, cmd = m.bucketTable.Update(msg)
		cmds = append(cmds, cmd)

		if m.state == stateBucketList {
			row := m.bucketTable.SelectedRow()
			if len(row) > 0 && (m.selectedBucketDetails == nil || m.bucket != row[0]) {
				m.bucket = row[0]
				cmds = append(cmds, m.fetchBucketDetails(row[0]))
			}
		}
	} else if m.state == stateObjectList {
		prevRow := m.objectTable.Cursor()
		m.objectTable, cmd = m.objectTable.Update(msg)
		cmds = append(cmds, cmd)

		// If cursor changed, fetch details
		if m.focus == focusObjects && prevRow != m.objectTable.Cursor() && len(m.objectTable.SelectedRow()) > 0 {
			row := m.objectTable.SelectedRow()
			if row[3] != "DIR" {
				newKey := m.prefix + row[0]
				if newKey != m.lastSelectedKey {
					m.lastSelectedKey = newKey
					m.selectedDetails = nil // clear old details immediately
					cmds = append(cmds, m.fetchObjectDetails(newKey))
				}
			} else {
				m.lastSelectedKey = ""
				m.selectedDetails = nil
			}
		}
	}

	if m.isWaiting() && !spinnerTickScheduled {
		cmds = append(cmds, m.startSpinner())
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var content string

	headerText := "S3 TUI v1.2.0 (READ-ONLY)"
	if m.profile != "" {
		headerText += fmt.Sprintf("   Profile: %s", m.profile)
	}
	if m.region != "" {
		headerText += fmt.Sprintf("   Region: %s", m.region)
	}
	header := headerStyle().Render(headerText)

	if m.err != nil {
		errBox := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(featherColor(0))).
			Foreground(lipgloss.Color(featherColor(0))).
			Padding(1, 2).
			Align(lipgloss.Center).
			Render(fmt.Sprintf("Failed to access bucket: %s\n\n%s\n\nPress [Esc] to return to the bucket list.", m.bucket, errorStyle().Render(m.err.Error())))

		content = lipgloss.Place(m.width-4, m.height-10, lipgloss.Center, lipgloss.Center, errBox)
	} else if m.loading {
		message := "Loading buckets from AWS..."
		detail := "Resolving bucket regions and preparing the read-only explorer."
		if m.state == stateObjectList {
			message = "Loading S3 objects..."
			detail = fmt.Sprintf("Bucket: %s   Prefix: %s", m.bucket, displayPrefix(m.prefix))
		}
		content = lipgloss.Place(m.width-4, m.height-10, lipgloss.Center, lipgloss.Center, m.loadingBox(message, detail))
	} else {
		if m.state == stateBucketList {
			tableSection := selectedPanelStyle().Render(m.bucketTable.View())

			detailsPanel := "Select a bucket to view details"
			if len(m.bucketTable.SelectedRow()) > 0 {
				row := m.bucketTable.SelectedRow()
				name := row[0]
				region := row[1]
				date := row[2]

				metaText := m.loadingLine("Loading bucket details...")
				if !m.detailsLoading && m.selectedBucketDetails != nil {
					tagStr := ""
					if len(m.selectedBucketDetails.Tags) > 0 {
						for k, v := range m.selectedBucketDetails.Tags {
							tagStr += fmt.Sprintf("[%s: %s] ", k, v)
						}
					} else {
						tagStr = "None"
					}

					metaText = lipgloss.JoinHorizontal(lipgloss.Top,
						lipgloss.JoinVertical(lipgloss.Left,
							fmt.Sprintf("Region:     %s", region),
							fmt.Sprintf("Created:    %s", date),
							fmt.Sprintf("Versioning: %s", m.selectedBucketDetails.Versioning),
							fmt.Sprintf("Encryption: %s", m.selectedBucketDetails.Encryption),
						),
						"    ",
						lipgloss.JoinVertical(lipgloss.Left,
							fmt.Sprintf("Policy:    %s", m.selectedBucketDetails.Policy),
							fmt.Sprintf("Lifecycle: %d rules", m.selectedBucketDetails.LifecycleRules),
							fmt.Sprintf("PAB:       %s", m.selectedBucketDetails.PublicAccessBlock),
							fmt.Sprintf("Tags:      %s", tagStr),
						),
					)
				}

				detailsPanel = lipgloss.NewStyle().
					Width(m.width-4).
					Height(8).
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color(featherColor(1))).
					Foreground(lipgloss.Color(featherColor(0))).
					Padding(0, 1).
					Render(lipgloss.JoinVertical(lipgloss.Left,
						panelTitleStyle().Render(fmt.Sprintf("BUCKET DETAILS: %s", name)),
						"",
						metaText,
					))
			}

			content = lipgloss.JoinVertical(lipgloss.Left,
				tableSection,
				detailsPanel,
			)
		} else {
			sizeStr := formatSize(m.totalSize)

			headerRight := mutedStyle().Render(
				fmt.Sprintf("Objects: %d   Size: %s", m.objCount, sizeStr))

			bucketHeader := lipgloss.JoinHorizontal(lipgloss.Top,
				badgeStyle().Render(fmt.Sprintf("Bucket: %s", m.bucket)),
				"   ",
				headerRight,
			)

			prefixSection := lipgloss.JoinHorizontal(lipgloss.Center,
				lipgloss.NewStyle().Foreground(lipgloss.Color(featherColor(1))).Bold(true).Render("Prefix: "),
				m.prefixInput.View(),
			)

			tableStyle := panelStyle()
			if m.focus == focusObjects {
				tableStyle = selectedPanelStyle()
			}
			tableSection := tableStyle.Render(m.objectTable.View())

			// Details Panel
			detailsPanel := "Select an object to view details"
			if len(m.objectTable.SelectedRow()) > 0 {
				row := m.objectTable.SelectedRow()
				name, size, date, class, etag := row[0], row[1], row[2], row[3], row[4]

				isDir := (class == "DIR")

				details := fmt.Sprintf("Key: %s%s\nSize: %s\nLast Modified: %s\nStorage Class: %s\nETag: %s",
					m.prefix, name, size, date, class, etag)

				detailsBox := lipgloss.NewStyle().
					Width(m.width/2-4).
					Height(8).
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color(featherColor(1))).
					Foreground(lipgloss.Color(featherColor(0))).
					Padding(0, 1).
					Render(lipgloss.JoinVertical(lipgloss.Left,
						panelTitleStyle().Render("OBJECT DETAILS"),
						"",
						details,
					))

				metaText := ""
				if isDir {
					metaText = "Status: N/A"
				} else {
					if m.detailsLoading || m.selectedDetails == nil {
						metaText = m.loadingLine("Loading object metadata...")
					} else {
						// Build tags string
						tagStr := ""
						if len(m.selectedDetails.Tags) > 0 {
							for k, v := range m.selectedDetails.Tags {
								tagStr += fmt.Sprintf("[%s: %s] ", k, v)
							}
						} else {
							tagStr = "None"
						}

						// Build meta string
						customMetaStr := ""
						if len(m.selectedDetails.Metadata) > 0 {
							for k, v := range m.selectedDetails.Metadata {
								customMetaStr += fmt.Sprintf("x-amz-meta-%s: %s\n", k, v)
							}
						} else {
							customMetaStr = "None"
						}

						cType := m.selectedDetails.ContentType
						if cType == "" {
							cType = "unknown"
						}

						metaText = lipgloss.JoinHorizontal(lipgloss.Top,
							lipgloss.JoinVertical(lipgloss.Left,
								fmt.Sprintf("Content-Type: %s", cType),
								fmt.Sprintf("SSE: %s", m.selectedDetails.SSE),
								fmt.Sprintf("Version: %s", m.selectedDetails.VersionID),
							),
							"    ",
							lipgloss.JoinVertical(lipgloss.Left,
								fmt.Sprintf("Tags: %s", tagStr),
								"Metadata:",
								customMetaStr,
							),
						)
					}
				}

				metadataBox := lipgloss.NewStyle().
					Width(m.width/2-4).
					Height(8).
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color(featherColor(1))).
					Foreground(lipgloss.Color(featherColor(0))).
					Padding(0, 1).
					Render(lipgloss.JoinVertical(lipgloss.Left,
						panelTitleStyle().Render("TAGS & METADATA"),
						"",
						metaText,
					))

				detailsPanel = lipgloss.JoinHorizontal(lipgloss.Top, detailsBox, "  ", metadataBox)
			}

			content = lipgloss.JoinVertical(lipgloss.Left,
				bucketHeader,
				"",
				prefixSection,
				"",
				tableSection,
				"",
				detailsPanel,
			)
		}
	}

	if m.showHelp {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.helpView())
	} else if m.showPreview {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.previewView())
	}

	help := infoStyle().Render("[↑/↓] Move | [Enter/p] Preview | [/] Jump Prefix | [Esc] Back/Close | [s] Sort | [r] Reverse | [?] Help | [q] Quit")

	return appStyle().Render(lipgloss.JoinVertical(lipgloss.Left, header, featherRail(max(12, m.width-4)), "", content, "", help))
}

func (m *Model) helpView() string {
	commands := lipgloss.JoinVertical(lipgloss.Left,
		"S3 Explorer Help",
		"",
		"Navigation",
		"  ↑/↓, PgUp/PgDn     Move selection",
		"  Enter              Open bucket, prefix, or object preview",
		"  Esc                Back, close preview/help, or clear prefix input",
		"",
		"Objects",
		"  /                  Jump to prefix",
		"  p                  Preview selected object (read-only, truncated)",
		"  s                  Cycle sort column",
		"  r                  Reverse sort direction",
		"",
		"Utility",
		"  ?                  Toggle this help",
		"  q, Ctrl+C          Quit",
	)
	return lipgloss.NewStyle().
		Width(min(72, max(32, m.width-12))).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(featherColor(1))).
		Foreground(lipgloss.Color(featherColor(0))).
		Padding(1, 2).
		Render(commands)
}

func (m *Model) previewView() string {
	body := m.loadingLine("Loading preview...")
	if m.previewErr != nil {
		body = "Preview failed: " + summarizeS3Error(m.previewErr)
	} else if !m.previewLoading {
		body = m.previewContent
		if body == "" {
			body = "Object is empty."
		}
	}

	width := min(100, max(40, m.width-12))
	height := min(28, max(10, m.height-10))
	title := panelTitleStyle().Render("OBJECT PREVIEW: " + m.previewKey)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(featherColor(0))).
		Foreground(lipgloss.Color(featherColor(0))).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", mutedStyle().Render("Esc closes preview")))
}
