package s3tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/table"
	"github.com/user/aws_explorer/internal/ui"
)

// ---------------------------------------------------------------------------
// State / Focus enumerations
// ---------------------------------------------------------------------------

type state int

const (
	stateBucketList state = iota
	stateObjectList
	stateBucketDetail
)

type focus int

const (
	focusBuckets focus = iota
	focusObjects
	focusPrefixInput
	focusBucketSearch
)

// ---------------------------------------------------------------------------
// BucketDetails struct
// ---------------------------------------------------------------------------

type BucketDetails struct {
	Versioning        string
	Encryption        string
	Tags              map[string]string
	Policy            string
	LifecycleRules    int
	PublicAccessBlock string
	// Extended fields
	ACLSummary         string
	OwnershipControls  string
	PolicyStatus       string
	CORS               string
	Website            string
	Logging            string
	Notifications      string
	RequestPayment     string
	Acceleration       string
	ObjectLock         string
	Replication        string
	MultipartUploads   int
	IntelligentTiering string
}

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

type bucketsLoadedMsg struct {
	rows    []table.Row
	warning string
}

type regionsDiscoveredMsg struct {
	regions []string
}

type regionScannedMsg struct {
	region  string
	buckets []s3types.Bucket
	err     error
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
	name   string
	region string
}

type presignedURLMsg struct {
	key string
	url string
	err error
}

type downloadDoneMsg struct {
	key  string
	path string
	err  error
}

type deleteObjectDoneMsg struct {
	key string
	err error
}

type errMsg struct{ err error }

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type Model struct {
	client *S3Client
	state  state
	focus  focus

	awsCfg      *config.AWSConfig
	region      string
	bucket      string
	prefix      string
	endpointURL string

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

	// Multi-region bucket scan state
	scanTotal   int
	scanDone    int
	scanning    bool
	seenBuckets map[string]bool

	// Bucket search overlay
	inBucketSearch bool
	bucketSearch   textinput.Model
	allBucketRows  []table.Row

	// Bucket detail full-screen view
	detailBucket string
	detailTabIdx int

	// Object browser extras
	flatMode     bool
	showVersions bool

	// Actions
	allowDelete      bool
	confirmingDelete bool
	deleteConfirm    textinput.Model
	deleteKey        string

	copyMenuActive bool
	copyContent    string

	presignedURL  string
	showPresigned bool

	previewViewport viewport.Model

	statusMsg string // transient status shown in footer

	// Shared settings panel (theme & colors). Opened with KeySettings.
	showSettings bool
	settings     ui.SettingsModel
	configPath   string
	cfg          *config.Config
}

// authDisplayInfo returns a short human-readable label for the active auth method.
func authDisplayInfo(cfg *config.AWSConfig) string {
	if cfg == nil {
		return ""
	}
	switch cfg.AuthMethod {
	case "sts":
		if cfg.STS.RoleARN != "" {
			// Show only the role name portion of the ARN for brevity.
			parts := strings.Split(cfg.STS.RoleARN, "/")
			return "Role: " + parts[len(parts)-1]
		}
	case "static":
		return "Auth: static"
	case "env":
		return "Auth: env"
	case "profile":
		if cfg.Profile != "" {
			return "Profile: " + cfg.Profile
		}
	default:
		if cfg.Profile != "" && cfg.Profile != "default" {
			return "Profile: " + cfg.Profile
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// NewModel
// ---------------------------------------------------------------------------

func NewModel(ctx context.Context, awsCfg *config.AWSConfig, region, bucket, prefix, themeName string, allowDelete bool, endpointURL, configPath string, cfg *config.Config) (*Model, error) {
	client, err := NewS3Client(ctx, awsCfg, region, endpointURL)
	if err != nil {
		return nil, err
	}

	themeIdx := 0
	if idx, ok := ui.LookupTheme(themeName); ok {
		themeIdx = idx
	}
	ui.SetActiveTheme(themeIdx)

	m := &Model{
		client:            client,
		awsCfg:            awsCfg,
		region:            region,
		bucket:            bucket,
		prefix:            prefix,
		endpointURL:       endpointURL,
		sortAsc:           true,
		bucketRegionCache: make(map[string]string),
		seenBuckets:       make(map[string]bool),
		themeIdx:          themeIdx,
		allowDelete:       allowDelete,
		configPath:        configPath,
		cfg:               cfg,
	}
	m.settings = ui.NewSettingsModel(0, 0, configPath, cfg)

	m.initBucketTable()
	m.initObjectTable()

	m.prefixInput = textinput.New()
	m.prefixInput.Placeholder = "Enter prefix (e.g. photos/2024/)"
	m.prefixInput.CharLimit = 256
	m.prefixInput.Width = 50
	m.prefixInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1))).Bold(true)
	m.prefixInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))
	m.prefixInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1)))
	m.prefixInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))

	m.bucketSearch = textinput.New()
	m.bucketSearch.Placeholder = "Filter buckets..."
	m.bucketSearch.CharLimit = 128
	m.bucketSearch.Width = 40
	m.bucketSearch.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1))).Bold(true)
	m.bucketSearch.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))
	m.bucketSearch.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1)))
	m.bucketSearch.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))

	m.deleteConfirm = textinput.New()
	m.deleteConfirm.Placeholder = "Type 'delete' to confirm"
	m.deleteConfirm.CharLimit = 32
	m.deleteConfirm.Width = 30
	m.deleteConfirm.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0))).Bold(true)
	m.deleteConfirm.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))
	m.deleteConfirm.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0))).Bold(true)),
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

// ---------------------------------------------------------------------------
// Table initialization
// ---------------------------------------------------------------------------

func (m *Model) initBucketTable() {
	columns := []table.Column{
		{Title: "#", Width: 4},
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
		Foreground(lipgloss.Color(ui.FeatherColor(1))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ui.FeatherColor(1))).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(ui.FeatherColor(0)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(ui.FeatherColor(0))).
		Background(lipgloss.Color(ui.FeatherColor(1))).
		Bold(true)
	m.bucketTable.SetStyles(s)
}

func (m *Model) initObjectTable() {
	columns := []table.Column{
		{Title: "#", Width: 4},
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
		Foreground(lipgloss.Color(ui.FeatherColor(1))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ui.FeatherColor(1))).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(ui.FeatherColor(0)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(ui.FeatherColor(0))).
		Background(lipgloss.Color(ui.FeatherColor(1))).
		Bold(true)
	m.objectTable.SetStyles(s)
}

// restyleForTheme re-applies palette-derived styles to the existing tables and
// inputs without resetting their data. Called after the settings panel saves a
// new theme so the change is visible immediately.
func (m *Model) restyleForTheme() {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(lipgloss.Color(ui.FeatherColor(1))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ui.FeatherColor(1))).
		BorderBottom(true).
		Bold(true)
	s.Cell = s.Cell.Foreground(lipgloss.Color(ui.FeatherColor(0)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(ui.FeatherColor(0))).
		Background(lipgloss.Color(ui.FeatherColor(1))).
		Bold(true)
	m.bucketTable.SetStyles(s)
	m.objectTable.SetStyles(s)

	for _, in := range []*textinput.Model{&m.prefixInput, &m.bucketSearch, &m.deleteConfirm} {
		in.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1))).Bold(true)
		in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))
		in.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1)))
		in.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0)))
	}
}

// ---------------------------------------------------------------------------
// Sort
// ---------------------------------------------------------------------------

func (m *Model) sortObjects(rows []table.Row) {
	if len(rows) <= 1 {
		return
	}

	var dirs []table.Row
	var objs []table.Row
	for _, r := range rows {
		if len(r) > 4 && r[4] == "DIR" {
			dirs = append(dirs, r)
		} else {
			objs = append(objs, r)
		}
	}

	sort.SliceStable(objs, func(i, j int) bool {
		if m.sortCol == 1 {
			left := parseSize(objs[i][2])
			right := parseSize(objs[j][2])
			if m.sortAsc {
				return left < right
			}
			return left > right
		}

		left := objs[i][m.sortCol+1]
		right := objs[j][m.sortCol+1]
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
	nameWidth := max(18, objectWidth-93)
	m.objectTable.SetColumns([]table.Column{
		{Title: "#", Width: 4},
		{Title: sortTitle("Name", 0, m.sortCol, m.sortAsc), Width: nameWidth},
		{Title: sortTitle("Size", 1, m.sortCol, m.sortAsc), Width: 10},
		{Title: sortTitle("Last Modified", 2, m.sortCol, m.sortAsc), Width: 19},
		{Title: sortTitle("Storage Class", 3, m.sortCol, m.sortAsc), Width: 14},
		{Title: sortTitle("ETag", 4, m.sortCol, m.sortAsc), Width: 32},
	})
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

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

func (m *Model) loadBuckets() tea.Cmd {
	// Reset scan state for a clean run (handles refresh too).
	m.loading = true
	m.scanning = false
	m.scanTotal = 0
	m.scanDone = 0
	m.seenBuckets = make(map[string]bool)
	m.allBucketRows = nil
	m.bucketTable.SetRows(nil)
	m.statusMsg = ""

	awsCfg := m.awsCfg
	ctx := m.client.ctx
	return func() tea.Msg {
		regions := ListRegions(ctx, awsCfg)
		return regionsDiscoveredMsg{regions: regions}
	}
}

func (m *Model) fetchBucketRegions() tea.Cmd {
	rows := m.bucketTable.Rows()
	if len(rows) == 0 {
		return nil
	}

	sem := make(chan struct{}, 20)

	// Apply any already-cached regions immediately so a reload doesn't
	// leave the region column stuck at "..." for buckets we've seen before.
	cacheApplied := false
	for i, row := range rows {
		if row[2] != "..." {
			continue
		}
		if region, ok := m.bucketRegionCache[row[1]]; ok {
			rows[i][2] = region
			if i < len(m.allBucketRows) {
				m.allBucketRows[i][2] = region
			}
			cacheApplied = true
		}
	}
	if cacheApplied {
		m.bucketTable.SetRows(seqRows(rows))
	}

	cmds := make([]tea.Cmd, 0, len(rows))
	for i, row := range rows {
		name := row[1]
		if row[2] != "..." {
			continue
		}
		idx := i
		bucketName := name
		cmds = append(cmds, func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()
			region := m.client.GetBucketRegion(bucketName, m.region)
			return bucketRegionMsg{idx: idx, name: bucketName, region: region}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) loadObjects() tea.Cmd {
	m.loading = true
	flat := m.flatMode
	return func() tea.Msg {
		var res *ListObjectsResult
		var err error
		if flat {
			res, err = m.client.ListObjectsFlat(m.bucket, m.prefix)
		} else {
			res, err = m.client.ListObjects(m.bucket, m.prefix)
		}
		if err != nil {
			return errMsg{fmt.Errorf("access denied or region mismatch for bucket '%s': %w", m.bucket, err)}
		}

		var rows []table.Row
		var count int
		var totalSize int64

		// Add ".." navigation if we are inside a prefix (non-flat mode only)
		if m.prefix != "" && !flat {
			rows = append(rows, table.Row{"", "..", "-", "-", "DIR", "-"})
		}

		if !flat {
			for _, p := range res.Prefixes {
				name := aws.ToString(p.Prefix)
				if m.prefix != "" && strings.HasPrefix(name, m.prefix) {
					name = strings.TrimPrefix(name, m.prefix)
				}
				rows = append(rows, table.Row{"", name, "-", "-", "DIR", "-"})
			}
		}

		for _, o := range res.Objects {
			name := aws.ToString(o.Key)
			if m.prefix != "" && strings.HasPrefix(name, m.prefix) {
				name = strings.TrimPrefix(name, m.prefix)
			}
			if name == "" {
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

			rows = append(rows, table.Row{"", name, size, date, class, etag})
		}

		m.sortObjects(rows)
		return objectsLoadedMsg{rows, count, totalSize}
	}
}

func (m *Model) generatePresignCmd(key string) tea.Cmd {
	bucket := m.bucket
	return func() tea.Msg {
		url, err := m.client.PresignGetObject(bucket, key, time.Hour)
		return presignedURLMsg{key: key, url: url, err: err}
	}
}

func (m *Model) downloadObjectCmd(key string) tea.Cmd {
	bucket := m.bucket
	return func() tea.Msg {
		// Download to current directory with the base filename
		localPath := filepath.Base(key)
		err := m.client.DownloadObject(bucket, key, localPath)
		return downloadDoneMsg{key: key, path: localPath, err: err}
	}
}

func (m *Model) deleteObjectCmd(key string) tea.Cmd {
	bucket := m.bucket
	return func() tea.Msg {
		err := m.client.DeleteObject(bucket, key)
		return deleteObjectDoneMsg{key: key, err: err}
	}
}

// ---------------------------------------------------------------------------
// Init / spinner helpers
// ---------------------------------------------------------------------------

func (m *Model) Init() tea.Cmd {
	if m.state == stateBucketList {
		return tea.Batch(m.loadBuckets(), m.startSpinner())
	}
	return tea.Batch(m.loadObjects(), m.startSpinner())
}

func (m *Model) isWaiting() bool {
	return m.loading || m.scanning || m.detailsLoading || m.previewLoading
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
		lines = append(lines, "", ui.MutedStyle().Render(detail))
	}
	return ui.LoadingBoxStyle().Render(lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func (m *Model) selectedObjectKey() (string, bool) {
	row := m.objectTable.SelectedRow()
	if len(row) == 0 || row[4] == "DIR" {
		return "", false
	}
	return m.prefix + row[1], true
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	spinnerTickScheduled := false

	// Route all events to the shared settings panel while it is open.
	if m.showSettings {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" && !m.settings.EditMode() {
				m.showSettings = false
				return m, nil
			}
		case ui.SettingsSavedMsg:
			m.showSettings = false
			m.statusMsg = "Theme saved: " + msg.Theme
			m.restyleForTheme()
			return m, nil
		case ui.SettingsErrMsg:
			m.showSettings = false
			m.statusMsg = "Save failed: " + msg.Err.Error()
			return m, nil
		}
		var scmd tea.Cmd
		m.settings, scmd = m.settings.Update(msg)
		return m, scmd
	}

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
			{Title: "#", Width: 4},
			{Title: "Name", Width: max(20, bucketWidth-54)},
			{Title: "Region", Width: 16},
			{Title: "Creation Date", Width: 22},
		})

		tableHeight := (m.height / 2) - 4
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.objectTable.SetHeight(tableHeight)
		m.updateObjectColumns()

	case tea.KeyMsg:
		// Handle modals / overlays first
		if m.confirmingDelete {
			switch msg.String() {
			case "esc":
				m.confirmingDelete = false
				m.deleteKey = ""
				m.deleteConfirm.SetValue("")
				m.deleteConfirm.Blur()
				return m, nil
			case "enter":
				if m.deleteConfirm.Value() == "delete" {
					key := m.deleteKey
					m.confirmingDelete = false
					m.deleteKey = ""
					m.deleteConfirm.SetValue("")
					m.deleteConfirm.Blur()
					cmds = append(cmds, m.deleteObjectCmd(key))
					return m, tea.Batch(cmds...)
				}
			default:
				m.deleteConfirm, cmd = m.deleteConfirm.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
			return m, nil
		}

		if m.showPresigned {
			if msg.String() == "esc" {
				m.showPresigned = false
				return m, nil
			}
			return m, nil
		}

		if m.copyMenuActive {
			if msg.String() == "esc" || msg.String() == "y" {
				m.copyMenuActive = false
				return m, nil
			}
			return m, nil
		}

		// Global keys — skipped while typing into an input so that bucket
		// names / prefixes containing these characters work as expected.
		if !m.inBucketSearch && m.focus != focusPrefixInput {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case ui.KeyHelp:
				m.showHelp = !m.showHelp
				return m, nil
			case ui.KeySettings:
				m.settings = ui.NewSettingsModel(m.width, m.height, m.configPath, m.cfg)
				m.showSettings = true
				return m, nil
			}
		}

		if m.showHelp {
			if msg.String() == "esc" || msg.String() == "?" {
				m.showHelp = false
			}
			return m, nil
		}
		if m.showPreview {
			if msg.String() == "esc" {
				m.showPreview = false
				m.previewLoading = false
				return m, nil
			}
			// Forward all other keys to the preview viewport for scrolling.
			var vpCmd tea.Cmd
			m.previewViewport, vpCmd = m.previewViewport.Update(msg)
			if vpCmd != nil {
				cmds = append(cmds, vpCmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Bucket search overlay
		if m.inBucketSearch {
			switch msg.String() {
			case "esc":
				m.inBucketSearch = false
				m.bucketSearch.Blur()
				m.bucketSearch.SetValue("")
				// Restore full bucket list
				if len(m.allBucketRows) > 0 {
					m.bucketTable.SetRows(seqRows(m.allBucketRows))
				}
				return m, nil
			case "enter":
				// Select the first visible row
				rows := m.bucketTable.Rows()
				if len(rows) > 0 {
					name := rows[0][1]
					m.inBucketSearch = false
					m.bucketSearch.Blur()
					m.bucketSearch.SetValue("")
					m.bucket = name
					m.region = rows[0][2]
					// Region may still be loading; fall back to the cache.
					if m.region == "..." {
						if cached, ok := m.bucketRegionCache[m.bucket]; ok {
							m.region = cached
						}
					}
					// Re-initialize client for the correct bucket region
					newClient, err := NewS3Client(m.client.ctx, m.awsCfg, m.region, m.endpointURL)
					if err == nil {
						m.client = newClient
					}
					m.state = stateObjectList
					m.focus = focusObjects
					cmds = append(cmds, m.loadObjects())
					return m, tea.Batch(cmds...)
				}
				m.inBucketSearch = false
				m.bucketSearch.Blur()
				return m, nil
			default:
				m.bucketSearch, cmd = m.bucketSearch.Update(msg)
				cmds = append(cmds, cmd)
				// Filter bucket rows
				query := strings.ToLower(m.bucketSearch.Value())
				var filtered []table.Row
				for _, r := range m.allBucketRows {
					if strings.Contains(strings.ToLower(r[1]), query) {
						filtered = append(filtered, r)
					}
				}
				m.bucketTable.SetRows(seqRows(filtered))
				return m, tea.Batch(cmds...)
			}
		}

		// Bucket detail view
		if m.state == stateBucketDetail {
			switch msg.String() {
			case "esc":
				m.state = stateBucketList
				m.focus = focusBuckets
				return m, nil
			case "tab":
				m.detailTabIdx = (m.detailTabIdx + 1) % 5
				return m, nil
			case "shift+tab":
				m.detailTabIdx = (m.detailTabIdx + 4) % 5
				return m, nil
			}
			return m, nil
		}

		// Prefix input handling
		if m.focus == focusPrefixInput {
			switch msg.String() {
			case "esc":
				m.focus = focusObjects
				m.prefixInput.Blur()
				return m, nil
			case "enter":
				m.focus = focusObjects
				m.prefixInput.Blur()
				m.prefix = m.prefixInput.Value()
				if m.prefix != "" && !strings.HasSuffix(m.prefix, "/") {
					m.prefix += "/"
					m.prefixInput.SetValue(m.prefix)
				}
				cmds = append(cmds, m.loadObjects())
				return m, tea.Batch(cmds...)
			default:
				m.prefixInput, cmd = m.prefixInput.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
		}

		// State-specific keys
		switch msg.String() {
		case "esc":
			m.err = nil
			if m.state == stateObjectList {
				m.state = stateBucketList
				m.focus = focusBuckets
				m.bucket = ""
				m.prefix = ""
				cmds = append(cmds, m.loadBuckets())
			}

		case "/":
			if m.state == stateBucketList {
				m.inBucketSearch = true
				m.focus = focusBucketSearch
				// Save all rows for restore
				m.allBucketRows = m.bucketTable.Rows()
				m.bucketSearch.Focus()
				return m, nil
			} else if m.state == stateObjectList {
				m.focus = focusPrefixInput
				m.prefixInput.Focus()
				return m, nil
			}

		case "d":
			if m.state == stateBucketList {
				row := m.bucketTable.SelectedRow()
				if len(row) > 0 {
					m.detailBucket = row[1]
					m.detailTabIdx = 0
					m.state = stateBucketDetail
					// Trigger fetch if needed
					if m.selectedBucketDetails == nil || m.bucket != row[1] {
						m.bucket = row[1]
						cmds = append(cmds, m.fetchBucketDetails(row[1]))
					}
					return m, tea.Batch(cmds...)
				}
			}

		case "f":
			if m.state == stateObjectList {
				m.flatMode = !m.flatMode
				if m.flatMode {
					m.statusMsg = "Flat mode: ON (showing all objects recursively)"
				} else {
					m.statusMsg = "Flat mode: OFF (hierarchical view)"
				}
				cmds = append(cmds, m.loadObjects())
			}

		case "v":
			if m.state == stateObjectList {
				m.showVersions = !m.showVersions
				if m.showVersions {
					m.statusMsg = "Versions: ON (visual indicator only)"
				} else {
					m.statusMsg = "Versions: OFF"
				}
			}

		case "R":
			if m.state == stateObjectList {
				m.sortAsc = !m.sortAsc
				rows := m.objectTable.Rows()
				m.sortObjects(rows)
				m.objectTable.SetRows(seqRows(rows))
				m.updateObjectColumns()
				return m, nil
			}

		case "s":
			if m.state == stateObjectList {
				m.sortCol = (m.sortCol + 1) % 5
				rows := m.objectTable.Rows()
				m.sortObjects(rows)
				m.objectTable.SetRows(seqRows(rows))
				m.updateObjectColumns()
				return m, nil
			}

		case "r":
			// Refresh current view
			if m.state == stateBucketList {
				cmds = append(cmds, m.loadBuckets())
			} else if m.state == stateObjectList {
				cmds = append(cmds, m.loadObjects())
			}

		case "p":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					m.showPreview = true
					m.previewKey = key
					cmds = append(cmds, m.fetchObjectPreview(key))
				}
			}

		case "y":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					uri := fmt.Sprintf("s3://%s/%s", m.bucket, key)
					arn := fmt.Sprintf("arn:aws:s3:::%s/%s", m.bucket, key)
					m.copyContent = uri + "\n" + arn
					// Ignore clipboard error silently
					_ = clipboard.WriteAll(uri)
					m.copyMenuActive = true
					return m, nil
				}
			}

		case "g":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					cmds = append(cmds, m.generatePresignCmd(key))
				}
			}

		case "D":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					cmds = append(cmds, m.downloadObjectCmd(key))
					m.statusMsg = fmt.Sprintf("Downloading: %s ...", key)
				}
			}

		case "x":
			if m.state == stateObjectList && m.focus == focusObjects && m.allowDelete {
				if key, ok := m.selectedObjectKey(); ok {
					m.deleteKey = key
					m.confirmingDelete = true
					m.deleteConfirm.SetValue("")
					m.deleteConfirm.Focus()
					return m, nil
				}
			}

		case "enter":
			if m.state == stateBucketList {
				row := m.bucketTable.SelectedRow()
				if len(row) > 0 {
					m.bucket = row[1]
					m.region = row[2]
					// Region may still be loading; fall back to the cache.
					if m.region == "..." {
						if cached, ok := m.bucketRegionCache[m.bucket]; ok {
							m.region = cached
						}
					}

					// Re-initialize client for the correct bucket region
					newClient, err := NewS3Client(m.client.ctx, m.awsCfg, m.region, m.endpointURL)
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
					name := row[1]
					class := row[4]
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

	// --- Message handlers ---

	case regionsDiscoveredMsg:
		m.loading = false
		m.scanning = true
		m.scanTotal = len(msg.regions)
		m.scanDone = 0
		m.statusMsg = fmt.Sprintf("Scanning %d regions...", m.scanTotal)
		awsCfg := m.awsCfg
		endpointURL := m.endpointURL
		ctx := m.client.ctx
		scanCmds := make([]tea.Cmd, 0, len(msg.regions))
		for _, region := range msg.regions {
			r := region
			scanCmds = append(scanCmds, func() tea.Msg {
				buckets, err := ListBucketsInRegion(ctx, awsCfg, r, endpointURL)
				return regionScannedMsg{region: r, buckets: buckets, err: err}
			})
		}
		cmds = append(cmds, tea.Batch(scanCmds...))

	case regionScannedMsg:
		m.scanDone++
		if msg.err == nil && len(msg.buckets) > 0 {
			firstBucket := m.bucket == ""
			for _, b := range msg.buckets {
				name := aws.ToString(b.Name)
				if m.seenBuckets[name] {
					continue
				}
				m.seenBuckets[name] = true
				dateStr := ""
				if b.CreationDate != nil {
					dateStr = b.CreationDate.Format("2006-01-02 15:04:05")
				}
				m.allBucketRows = append(m.allBucketRows, table.Row{"", name, "...", dateStr})
			}
			m.bucketTable.SetRows(seqRows(m.allBucketRows))
			if firstBucket && m.bucket == "" && len(m.allBucketRows) > 0 {
				m.bucket = m.allBucketRows[0][1]
				cmds = append(cmds, m.fetchBucketDetails(m.bucket))
			}
		} else if msg.err != nil {
			// Non-access-denied error: surface it in status rather than crashing.
			m.statusMsg = fmt.Sprintf("Error scanning %s: %s", msg.region, summarizeS3Error(msg.err))
		}
		if m.scanDone == m.scanTotal {
			m.scanning = false
			count := len(m.allBucketRows)
			if count == 0 {
				m.statusMsg = fmt.Sprintf("Scanned %d regions — no accessible buckets found. Check IAM permissions.", m.scanTotal)
			} else {
				m.statusMsg = fmt.Sprintf("Scanned %d regions — %d bucket(s) found", m.scanTotal, count)
				cmds = append(cmds, m.fetchBucketRegions())
			}
		} else {
			m.statusMsg = fmt.Sprintf("Scanning regions... %d / %d  (%d bucket(s) found so far)", m.scanDone, m.scanTotal, len(m.allBucketRows))
		}

	case bucketsLoadedMsg:
		m.loading = false
		m.err = nil
		m.bucketTable.SetRows(seqRows(msg.rows))
		m.allBucketRows = msg.rows // keep a copy for search restore
		if msg.warning != "" {
			m.statusMsg = msg.warning
		}
		if len(msg.rows) > 0 {
			m.bucket = msg.rows[0][1]
			cmds = append(cmds, m.fetchBucketDetails(msg.rows[0][1]))
		}
		cmds = append(cmds, m.fetchBucketRegions())

	case bucketRegionMsg:
		m.bucketRegionCache[msg.name] = msg.region
		rows := m.bucketTable.Rows()
		if msg.idx < len(rows) {
			rows[msg.idx][2] = msg.region
			m.bucketTable.SetRows(seqRows(rows))
		}
		// Also update allBucketRows
		if msg.idx < len(m.allBucketRows) {
			m.allBucketRows[msg.idx][2] = msg.region
		}

	case objectsLoadedMsg:
		m.loading = false
		m.err = nil
		m.objCount = msg.count
		m.totalSize = msg.size
		m.objectTable.SetRows(seqRows(msg.rows))

		m.lastSelectedKey = ""
		m.selectedDetails = nil

		if len(msg.rows) > 0 && msg.rows[0][4] != "DIR" {
			m.lastSelectedKey = m.prefix + msg.rows[0][1]
			cmds = append(cmds, m.fetchObjectDetails(m.lastSelectedKey))
		}

	case objectDetailsMsg:
		m.detailsLoading = false
		if msg.key == m.lastSelectedKey {
			if msg.err == nil {
				m.selectedDetails = msg.details
			}
		}

	case bucketDetailsMsg:
		if msg.bucket == m.bucket || msg.bucket == m.detailBucket {
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
			// Initialise the scrollable viewport for this preview.
			panelW := min(100, max(40, m.width-12))
			panelH := min(28, max(10, m.height-10))
			vpW := panelW - 6 // subtract border + padding
			vpH := panelH - 8 // subtract title, blank lines, help text, border
			if vpW < 10 {
				vpW = 10
			}
			if vpH < 2 {
				vpH = 2
			}
			m.previewViewport = viewport.New(vpW, vpH)
			if msg.err == nil && msg.content != "" {
				m.previewViewport.SetContent(msg.content)
			}
		}

	case presignedURLMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Presign error: %s", summarizeS3Error(msg.err))
		} else {
			m.presignedURL = msg.url
			m.showPresigned = true
		}

	case downloadDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Download error: %s", summarizeS3Error(msg.err))
		} else {
			m.statusMsg = fmt.Sprintf("Downloaded: %s", msg.path)
		}

	case deleteObjectDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Delete error: %s", summarizeS3Error(msg.err))
		} else {
			m.statusMsg = fmt.Sprintf("Deleted: %s", msg.key)
			cmds = append(cmds, m.loadObjects())
		}

	case errMsg:
		m.loading = false
		m.detailsLoading = false
		m.previewLoading = false
		m.err = msg.err
	}

	// Route table updates
	if m.focus == focusPrefixInput {
		m.prefixInput, cmd = m.prefixInput.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.inBucketSearch {
		m.bucketSearch, cmd = m.bucketSearch.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.state == stateBucketList || m.state == stateBucketDetail {
		m.bucketTable, cmd = m.bucketTable.Update(msg)
		cmds = append(cmds, cmd)

		if m.state == stateBucketList {
			row := m.bucketTable.SelectedRow()
			if len(row) > 0 && (m.selectedBucketDetails == nil || m.bucket != row[1]) {
				m.bucket = row[1]
				cmds = append(cmds, m.fetchBucketDetails(row[1]))
			}
		}
	} else if m.state == stateObjectList {
		prevRow := m.objectTable.Cursor()
		m.objectTable, cmd = m.objectTable.Update(msg)
		cmds = append(cmds, cmd)

		if m.focus == focusObjects && prevRow != m.objectTable.Cursor() && len(m.objectTable.SelectedRow()) > 0 {
			row := m.objectTable.SelectedRow()
			if row[4] != "DIR" {
				newKey := m.prefix + row[1]
				if newKey != m.lastSelectedKey {
					m.lastSelectedKey = newKey
					m.selectedDetails = nil
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

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	// Bucket detail full-screen view
	if m.state == stateBucketDetail {
		return ui.AppStyle().Render(m.bucketDetailView())
	}

	var content string

	headerText := "S3 TUI v1.3.0"
	if info := authDisplayInfo(m.awsCfg); info != "" {
		headerText += "   " + info
	}
	if m.region != "" {
		headerText += fmt.Sprintf("   Region: %s", m.region)
	} else if m.state == stateBucketList {
		headerText += "   Regions: all"
	}
	if m.flatMode {
		headerText += "   [FLAT]"
	}
	if m.showVersions {
		headerText += "   [VERSIONS:ON]"
	}
	header := ui.HeaderStyle().Render(headerText)

	if m.err != nil {
		maxErrW := m.width - 20
		if maxErrW < 40 {
			maxErrW = 40
		}
		errBox := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ui.FeatherColor(0))).
			Foreground(lipgloss.Color(ui.FeatherColor(0))).
			Padding(1, 2).
			Width(maxErrW).
			Align(lipgloss.Center).
			Render(fmt.Sprintf("Failed to access bucket: %s\n\n%s\n\nPress [Esc] to return to the bucket list.", m.bucket, ui.ErrorStyle().Render(m.err.Error())))

		content = lipgloss.Place(m.width-4, m.height-10, lipgloss.Center, lipgloss.Center, errBox)
	} else if m.loading {
		message := "Discovering AWS regions..."
		detail := "Fetching the list of available regions before scanning for buckets."
		if m.state == stateObjectList {
			message = "Loading S3 objects..."
			detail = fmt.Sprintf("Bucket: %s   Prefix: %s", m.bucket, displayPrefix(m.prefix))
		}
		content = lipgloss.Place(m.width-4, m.height-10, lipgloss.Center, lipgloss.Center, m.loadingBox(message, detail))
	} else {
		if m.state == stateBucketList {
			content = m.bucketListView()
		} else {
			content = m.objectListView()
		}
	}

	// Overlays
	if m.showSettings {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.settings.View())
	} else if m.showHelp {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.helpView())
	} else if m.showPreview {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.previewView())
	} else if m.copyMenuActive {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.copyMenuView())
	} else if m.showPresigned {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.presignedURLView())
	} else if m.confirmingDelete {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.deleteConfirmView())
	}

	// Bucket search overlay (drawn on top of bucket list)
	if m.inBucketSearch {
		searchBox := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ui.FeatherColor(0))).
			Foreground(lipgloss.Color(ui.FeatherColor(0))).
			Padding(0, 1).
			Render(lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Search buckets:"),
				m.bucketSearch.View(),
				ui.MutedStyle().Render("[Enter] Select first  [Esc] Cancel"),
			))
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Top, searchBox)
	}

	return ui.AppStyle().Render(lipgloss.JoinVertical(lipgloss.Left,
		header,
		ui.FeatherRail(max(12, m.width-4)),
		"",
		content,
		"",
		m.renderStatusBar(),
	))
}

// ---------------------------------------------------------------------------
// Sub-views
// ---------------------------------------------------------------------------

func (m *Model) renderStatusBar() string {
	barWidth := max(12, m.width-4)

	var left string
	var hints string

	switch m.state {
	case stateBucketDetail:
		tabNames := []string{"Overview", "Access & Security", "Data Protection", "Operational", "Tags"}
		left = fmt.Sprintf("Bucket: %s  |  %s", m.detailBucket, tabNames[m.detailTabIdx])
		hints = "Tab  Shift+Tab  Esc"
	case stateBucketList:
		switch {
		case m.statusMsg != "":
			left = m.statusMsg
		case m.scanning:
			left = fmt.Sprintf("Scanning: %d / %d  |  Buckets: %d", m.scanDone, m.scanTotal, len(m.allBucketRows))
		default:
			left = fmt.Sprintf("Buckets: %d", len(m.allBucketRows))
		}
		hints = "↑/↓ Enter  d  /  r  S  ?  q"
	case stateObjectList:
		if m.statusMsg != "" {
			left = m.statusMsg
		} else {
			left = fmt.Sprintf("Bucket: %s  |  Objects: %d  |  Size: %s", m.bucket, m.objCount, formatSize(m.totalSize))
			if m.prefix != "" {
				left += fmt.Sprintf("  |  Prefix: %s", m.prefix)
			}
			if m.flatMode {
				left += "  [FLAT]"
			}
			if m.showVersions {
				left += "  [VERSIONS]"
			}
		}
		hints = "↑/↓ Enter  /  p  y  g  D  f  s  r  S  Esc  ?  q"
		if m.allowDelete {
			hints = "↑/↓ Enter  /  p  y  g  D  x Delete  f  s  r  S  Esc  ?  q"
		}
	default:
		left = m.statusMsg
		hints = "↑/↓  q"
	}

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(hints)
	inner := barWidth - 2
	gap := inner - lw - rw
	if gap < 2 {
		gap = 2
	}

	content := left + strings.Repeat(" ", gap) + hints
	return ui.StatusBarStyle(barWidth).Render(content)
}

func (m *Model) bucketListView() string {
	tableSection := ui.SelectedPanelStyle().Render(m.bucketTable.View())

	const detailsHeight = 10
	detailsWidth := max(20, m.width-4)

	title := "BUCKET DETAILS"
	metaText := ui.MutedStyle().Render("Select a bucket to view details.")
	if len(m.bucketTable.SelectedRow()) > 0 {
		row := m.bucketTable.SelectedRow()
		name := row[1]
		region := row[2]
		date := row[3]
		title = fmt.Sprintf("BUCKET DETAILS: %s  [d] Full detail view", name)

		metaText = m.loadingLine("Loading bucket details...")
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
					fmt.Sprintf("Region:      %s", region),
					fmt.Sprintf("Created:     %s", date),
					fmt.Sprintf("Versioning:  %s", m.selectedBucketDetails.Versioning),
					fmt.Sprintf("Encryption:  %s", m.selectedBucketDetails.Encryption),
					fmt.Sprintf("Replication: %s", m.selectedBucketDetails.Replication),
					fmt.Sprintf("Logging:     %s", m.selectedBucketDetails.Logging),
				),
				"    ",
				lipgloss.JoinVertical(lipgloss.Left,
					fmt.Sprintf("Policy:      %s", m.selectedBucketDetails.Policy),
					fmt.Sprintf("Lifecycle:   %d rules", m.selectedBucketDetails.LifecycleRules),
					fmt.Sprintf("PAB:         %s", m.selectedBucketDetails.PublicAccessBlock),
					fmt.Sprintf("CORS:        %s", m.selectedBucketDetails.CORS),
					fmt.Sprintf("Website:     %s", m.selectedBucketDetails.Website),
					fmt.Sprintf("Tags:        %s", tagStr),
				),
				"    ",
				lipgloss.JoinVertical(lipgloss.Left,
					fmt.Sprintf("Acceleration: %s", m.selectedBucketDetails.Acceleration),
					fmt.Sprintf("ObjectLock:   %s", m.selectedBucketDetails.ObjectLock),
					fmt.Sprintf("ACL:          %s", m.selectedBucketDetails.ACLSummary),
					fmt.Sprintf("Ownership:    %s", m.selectedBucketDetails.OwnershipControls),
				),
			)
		}
	}

	detailsPanel := ui.FixedPanelStyle(detailsWidth, detailsHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			ui.PanelTitleStyle().Render(title),
			"",
			metaText,
		))

	return lipgloss.JoinVertical(lipgloss.Left,
		tableSection,
		detailsPanel,
	)
}

func (m *Model) objectListView() string {
	sizeStr := formatSize(m.totalSize)

	headerRight := ui.MutedStyle().Render(
		fmt.Sprintf("Objects: %d   Size: %s", m.objCount, sizeStr))

	bucketHeader := lipgloss.JoinHorizontal(lipgloss.Top,
		ui.BadgeStyle().Render(fmt.Sprintf("Bucket: %s", m.bucket)),
		"   ",
		headerRight,
	)

	prefixSection := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(1))).Bold(true).Render("Prefix: "),
		m.prefixInput.View(),
	)

	tableStyle := ui.PanelStyle()
	if m.focus == focusObjects {
		tableStyle = ui.SelectedPanelStyle()
	}
	tableSection := tableStyle.Render(m.objectTable.View())

	// Details Panel — always render two fixed-size boxes so nothing below shifts.
	const detailsHeight = 10
	boxWidth := max(20, m.width/2-4)

	detailsContent := ui.MutedStyle().Render("Select an object to view details.")
	metaText := ""
	if len(m.objectTable.SelectedRow()) > 0 {
		row := m.objectTable.SelectedRow()
		name, size, date, class, etag := row[1], row[2], row[3], row[4], row[5]

		isDir := (class == "DIR")

		detailsContent = fmt.Sprintf("Key: %s%s\nSize: %s\nLast Modified: %s\nStorage Class: %s\nETag: %s",
			m.prefix, name, size, date, class, etag)

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

				encoding := m.selectedDetails.ContentEncoding
				if encoding == "" {
					encoding = "—"
				}
				disposition := m.selectedDetails.ContentDisposition
				if disposition == "" {
					disposition = "—"
				}
				cacheCtrl := m.selectedDetails.CacheControl
				if cacheCtrl == "" {
					cacheCtrl = "—"
				}
				kmsKey := m.selectedDetails.KMSKeyID
				if kmsKey == "" {
					kmsKey = "—"
				} else if len(kmsKey) > 20 {
					kmsKey = kmsKey[:20] + "..."
				}
				sc := m.selectedDetails.StorageClass
				if sc == "" {
					sc = "STANDARD"
				}
				restore := m.selectedDetails.RestoreStatus
				if restore == "" {
					restore = "—"
				}
				aclStr := m.selectedDetails.ACLGrants
				if aclStr == "" {
					aclStr = "—"
				}
				ret := m.selectedDetails.Retention
				if ret == "" {
					ret = "—"
				}
				lh := m.selectedDetails.LegalHold
				if lh == "" {
					lh = "—"
				}

				metaText = lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.JoinVertical(lipgloss.Left,
						fmt.Sprintf("Content-Type:    %s", cType),
						fmt.Sprintf("SSE:             %s", m.selectedDetails.SSE),
						fmt.Sprintf("Version:         %s", m.selectedDetails.VersionID),
						fmt.Sprintf("Storage Class:   %s", sc),
						fmt.Sprintf("Encoding:        %s", encoding),
						fmt.Sprintf("Cache-Control:   %s", cacheCtrl),
					),
					"    ",
					lipgloss.JoinVertical(lipgloss.Left,
						fmt.Sprintf("Disposition:   %s", disposition),
						fmt.Sprintf("KMS Key:       %s", kmsKey),
						fmt.Sprintf("Restore:       %s", restore),
						fmt.Sprintf("ACL:           %s", aclStr),
						fmt.Sprintf("Retention:     %s", ret),
						fmt.Sprintf("Legal Hold:    %s", lh),
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
	}

	detailsBox := ui.FixedPanelStyle(boxWidth, detailsHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			ui.PanelTitleStyle().Render("OBJECT DETAILS"),
			"",
			detailsContent,
		))

	metadataBox := ui.FixedPanelStyle(boxWidth, detailsHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			ui.PanelTitleStyle().Render("TAGS & METADATA"),
			"",
			metaText,
		))

	detailsPanel := lipgloss.JoinHorizontal(lipgloss.Top, detailsBox, "  ", metadataBox)

	return lipgloss.JoinVertical(lipgloss.Left,
		bucketHeader,
		"",
		prefixSection,
		"",
		tableSection,
		"",
		detailsPanel,
	)
}

// bucketDetailView renders the full-screen bucket detail view.
func (m *Model) bucketDetailView() string {
	bucket := m.detailBucket

	// Tab bar
	tabNames := []string{"Overview", "Access & Security", "Data Protection", "Operational", "Tags"}
	var tabs []string
	for i, name := range tabNames {
		if i == m.detailTabIdx {
			tabs = append(tabs, ui.BoldStyle().Underline(true).Render(fmt.Sprintf("[ %s ]", name)))
		} else {
			tabs = append(tabs, ui.MutedStyle().Render(fmt.Sprintf("  %s  ", name)))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	title := ui.PanelTitleStyle().Render(fmt.Sprintf("BUCKET DETAIL: %s", bucket))

	var body string
	if m.detailsLoading || m.selectedBucketDetails == nil {
		body = m.loadingLine("Loading bucket details...")
	} else {
		d := m.selectedBucketDetails
		orDash := func(s string) string {
			if s == "" {
				return "—"
			}
			return s
		}
		switch m.detailTabIdx {
		case 0: // Overview
			body = lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Name:        ")+orDash(bucket),
				ui.BoldStyle().Render("ARN:         ")+"arn:aws:s3:::"+bucket,
				ui.BoldStyle().Render("Region:      ")+orDash(m.region),
				ui.BoldStyle().Render("Versioning:  ")+orDash(d.Versioning),
				ui.BoldStyle().Render("Encryption:  ")+orDash(d.Encryption),
				ui.BoldStyle().Render("Lifecycle:   ")+fmt.Sprintf("%d rules", d.LifecycleRules),
			)
		case 1: // Access & Security
			policyTrunc := d.Policy
			if len(policyTrunc) > 80 {
				policyTrunc = policyTrunc[:80] + "..."
			}
			body = lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Public Access Block: ")+orDash(d.PublicAccessBlock),
				ui.BoldStyle().Render("ACL:                 ")+orDash(d.ACLSummary),
				ui.BoldStyle().Render("Ownership Controls:  ")+orDash(d.OwnershipControls),
				ui.BoldStyle().Render("Policy:              ")+orDash(policyTrunc),
				ui.BoldStyle().Render("Policy Status:       ")+orDash(d.PolicyStatus),
			)
		case 2: // Data Protection
			body = lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Versioning:   ")+orDash(d.Versioning),
				ui.BoldStyle().Render("Encryption:   ")+orDash(d.Encryption),
				ui.BoldStyle().Render("Object Lock:  ")+orDash(d.ObjectLock),
				ui.BoldStyle().Render("Replication:  ")+orDash(d.Replication),
			)
		case 3: // Operational
			body = lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Logging:              ")+orDash(d.Logging),
				ui.BoldStyle().Render("CORS:                 ")+orDash(d.CORS),
				ui.BoldStyle().Render("Website:              ")+orDash(d.Website),
				ui.BoldStyle().Render("Notifications:        ")+orDash(d.Notifications),
				ui.BoldStyle().Render("Request Payment:      ")+orDash(d.RequestPayment),
				ui.BoldStyle().Render("Transfer Accel.:      ")+orDash(d.Acceleration),
				ui.BoldStyle().Render("Intelligent Tiering:  ")+orDash(d.IntelligentTiering),
				ui.BoldStyle().Render("Multipart Uploads:    ")+fmt.Sprintf("%d in-progress", d.MultipartUploads),
			)
		case 4: // Tags
			if len(d.Tags) == 0 {
				body = "None"
			} else {
				var lines []string
				for k, v := range d.Tags {
					lines = append(lines, fmt.Sprintf("  %s = %s", k, v))
				}
				sort.Strings(lines)
				body = strings.Join(lines, "\n")
			}
		}
	}

	width := max(60, m.width-8)
	height := max(20, m.height-10)

	return lipgloss.JoinVertical(lipgloss.Left,
		ui.HeaderStyle().Render("S3 TUI"),
		ui.FeatherRail(max(12, m.width-4)),
		"",
		lipgloss.NewStyle().
			Width(width).
			Height(height).
			MaxWidth(width+2).
			MaxHeight(height+2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ui.FeatherColor(1))).
			Foreground(lipgloss.Color(ui.FeatherColor(0))).
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Left,
				title,
				"",
				tabBar,
				strings.Repeat("─", min(width-6, 60)),
				"",
				body,
			)),
		"",
		m.renderStatusBar(),
	)
}

func (m *Model) copyMenuView() string {
	width := min(80, max(40, m.width-12))
	content := lipgloss.JoinVertical(lipgloss.Left,
		ui.BoldStyle().Render("Copied to clipboard!"),
		"",
		m.copyContent,
		"",
		ui.MutedStyle().Render("[y] Copy URI  [Esc] Close"),
	)
	return ui.ModalStyle(width, 8).Render(content)
}

func (m *Model) presignedURLView() string {
	width := min(100, max(40, m.width-12))
	content := lipgloss.JoinVertical(lipgloss.Left,
		ui.BoldStyle().Render("PRESIGNED URL (1 hour)"),
		"",
		m.presignedURL,
		"",
		ui.MutedStyle().Render("[Esc] Close"),
	)
	return ui.ModalStyle(width, 8).Render(content)
}

func (m *Model) deleteConfirmView() string {
	width := min(70, max(40, m.width-12))
	content := lipgloss.JoinVertical(lipgloss.Left,
		ui.BoldStyle().Render(fmt.Sprintf("DELETE OBJECT: %s", m.deleteKey)),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.FeatherColor(0))).Render("This action is PERMANENT and cannot be undone."),
		"",
		m.deleteConfirm.View(),
		"",
		ui.MutedStyle().Render("Type 'delete' and press Enter to confirm. Esc to cancel."),
	)
	return ui.ModalStyle(width, 10).Render(content)
}

func (m *Model) helpView() string {
	deleteSection := ""
	if m.allowDelete {
		deleteSection = "\n  x                  Delete selected object (requires confirmation)"
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		"Navigation",
		"  ↑/↓, PgUp/PgDn     Move selection",
		"  Enter              Open bucket, prefix, or object preview",
		"  Esc                Back, close preview/help, or clear prefix input",
		"",
		"Buckets",
		"  /                  Search/filter buckets",
		"  d                  Full bucket detail view",
		"  r                  Refresh bucket list",
		"",
		"Objects",
		"  /                  Jump to prefix",
		"  p                  Preview selected object",
		"  y                  Copy S3 URI to clipboard",
		"  g                  Generate presigned URL (1 hour)",
		"  D                  Download object to current directory",
		"  f                  Toggle flat mode (show all objects)",
		"  v                  Toggle versions indicator",
		"  s                  Cycle sort column",
		"  R                  Reverse sort direction",
		"  r                  Refresh object list"+deleteSection,
		"",
		"Utility",
		"  S                  Settings (theme & colors)",
		"  ?                  Toggle this help",
		"  q, Ctrl+C          Quit",
	)
	return ui.HelpView("S3 Explorer Help", body, min(72, max(32, m.width-12)))
}

func (m *Model) previewView() string {
	var body string
	if m.previewErr != nil {
		body = "Preview failed: " + summarizeS3Error(m.previewErr)
	} else if m.previewLoading {
		body = m.loadingLine("Loading preview...")
	} else if m.previewContent == "" {
		body = "Object is empty."
	} else {
		body = m.previewViewport.View()
	}

	width := min(100, max(40, m.width-12))
	height := min(28, max(10, m.height-10))
	title := ui.PanelTitleStyle().Render("OBJECT PREVIEW: " + m.previewKey)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width+2).
		MaxHeight(height+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.FeatherColor(0))).
		Foreground(lipgloss.Color(ui.FeatherColor(0))).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", ui.MutedStyle().Render("[↑/↓/PgUp/PgDn] Scroll  [Esc] Close")))
}
