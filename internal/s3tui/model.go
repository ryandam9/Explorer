package s3tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/csvexport"
	"github.com/ryandam9/aws_explorer/internal/debugpane"
	"github.com/ryandam9/aws_explorer/internal/display"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
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

// bucketsScannedMsg carries the result of the single, global s3:ListBuckets
// call. The listing reports every bucket the account owns, each with its
// region, so no per-region scan is needed.
type bucketsScannedMsg struct {
	buckets []s3types.Bucket
	err     error
}

type objectsLoadedMsg struct {
	maps  []map[string]string
	count int
	size  int64
	// nextToken is non-nil when the bucket has more keys than the page
	// window fetched; appended marks a "load more" continuation batch.
	nextToken *string
	appended  bool
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

// archiveLoadedMsg carries a fetched-and-listed tar archive (data is the raw,
// already-decompressed tar bytes used to extract members on demand).
type archiveLoadedMsg struct {
	key       string
	data      []byte
	members   []archiveMember
	truncated bool
	err       error
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
	dir  string
	err  error
}

// downloadTickMsg drives the progress bar refresh while a download is running.
type downloadTickMsg struct{}

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

	sortCol    int
	sortAsc    bool
	objectMaps []map[string]string

	bucketTable table.Model
	objectTable table.Model
	prefixInput textinput.Model
	spinner     spinner.Model

	// Stats
	objCount  int
	totalSize int64

	lastSelectedKey       string
	selectedDetails       *ObjectDetails
	objectDetailsCache    map[string]*ObjectDetails // on-demand object metadata, by key
	selectedBucketDetails *BucketDetails
	// objectsNextToken is set when the current listing was cut off by the
	// page window; "L" continues from it.
	objectsNextToken *string
	detailsLoading   bool
	showHelp         bool
	showAbout        bool
	showPreview      bool
	previewKey       string
	previewContent   string
	previewLoading   bool
	previewErr       error

	// Full-screen CSV table view (a CSV/TSV object previewed with "p").
	showCSV      bool
	csvTable     table.Model
	csvDelim     rune
	csvAll       [][]string // parsed header + data rows
	csvRowCap    int        // first-N/last-N window (0 = all)
	csvRowCapSet bool
	csvTotal     int // total data rows parsed
	csvHidden    int // rows omitted by the window

	csvDelimInput   textinput.Model // typed custom-delimiter prompt ("S")
	csvDelimEditing bool
	csvDelimErr     string

	// Full-screen archive browser (a .tar/.tar.gz/.tgz object). Selecting a
	// member opens its content in the CSV or text view.
	showArchive        bool
	archiveTable       table.Model
	archiveKey         string
	archiveData        []byte // decompressed tar bytes, for extracting members
	archiveMembers     []archiveMember
	archiveTruncated   bool
	archiveLoading     bool
	archiveErr         error
	previewFromArchive bool // the current preview/CSV came from an archive member

	bucketRegionCache map[string]string
	// bucketDetailsCache holds fetched bucket details for the session: each
	// fetch costs ~19 API calls and is triggered by every selection change in
	// the bucket list, so revisiting a bucket must not refetch. The refresh
	// keys invalidate it.
	bucketDetailsCache map[string]*BucketDetails
	themeIdx           int

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
	allowDelete         bool
	confirmingDelete    bool
	deleteConfirmErrMsg string // set when user presses Enter with wrong text
	deleteConfirm       textinput.Model
	deleteKey           string

	copyMenuActive bool
	copyContent    string

	// Object download state. downloadDir is resolved once from config; the
	// progress bar is driven by a ticking poll of downloadState while a
	// download is in flight.
	downloadDir      string
	downloading      bool
	downloadKey      string
	downloadPercent  float64
	downloadProgress progress.Model
	downloadState    *DownloadProgress

	presignedURL  string
	showPresigned bool

	previewViewport viewport.Model

	statusMsg string // transient status shown in footer

	// Shared settings panel (theme & colors). Opened with KeySettings.
	showSettings bool
	settings     ui.SettingsModel
	configPath   string
	cfg          *config.Config

	debug debugpane.Model // "~" live activity overlay
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
		client:             client,
		awsCfg:             awsCfg,
		region:             region,
		bucket:             bucket,
		prefix:             prefix,
		endpointURL:        endpointURL,
		sortAsc:            true,
		bucketRegionCache:  make(map[string]string),
		bucketDetailsCache: make(map[string]*BucketDetails),
		objectDetailsCache: make(map[string]*ObjectDetails),
		themeIdx:           themeIdx,
		allowDelete:        allowDelete,
		configPath:         configPath,
		cfg:                cfg,
	}
	m.settings = ui.NewSettingsModel(0, 0, configPath, cfg)

	m.initBucketTable()
	m.initObjectTable()

	m.prefixInput = textinput.New()
	m.prefixInput.Placeholder = "Enter prefix (e.g. photos/2024/)"
	m.prefixInput.CharLimit = 256
	m.prefixInput.Width = 50
	m.prefixInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.prefixInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.prefixInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.prefixInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.bucketSearch = textinput.New()
	m.bucketSearch.Placeholder = "Filter buckets…"
	m.bucketSearch.CharLimit = 128
	m.bucketSearch.Width = 40
	m.bucketSearch.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	m.bucketSearch.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.bucketSearch.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	m.bucketSearch.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.deleteConfirm = textinput.New()
	m.deleteConfirm.Placeholder = "Type 'delete' to confirm"
	m.deleteConfirm.CharLimit = 32
	m.deleteConfirm.Width = 30
	m.deleteConfirm.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true)
	m.deleteConfirm.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	m.deleteConfirm.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)),
	)

	m.downloadDir = resolveDownloadDir(cfg)
	m.downloadProgress = progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(),
	)
	m.downloadProgress.Width = 30

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

// objectColFields returns the resolved column fields for the object table.
func (m *Model) objectColFields() []display.FieldMeta {
	var cfgCols []string
	if m.cfg != nil {
		cfgCols = m.cfg.Display.S3.Objects.Columns
	}
	return display.ResolveColumns(display.S3ObjectFields, cfgCols)
}

// bucketColFields returns the resolved column fields for the bucket table.
func (m *Model) bucketColFields() []display.FieldMeta {
	var cfgCols []string
	if m.cfg != nil {
		cfgCols = m.cfg.Display.S3.Buckets.Columns
	}
	return display.ResolveColumns(display.S3BucketFields, cfgCols)
}

func (m *Model) applyTableStyle(t *table.Model) {
	t.SetStyles(ui.TableStyles())
}

// tableViewWidth is the inner width available to a full-width table: terminal
// width minus the AppStyle margins (2×2) and the panel border + padding (2+2).
func (m *Model) tableViewWidth() int {
	return max(30, m.width-8)
}

// activeTable returns the table the current state navigates, or nil when no
// table has focus (e.g. while typing into an input).
func (m *Model) activeTable() *table.Model {
	switch {
	case m.state == stateBucketList && m.focus == focusBuckets:
		return &m.bucketTable
	case m.state == stateObjectList && m.focus == focusObjects:
		return &m.objectTable
	}
	return nil
}

func (m *Model) initBucketTable() {
	cols := display.Columns(m.bucketColFields())
	m.bucketTable = table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(15))
	m.applyTableStyle(&m.bucketTable)
}

func (m *Model) initObjectTable() {
	cols := m.buildObjectColumns()
	m.objectTable = table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(10))
	m.applyTableStyle(&m.objectTable)
}

// buildObjectColumns produces columns with sort indicators on the active field.
func (m *Model) buildObjectColumns() []table.Column {
	fields := m.objectColFields()
	cols := make([]table.Column, 0, len(fields)+1)
	cols = append(cols, table.Column{Title: "#", Width: 4})
	for _, f := range fields {
		cols = append(cols, table.Column{Title: f.Title, Width: f.Width})
	}
	applyObjectSortHeader(cols, m.sortCol, m.sortAsc)
	return cols
}

// applyObjectSortHeader marks the active sort column (leading "#" at index 0 is
// not sortable, so the field index maps to display index field+1) and reserves
// the arrow's width so the table never reflows when the sort moves.
func applyObjectSortHeader(cols []table.Column, sortCol int, asc bool) {
	active := -1
	if sortCol >= 0 {
		active = sortCol + 1
	}
	table.ApplySortHeader(cols, active, asc, func(i int) bool { return i > 0 })
}

func (m *Model) restyleForTheme() {
	m.applyTableStyle(&m.bucketTable)
	m.applyTableStyle(&m.objectTable)
	for _, in := range []*textinput.Model{&m.prefixInput, &m.bucketSearch, &m.deleteConfirm} {
		in.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
		in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
		in.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
		in.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	}
}

// ---------------------------------------------------------------------------
// Sort
// ---------------------------------------------------------------------------

func (m *Model) sortObjects(objs []map[string]string) {
	if len(objs) <= 1 {
		return
	}
	fields := m.objectColFields()
	col := m.sortCol
	if col >= len(fields) {
		col = 0
	}
	fieldKey := fields[col].Key

	sort.SliceStable(objs, func(i, j int) bool {
		// Directories always sort before files regardless of direction.
		di, dj := objs[i]["type"] == "DIR", objs[j]["type"] == "DIR"
		if di != dj {
			return di
		}
		if di && dj {
			return strings.ToLower(objs[i]["name"]) < strings.ToLower(objs[j]["name"])
		}
		if fieldKey == "size" {
			li := parseSize(objs[i]["size"])
			lj := parseSize(objs[j]["size"])
			if m.sortAsc {
				return li < lj
			}
			return li > lj
		}
		li := objs[i][fieldKey]
		lj := objs[j][fieldKey]
		if fieldKey == "name" {
			li = strings.ToLower(li)
			lj = strings.ToLower(lj)
		}
		if m.sortAsc {
			return li < lj
		}
		return li > lj
	})
}

func (m *Model) updateObjectColumns() {
	fields := m.objectColFields()
	// Distribute any extra terminal width to the name column. Each column
	// also occupies 2 cells of padding, which must be subtracted so the
	// stretched table exactly fits the panel instead of overflowing into a
	// permanent one-column horizontal scroll.
	padding := 2 * (len(fields) + 1)
	fixedW := 4 // # col
	for _, f := range fields {
		if f.Key != "name" {
			fixedW += f.Width
		}
	}
	nameWidth := max(18, m.tableViewWidth()-fixedW-padding)
	cols := make([]table.Column, 0, len(fields)+1)
	cols = append(cols, table.Column{Title: "#", Width: 4})
	for _, f := range fields {
		w := f.Width
		if f.Key == "name" {
			w = nameWidth
		}
		cols = append(cols, table.Column{Title: f.Title, Width: w})
	}
	applyObjectSortHeader(cols, m.sortCol, m.sortAsc)
	m.objectTable.SetColumns(cols)
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

// ensureBucketDetails makes bucket's details current, serving them from the
// session cache when possible. It returns nil when no fetch is needed (cache
// hit, or the same fetch is already in flight).
func (m *Model) ensureBucketDetails(bucket string) tea.Cmd {
	if d, ok := m.bucketDetailsCache[bucket]; ok {
		m.selectedBucketDetails = d
		m.detailsLoading = false
		return nil
	}
	if m.detailsLoading && m.bucket == bucket {
		return nil
	}
	m.selectedBucketDetails = nil
	return m.fetchBucketDetails(bucket)
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

// openPreview starts previewing an object. A tar archive opens the full-screen
// member browser; a plain .gz is decompressed and shown as a CSV table or text
// by its inner name; a CSV/TSV opens the table view; anything else opens the
// text overlay. Content is fetched asynchronously with a spinner.
func (m *Model) openPreview(key string) tea.Cmd {
	m.previewKey = key
	m.previewFromArchive = false
	m.showCSV = false
	m.showPreview = false
	m.showArchive = false

	switch {
	case looksLikeTar(key):
		m.showArchive = true
		m.archiveKey = key
		m.archiveLoading = true
		m.archiveErr = nil
		m.archiveData = nil
		m.archiveMembers = nil
		return m.fetchArchive(key)
	case looksLikeGzip(key):
		m.routePreviewView(innerName(key))
		m.previewLoading = true
		m.previewErr = nil
		return m.fetchGzipPreview(key)
	default:
		m.routePreviewView(key)
		return m.fetchObjectPreview(key)
	}
}

// routePreviewView selects the CSV table or the text overlay for a logical
// filename (the inner name for compressed objects).
func (m *Model) routePreviewView(name string) {
	if looksLikeCSV(name) {
		m.showCSV = true
		m.showPreview = false
	} else {
		m.showPreview = true
		m.showCSV = false
	}
}

// fetchGzipPreview downloads and decompresses a plain .gz object for preview.
func (m *Model) fetchGzipPreview(key string) tea.Cmd {
	m.previewLoading = true
	m.previewErr = nil
	m.previewContent = ""
	bucket := m.bucket
	client := m.client
	return func() tea.Msg {
		data, _, err := client.GetObjectRange(bucket, key, gzCompressedCap)
		if err != nil {
			return objectPreviewMsg{key: key, err: err}
		}
		out, truncated, err := gunzip(data, gzDecompressedCap)
		if err != nil {
			return objectPreviewMsg{key: key, err: err}
		}
		return objectPreviewMsg{key: key, content: decompressedPreview(out, truncated, looksLikeCSV(innerName(key)))}
	}
}

// fetchArchive downloads a tar archive (decompressing if gzipped) and lists its
// members.
func (m *Model) fetchArchive(key string) tea.Cmd {
	bucket := m.bucket
	client := m.client
	gzipped := isGzipCompressed(key)
	return func() tea.Msg {
		data, truncated, err := client.GetObjectRange(bucket, key, tarCompressedCap)
		if err != nil {
			return archiveLoadedMsg{key: key, err: err}
		}
		raw := data
		if gzipped {
			out, tr, gerr := gunzip(data, tarDecompressedCap)
			if gerr != nil {
				return archiveLoadedMsg{key: key, err: gerr}
			}
			raw = out
			truncated = truncated || tr
		}
		members, merr := tarMembers(raw)
		if merr != nil {
			return archiveLoadedMsg{key: key, err: merr}
		}
		return archiveLoadedMsg{key: key, data: raw, members: members, truncated: truncated}
	}
}

// decompressedPreview turns decompressed bytes into preview text, flagging
// binary content. A truncation note is appended only for non-CSV content — for
// a CSV it would corrupt the last row, and the table reports its own row window.
func decompressedPreview(out []byte, truncated, isCSV bool) string {
	for _, b := range out {
		if b == 0 {
			return "Binary content (decompressed). Download to inspect."
		}
	}
	text := string(out)
	if truncated && !isCSV {
		text += "\n\n… preview truncated …"
	}
	return text
}

// initPreviewViewport builds the scrollable text viewport for a (non-CSV)
// preview from the fetched content.
func (m *Model) initPreviewViewport(content string, err error) {
	panelW := min(100, max(40, m.width-12))
	panelH := min(28, max(10, m.height-10))
	vpW := panelW - 8 // border + padding + scrollbar gutter
	vpH := panelH - 8 // title, blank lines, help text, border
	if vpW < 10 {
		vpW = 10
	}
	if vpH < 2 {
		vpH = 2
	}
	m.previewViewport = viewport.New(vpW, vpH)
	if err == nil && content != "" {
		// Pretty-print XML so a minified single-line document is readable, then
		// wrap long lines so nothing is clipped off the right edge of the pane.
		display := content
		if looksLikeXMLContent(display) {
			if formatted, ok := formatXML(display); ok {
				display = formatted
			}
		}
		m.previewViewport.SetContent(hardWrap(display, vpW))
	}
}

func (m *Model) loadBuckets() tea.Cmd {
	// Reset for a clean run (handles refresh too).
	m.loading = true
	m.allBucketRows = nil
	m.bucketTable.SetRows(nil)
	m.statusMsg = ""

	// s3:ListBuckets is a global call that returns every bucket the account
	// owns, each tagged with its region — so a single request replaces the old
	// per-region scan. Access-denied is reported as an empty list (the IAM hint
	// is shown by the handler) rather than a hard error.
	client := m.client
	return func() tea.Msg {
		slog.Info("Listing S3 buckets")
		buckets, err := client.ListBuckets()
		if err != nil && hasAPIErrorCode(err, "AccessDenied", "AccessDeniedException",
			"UnauthorizedOperation", "AuthorizationError") {
			return bucketsScannedMsg{}
		}
		slog.Info("Listed S3 buckets", "count", len(buckets))
		return bucketsScannedMsg{buckets: buckets, err: err}
	}
}

// regionPending marks a bucket whose region is not yet known — shown in the
// Region column until the async per-bucket lookup fills it in. With modern S3
// listings the region arrives up front, so this is only seen for non-AWS
// S3-compatible endpoints that omit it.
const regionPending = "…"

// bucketRow turns a listing entry into the (name, region, created) triple shown
// in the bucket table. The region comes straight from the listing when S3
// provides it (s3:ListBuckets now reports BucketRegion), falling back to
// regionPending so the per-bucket lookup can fill it in later.
func bucketRow(b s3types.Bucket) (name, region, created string) {
	name = aws.ToString(b.Name)
	region = aws.ToString(b.BucketRegion)
	if region == "" {
		region = regionPending
	}
	if b.CreationDate != nil {
		created = b.CreationDate.Format("2006-01-02 15:04:05")
	}
	return name, region, created
}

func (m *Model) fetchBucketRegions() tea.Cmd {
	rows := m.bucketTable.Rows()
	if len(rows) == 0 {
		return nil
	}

	sem := make(chan struct{}, 20)

	// Apply any already-cached regions immediately so a reload doesn't
	// leave the region column stuck at "…" for buckets we've seen before.
	cacheApplied := false
	for i, row := range rows {
		if row[2] != regionPending {
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
		if row[2] != regionPending {
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
	m.objectsNextToken = nil
	flat := m.flatMode
	prefix := m.prefix
	bucket := m.bucket
	client := m.client
	return func() tea.Msg {
		slog.Info("Listing S3 objects", "bucket", bucket, "prefix", prefix, "flat", flat)
		return fetchObjectWindow(client, bucket, prefix, flat, nil, false)
	}
}

// loadMoreObjects continues a truncated listing from the saved token and
// appends the next window to the current view.
func (m *Model) loadMoreObjects() tea.Cmd {
	if m.objectsNextToken == nil {
		return nil
	}
	m.loading = true
	token := m.objectsNextToken
	flat := m.flatMode
	prefix := m.prefix
	bucket := m.bucket
	client := m.client
	return func() tea.Msg {
		return fetchObjectWindow(client, bucket, prefix, flat, token, true)
	}
}

// fetchObjectWindow lists one page window and converts it to row maps.
func fetchObjectWindow(client *S3Client, bucket, prefix string, flat bool, token *string, appended bool) tea.Msg {
	var res *ListObjectsResult
	var err error
	if flat {
		res, err = client.ListObjectsFlat(bucket, prefix, token)
	} else {
		res, err = client.ListObjects(bucket, prefix, token)
	}
	if err != nil {
		return errMsg{fmt.Errorf("access denied or region mismatch for bucket '%s': %w", bucket, err)}
	}
	// The ".." up-dir entry belongs only to the first window of a
	// hierarchical listing; continuation batches are appended after it.
	includeUp := prefix != "" && !flat && !appended
	maps, count, size := buildObjectMaps(res, prefix, flat, includeUp)
	return objectsLoadedMsg{maps: maps, count: count, size: size, nextToken: res.NextToken, appended: appended}
}

// buildObjectMaps converts a listing window into the row maps the object
// table renders, returning the file count and cumulative size alongside.
func buildObjectMaps(res *ListObjectsResult, prefix string, flat, includeUp bool) ([]map[string]string, int, int64) {
	var maps []map[string]string
	var count int
	var totalSize int64

	if includeUp {
		maps = append(maps, map[string]string{"name": "..", "type": "DIR", "size": "-", "last_modified": "-", "storage_class": "DIR", "etag": "-"})
	}

	if !flat {
		for _, p := range res.Prefixes {
			name := aws.ToString(p.Prefix)
			if prefix != "" && strings.HasPrefix(name, prefix) {
				name = strings.TrimPrefix(name, prefix)
			}
			maps = append(maps, map[string]string{"name": name, "type": "DIR", "size": "-", "last_modified": "-", "storage_class": "DIR", "etag": "-"})
		}
	}

	for _, o := range res.Objects {
		name := aws.ToString(o.Key)
		if prefix != "" && strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" {
			continue
		}
		count++
		sizeBytes := aws.ToInt64(o.Size)
		totalSize += sizeBytes
		date := ""
		if o.LastModified != nil {
			date = o.LastModified.Format("2006-01-02 15:04:05")
		}
		class := string(o.StorageClass)
		if class == "" {
			class = "STANDARD"
		}
		etag := strings.Trim(aws.ToString(o.ETag), "\"")
		maps = append(maps, map[string]string{
			"name":          name,
			"type":          "FILE",
			"size":          formatSize(sizeBytes),
			"last_modified": date,
			"storage_class": class,
			"etag":          etag,
		})
	}

	return maps, count, totalSize
}

// exportObjectsCSV writes the current object listing (current sort order,
// full values) to a timestamped CSV and returns its path.
func (m *Model) exportObjectsCSV() (string, error) {
	fields := m.objectColFields()
	header := make([]string, 0, len(fields))
	for _, f := range fields {
		header = append(header, f.Title)
	}
	rows := make([][]string, 0, len(m.objectMaps))
	for _, r := range m.objectMaps {
		if r["name"] == ".." {
			continue
		}
		row := make([]string, 0, len(fields))
		for _, f := range fields {
			row = append(row, r[f.Key])
		}
		rows = append(rows, row)
	}
	dir, err := csvexport.DefaultDir()
	if err != nil {
		return "", err
	}
	name := "s3-" + m.bucket
	if m.prefix != "" {
		name += "-" + strings.TrimSuffix(m.prefix, "/")
	}
	return csvexport.Write(dir, name, header, rows)
}

// buildObjectRows converts objectMaps to display rows using current column config.
func (m *Model) buildObjectRows() []table.Row {
	fields := m.objectColFields()
	rows := make([]table.Row, len(m.objectMaps))
	for i, r := range m.objectMaps {
		rows[i] = display.Row(fields, r)
	}
	return seqRows(rows)
}

func (m *Model) generatePresignCmd(key string) tea.Cmd {
	bucket := m.bucket
	return func() tea.Msg {
		url, err := m.client.PresignGetObject(bucket, key, time.Hour)
		return presignedURLMsg{key: key, url: url, err: err}
	}
}

// resolveDownloadDir determines where downloaded objects are written, from the
// app.downloadDir config value. A leading "~" is expanded to the user's home
// directory and an empty value falls back to the current working directory.
func resolveDownloadDir(cfg *config.Config) string {
	dir := "."
	if cfg != nil && strings.TrimSpace(cfg.App.DownloadDir) != "" {
		dir = strings.TrimSpace(cfg.App.DownloadDir)
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
		}
	}
	return dir
}

func (m *Model) downloadObjectCmd(key string) tea.Cmd {
	bucket := m.bucket
	dir := m.downloadDir
	ds := m.downloadState
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return downloadDoneMsg{key: key, dir: dir, err: fmt.Errorf("create directory %q: %w", dir, err)}
		}
		localPath := uniquePath(dir, filepath.Base(key))
		err := m.client.DownloadObject(bucket, key, localPath, ds)
		return downloadDoneMsg{key: key, path: localPath, dir: dir, err: err}
	}
}

// uniquePath returns dir/base, or a "name (N).ext" variant when a file with
// that name already exists, so a download never silently overwrites a file.
func uniquePath(dir, base string) string {
	path := filepath.Join(dir, base)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}
}

// downloadTickCmd schedules the next progress-bar refresh.
func downloadTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return downloadTickMsg{}
	})
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
		lines = append(lines, "", ui.MutedStyle().Render(detail))
	}
	return ui.LoadingBoxStyle().Render(lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func (m *Model) selectedObjectKey() (string, bool) {
	idx := m.objectTable.Cursor()
	if idx < 0 || idx >= len(m.objectMaps) {
		return "", false
	}
	r := m.objectMaps[idx]
	if r["type"] == "DIR" {
		return "", false
	}
	return m.prefix + r["name"], true
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

	// While the debug overlay is open, it consumes key/mouse input; every other
	// message falls through so loads keep streaming underneath.
	if m.debug.Visible() {
		if m.debug.HandleInput(msg) {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.debug.Refresh()
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

		if m.showCSV {
			m.layoutCSVTable()
		}
		if m.showArchive {
			m.layoutArchiveTable()
		}

		bucketTableHeight := m.height - 18
		if bucketTableHeight < 5 {
			bucketTableHeight = 5
		}
		m.bucketTable.SetHeight(bucketTableHeight)
		// Fixed columns (4+16+22) plus 2 cells of padding per column; the
		// name column stretches to fill the rest of the panel exactly.
		m.bucketTable.SetColumns([]table.Column{
			{Title: "#", Width: 4},
			{Title: "Name", Width: max(20, m.tableViewWidth()-42-8)},
			{Title: "Region", Width: 16},
			{Title: "Creation Date", Width: 22},
		})

		tableHeight := (m.height / 2) - 4
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.objectTable.SetHeight(tableHeight)
		m.updateObjectColumns()

		// Constrain both tables to the visible width: columns that do not fit
		// scroll horizontally (< / >) instead of overflowing the panel.
		m.bucketTable.SetWidth(m.tableViewWidth())
		m.objectTable.SetWidth(m.tableViewWidth())

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
					m.deleteConfirmErrMsg = ""
					m.deleteConfirm.SetValue("")
					m.deleteConfirm.Blur()
					cmds = append(cmds, m.deleteObjectCmd(key))
					return m, tea.Batch(cmds...)
				}
				m.deleteConfirmErrMsg = "Type exactly 'delete' (lowercase) to confirm"
			default:
				m.deleteConfirmErrMsg = ""
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
			case ui.KeyAbout:
				m.showAbout = !m.showAbout
				return m, nil
			case ui.KeySettings:
				m.settings = ui.NewSettingsModel(m.width, m.height, m.configPath, m.cfg)
				m.showSettings = true
				return m, nil
			case ui.KeyDebug:
				m.debug.Open(m.width, m.height)
				return m, nil
			}
		}

		if m.showHelp {
			if msg.String() == "esc" || msg.String() == "?" {
				m.showHelp = false
			}
			return m, nil
		}
		if m.showAbout {
			if msg.String() == "esc" || msg.String() == ui.KeyAbout {
				m.showAbout = false
			}
			return m, nil
		}
		if m.showCSV {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			// The typed delimiter prompt captures keys while open.
			if m.csvDelimEditing {
				switch msg.String() {
				case "enter":
					m.applyDelimiterInput()
				case "esc":
					m.csvDelimEditing = false
					m.csvDelimErr = ""
				default:
					m.csvDelimInput, _ = m.csvDelimInput.Update(msg)
				}
				return m, nil
			}
			if m.handleCSVKey(msg.String()) {
				return m, nil
			}
			// Everything else (↑/↓, PgUp/PgDn, g/G…) drives the table viewport.
			m.csvTable, _ = m.csvTable.Update(msg)
			return m, nil
		}
		if m.showArchive {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			if m.handleArchiveKey(msg.String()) {
				return m, nil
			}
			m.archiveTable, _ = m.archiveTable.Update(msg)
			return m, nil
		}
		if m.showPreview {
			if msg.String() == "esc" {
				// A member opened from an archive returns to the member list.
				if m.previewFromArchive {
					m.previewFromArchive = false
					m.showPreview = false
					m.showArchive = true
					return m, nil
				}
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
					// Start fresh at the bucket root — clear any prefix left
					// over from a previously browsed bucket.
					m.prefix = ""
					m.prefixInput.SetValue("")
					// Region may still be loading; fall back to the cache.
					if m.region == regionPending {
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
			case "r":
				// Force-refetch this bucket's details, bypassing the cache.
				delete(m.bucketDetailsCache, m.detailBucket)
				m.selectedBucketDetails = nil
				return m, m.fetchBucketDetails(m.detailBucket)
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
		case ">", ".":
			if t := m.activeTable(); t != nil {
				t.ScrollRight()
				return m, nil
			}

		case "<", ",":
			if t := m.activeTable(); t != nil {
				t.ScrollLeft()
				return m, nil
			}

		case "esc":
			m.err = nil
			if m.state == stateObjectList {
				if m.prefix != "" {
					// Step up one folder (to the object's parent) rather than all
					// the way back to the bucket list, so siblings stay browsable.
					m.prefix = parentPrefix(m.prefix)
					m.prefixInput.SetValue(m.prefix)
					cmds = append(cmds, m.loadObjects())
				} else {
					m.state = stateBucketList
					m.focus = focusBuckets
					m.bucket = ""
					m.prefix = ""
					cmds = append(cmds, m.loadBuckets())
				}
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
						if cmd := m.ensureBucketDetails(row[1]); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
					return m, tea.Batch(cmds...)
				}
			} else if m.state == stateObjectList && m.focus == focusObjects {
				// On-demand object metadata: fetch the selected file's extended
				// details (content-type, encryption, tags, ACL…) only when asked.
				// Return so "d" isn't also forwarded to the table (its keymap binds
				// d to half-page-down).
				if key, ok := m.selectedObjectKey(); ok && m.selectedDetails == nil && !m.detailsLoading {
					cmds = append(cmds, m.fetchObjectDetails(key))
				}
				return m, tea.Batch(cmds...)
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
				m.sortObjects(m.objectMaps)
				m.updateObjectColumns()
				m.objectTable.SetRows(m.buildObjectRows())
				return m, nil
			}

		case "s":
			if m.state == stateObjectList {
				m.sortCol = (m.sortCol + 1) % len(m.objectColFields())
				m.sortObjects(m.objectMaps)
				m.updateObjectColumns()
				m.objectTable.SetRows(m.buildObjectRows())
				return m, nil
			}

		case "r":
			// Refresh current view
			if m.state == stateBucketList {
				// A refresh means "show me live state": drop the cached
				// bucket details along with the list.
				m.bucketDetailsCache = make(map[string]*BucketDetails)
				m.selectedBucketDetails = nil
				cmds = append(cmds, m.loadBuckets())
			} else if m.state == stateObjectList {
				cmds = append(cmds, m.loadObjects())
			}

		case "L":
			// Continue a listing the page window cut off.
			if m.state == stateObjectList && m.objectsNextToken != nil && !m.loading {
				cmds = append(cmds, m.loadMoreObjects(), m.startSpinner())
			}

		case "C":
			if m.state == stateObjectList && len(m.objectMaps) > 0 {
				if path, err := m.exportObjectsCSV(); err != nil {
					m.statusMsg = "CSV export failed: " + err.Error()
				} else {
					m.statusMsg = "Exported " + path
				}
			}

		case "p":
			if m.state == stateObjectList && m.focus == focusObjects {
				if key, ok := m.selectedObjectKey(); ok {
					cmds = append(cmds, m.openPreview(key))
				}
			}

		case "o":
			// Console URL for the selection: object, or bucket (with the
			// current prefix) on the bucket list. Copy, and open in a
			// browser when the session is local.
			var url string
			switch {
			case m.state == stateObjectList && m.focus == focusObjects:
				if key, ok := m.selectedObjectKey(); ok {
					url = consolelink.S3ObjectURL(m.bucket, key, m.region)
				} else {
					url = consolelink.S3BucketURL(m.bucket, m.prefix, m.region)
				}
			case m.state == stateObjectList:
				url = consolelink.S3BucketURL(m.bucket, m.prefix, m.region)
			case m.state == stateBucketList || m.state == stateBucketDetail:
				if row := m.bucketTable.SelectedRow(); len(row) > 1 && row[1] != "" {
					url = consolelink.S3BucketURL(row[1], "", m.region)
				}
			}
			if url != "" {
				_ = clipboard.WriteAll(url)
				if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
					m.statusMsg = "Opened in browser · copied console URL"
				} else {
					m.statusMsg = "Copied console URL"
				}
				return m, nil
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
			if m.state == stateObjectList && m.focus == focusObjects && !m.downloading {
				if key, ok := m.selectedObjectKey(); ok {
					m.downloading = true
					m.downloadKey = key
					m.downloadPercent = 0
					m.downloadState = &DownloadProgress{}
					cmds = append(cmds, m.downloadObjectCmd(key), downloadTickCmd())
					m.statusMsg = ""
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
					// Start fresh at the bucket root — clear any prefix left
					// over from a previously browsed bucket.
					m.prefix = ""
					m.prefixInput.SetValue("")
					// Region may still be loading; fall back to the cache.
					if m.region == regionPending {
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
				idx := m.objectTable.Cursor()
				if idx >= 0 && idx < len(m.objectMaps) {
					r := m.objectMaps[idx]
					name := r["name"]
					if r["type"] == "DIR" {
						if name == ".." {
							m.prefix = parentPrefix(m.prefix)
						} else {
							m.prefix += name
						}
						m.prefixInput.SetValue(m.prefix)
						cmds = append(cmds, m.loadObjects())
					} else if key, ok := m.selectedObjectKey(); ok {
						cmds = append(cmds, m.openPreview(key))
					}
				}
			}
		}

	// --- Message handlers ---

	case bucketsScannedMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = "Could not list buckets: " + summarizeS3Error(msg.err)
			break
		}
		m.allBucketRows = m.allBucketRows[:0]
		pending := false
		for _, b := range msg.buckets {
			name, region, dateStr := bucketRow(b)
			// S3 reports each bucket's region in the listing itself, so the region
			// column is filled in immediately and correctly. Only buckets with no
			// region in the listing (a non-AWS S3-compatible endpoint) fall back to
			// the async per-bucket lookup.
			if region != regionPending {
				m.bucketRegionCache[name] = region
			} else {
				pending = true
			}
			m.allBucketRows = append(m.allBucketRows, table.Row{"", name, region, dateStr})
		}
		m.bucketTable.SetRows(seqRows(m.allBucketRows))
		if count := len(m.allBucketRows); count == 0 {
			m.statusMsg = "No accessible buckets found. Check IAM permissions."
		} else {
			m.statusMsg = fmt.Sprintf("%d bucket(s) found", count)
			if m.bucket == "" {
				m.bucket = m.allBucketRows[0][1]
				if cmd := m.ensureBucketDetails(m.bucket); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			if pending {
				cmds = append(cmds, m.fetchBucketRegions())
			}
		}

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
		m.objectsNextToken = msg.nextToken
		if msg.appended {
			// "Load more": extend the current view, keep the selection.
			m.objCount += msg.count
			m.totalSize += msg.size
			m.objectMaps = append(m.objectMaps, msg.maps...)
			m.sortObjects(m.objectMaps)
			m.updateObjectColumns()
			m.objectTable.SetRows(m.buildObjectRows())
			break
		}
		m.objCount = msg.count
		m.totalSize = msg.size
		m.objectMaps = msg.maps
		m.sortObjects(m.objectMaps)
		m.updateObjectColumns()
		m.objectTable.SetRows(m.buildObjectRows())

		m.lastSelectedKey = ""
		m.selectedDetails = nil

		if len(m.objectMaps) > 0 && m.objectMaps[0]["type"] != "DIR" {
			// On-demand: show cached metadata if we have it, but don't fetch on
			// load. The user requests it with "d".
			m.lastSelectedKey = m.prefix + m.objectMaps[0]["name"]
			m.selectedDetails = m.objectDetailsCache[m.lastSelectedKey]
		}

	case objectDetailsMsg:
		m.detailsLoading = false
		if msg.err == nil && msg.details != nil {
			m.objectDetailsCache[msg.key] = msg.details // serve revisits from cache
		}
		if msg.key == m.lastSelectedKey && msg.err == nil {
			m.selectedDetails = msg.details
		}

	case bucketDetailsMsg:
		if msg.err == nil && msg.details != nil {
			m.bucketDetailsCache[msg.bucket] = msg.details
		}
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
			switch {
			case m.showCSV && msg.err == nil && m.initCSV(msg.content):
				// CSV parsed: the full-screen table is ready.
			case m.showCSV:
				// A .csv that doesn't parse as a table — fall back to raw text.
				m.showCSV = false
				m.showPreview = true
				m.initPreviewViewport(msg.content, msg.err)
			default:
				m.initPreviewViewport(msg.content, msg.err)
			}
		}

	case archiveLoadedMsg:
		if msg.key == m.archiveKey {
			m.applyArchiveLoaded(msg)
		}

	case presignedURLMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Presign error: %s", summarizeS3Error(msg.err))
		} else {
			m.presignedURL = msg.url
			m.showPresigned = true
		}

	case downloadTickMsg:
		if m.downloading && m.downloadState != nil {
			if total := m.downloadState.Total(); total > 0 {
				m.downloadPercent = float64(m.downloadState.Written()) / float64(total)
			}
			cmds = append(cmds, downloadTickCmd())
		}

	case downloadDoneMsg:
		m.downloading = false
		m.downloadState = nil
		if msg.err != nil {
			m.downloadPercent = 0
			m.statusMsg = fmt.Sprintf("Download error: %s", summarizeS3Error(msg.err))
		} else {
			m.downloadPercent = 1
			m.statusMsg = fmt.Sprintf("Object %s is downloaded to %s", filepath.Base(msg.key), msg.dir)
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
				// Served from the session cache when this bucket was already
				// visited — scrolling the list doesn't refetch ~19 calls per row.
				if cmd := m.ensureBucketDetails(row[1]); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	} else if m.state == stateObjectList {
		prevRow := m.objectTable.Cursor()
		m.objectTable, cmd = m.objectTable.Update(msg)
		cmds = append(cmds, cmd)

		if m.focus == focusObjects && prevRow != m.objectTable.Cursor() {
			idx := m.objectTable.Cursor()
			if idx >= 0 && idx < len(m.objectMaps) {
				r := m.objectMaps[idx]
				if r["type"] != "DIR" {
					m.lastSelectedKey = m.prefix + r["name"]
					// On-demand: don't fetch metadata while scrolling. Serve it from
					// the cache if this key was fetched before; otherwise leave it for
					// the user to request with "d".
					m.selectedDetails = m.objectDetailsCache[m.lastSelectedKey]
				} else {
					m.lastSelectedKey = ""
					m.selectedDetails = nil
				}
			}
		}
	}

	if m.isWaiting() && !spinnerTickScheduled {
		cmds = append(cmds, m.startSpinner())
	}

	return m, tea.Batch(cmds...)
}

// PageTitle names the current screen for the terminal window/tab title, so
// every page has a unique, shareable name (see ui.WithWindowTitle).
func (m *Model) PageTitle() string {
	switch m.state {
	case stateObjectList:
		title := "S3 Browser › " + m.bucket
		if m.prefix != "" {
			title += "/" + m.prefix
		}
		return title
	case stateBucketDetail:
		return "S3 Browser › " + m.detailBucket + " › Bucket detail"
	default:
		return "S3 Browser › Buckets"
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing…"
	}

	// Bucket detail full-screen view
	if m.state == stateBucketDetail {
		out := ui.AppStyle().Render(m.bucketDetailView())
		return m.debug.Overlay(out, m.width, m.height)
	}

	// Full-screen CSV table view: a CSV can be very wide and very tall, so it
	// gets the whole window rather than a narrow overlay.
	if m.showCSV {
		out := ui.AppStyle().Render(m.csvView())
		return m.debug.Overlay(out, m.width, m.height)
	}

	// Full-screen archive member browser.
	if m.showArchive {
		out := ui.AppStyle().Render(m.archiveView())
		return m.debug.Overlay(out, m.width, m.height)
	}

	var content string

	headerText := "S3 TUI v1.3.0"
	if info := authDisplayInfo(m.awsCfg); info != "" {
		headerText += "   " + info
	}
	if m.region == "" && m.state == stateBucketList {
		headerText += "   Regions: all"
	}
	if m.flatMode {
		headerText += "   [FLAT]"
	}
	if m.showVersions {
		headerText += "   [VERSIONS:ON]"
	}
	header := ui.HeaderStyle().Render(headerText)
	// A pinned region gets a distinctive badge (joined after the styled header
	// so its colors don't bleed into the rest of the title).
	if badge := ui.RegionBadge([]string{m.region}, false); badge != "" {
		header = lipgloss.JoinHorizontal(lipgloss.Top, header, " ", badge)
	}

	if m.err != nil {
		maxErrW := m.width - 20
		if maxErrW < 40 {
			maxErrW = 40
		}
		errBox := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ui.ColorError())).
			Foreground(lipgloss.Color(ui.ColorText())).
			Padding(1, 2).
			Width(maxErrW).
			Align(lipgloss.Center).
			Render(fmt.Sprintf("Failed to access bucket: %s\n\n%s\n\nPress [Esc] to return to the bucket list.", m.bucket, ui.ErrorStyle().Render(m.err.Error())))

		content = lipgloss.Place(m.width-4, m.height-10, lipgloss.Center, lipgloss.Center, errBox)
	} else if m.loading {
		message := "Listing buckets…"
		detail := "Fetching your S3 buckets and their regions."
		if m.state == stateObjectList {
			message = "Loading objects…"
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

	// Overlays. (The settings console is composited over the final frame
	// below, so the live app stays visible around it.)
	if m.showHelp {
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, m.helpView())
	} else if m.showAbout {
		about := ui.AboutView("About — S3 Browser", s3AboutText, ui.AboutWidth(m.width))
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Center, about)
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
			BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
			Foreground(lipgloss.Color(ui.ColorText())).
			Padding(0, 1).
			Render(lipgloss.JoinVertical(lipgloss.Left,
				ui.BoldStyle().Render("Search buckets:"),
				m.bucketSearch.View(),
				ui.MutedStyle().Render("[Enter] Select first  [Esc] Cancel"),
			))
		content = lipgloss.Place(m.width-4, max(8, m.height-8), lipgloss.Center, lipgloss.Top, searchBox)
	}

	out := ui.AppStyle().Render(lipgloss.JoinVertical(lipgloss.Left,
		header,
		ui.FeatherRail(max(12, m.width-4)),
		"",
		content,
		"",
		m.renderStatusBar(),
	))
	if m.width > 0 && m.height > 0 {
		out = ui.ClipToSize(out, m.width, m.height)
	}
	if m.showSettings {
		// HUD-style: float the fixed-size console over the live app.
		out = ui.OverlayCenter(out, m.settings.View(), m.width, m.height)
		if m.width > 0 && m.height > 0 {
			out = ui.ClipToSize(out, m.width, m.height)
		}
	}
	if m.debug.Visible() {
		// Float the debug pane over the live app, above any other overlay.
		out = m.debug.Overlay(out, m.width, m.height)
		if m.width > 0 && m.height > 0 {
			out = ui.ClipToSize(out, m.width, m.height)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Sub-views
// ---------------------------------------------------------------------------

func (m *Model) renderStatusBar() string {
	barWidth := max(12, m.width-4)

	var left string

	switch m.state {
	case stateBucketDetail:
		tabNames := []string{"Overview", "Access & Security", "Data Protection", "Operational", "Tags"}
		left = fmt.Sprintf("Bucket: %s  |  %s", m.detailBucket, tabNames[m.detailTabIdx])
	case stateBucketList:
		switch {
		case m.statusMsg != "":
			left = m.statusMsg
		default:
			left = fmt.Sprintf("Buckets: %d", len(m.allBucketRows))
		}
	case stateObjectList:
		if m.downloading {
			ds := m.downloadState
			var sizeInfo string
			if ds != nil && ds.Total() > 0 {
				sizeInfo = fmt.Sprintf(" %s / %s", formatSize(ds.Written()), formatSize(ds.Total()))
			}
			left = fmt.Sprintf("Downloading %s %s%s", filepath.Base(m.downloadKey), m.downloadProgress.ViewAs(m.downloadPercent), sizeInfo)
		} else if m.statusMsg != "" {
			left = m.statusMsg
		} else {
			objects := fmt.Sprintf("%d", m.objCount)
			if m.objectsNextToken != nil {
				// Be honest about the page window: the bucket has more keys
				// than are on screen.
				objects += "+ (truncated — L loads more)"
			}
			left = fmt.Sprintf("Bucket: %s  |  Objects: %s  |  Size: %s", m.bucket, objects, formatSize(m.totalSize))
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
	default:
		left = m.statusMsg
	}

	return ui.StatusBar(barWidth, left, m.statusHints())
}

// statusHints returns only the shortcuts that are usable right now, given the
// open overlay, current state and focus. Hints are ordered most-important
// first; the bar elides from the tail when the terminal is narrow, always
// keeping the final hint visible.
func (m *Model) statusHints() []ui.KeyHint {
	// Overlays and inputs capture the keyboard, so only their keys are shown.
	switch {
	case m.confirmingDelete:
		return []ui.KeyHint{ui.H("type 'delete'", ""), ui.H("Enter", "confirm"), ui.H("Esc", "cancel")}
	case m.showPresigned:
		return []ui.KeyHint{ui.H("Esc", "close")}
	case m.copyMenuActive:
		return []ui.KeyHint{ui.H("y/Esc", "close")}
	case m.showHelp:
		return []ui.KeyHint{ui.H("?/Esc", "close help")}
	case m.showAbout:
		return []ui.KeyHint{ui.H("i/Esc", "close about")}
	case m.showPreview:
		return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("PgUp/PgDn", "page"), ui.H("Esc", "close")}
	case m.inBucketSearch:
		return []ui.KeyHint{ui.H("type", "to filter"), ui.H("Enter", "open first match"), ui.H("Esc", "cancel")}
	case m.focus == focusPrefixInput:
		return []ui.KeyHint{ui.H("Enter", "go to prefix"), ui.H("Esc", "cancel")}
	}

	switch m.state {
	case stateBucketDetail:
		return []ui.KeyHint{
			ui.H("Tab/Shift+Tab", "switch tab"),
			ui.H("r", "refresh"),
			ui.H("Esc", "back"),
			ui.H("q", "quit"),
		}
	case stateBucketList:
		hints := []ui.KeyHint{
			ui.H("↑/↓", "navigate"),
			ui.H("Enter", "open bucket"),
			ui.H("d", "details"),
			ui.H("o", "console"),
			ui.H("/", "search"),
		}
		hints = append(hints, colScrollHints(&m.bucketTable)...)
		return append(hints, ui.H("r", "refresh"), ui.H("S", "theme"), ui.H("~", "debug"), ui.H("i", "about"), ui.H("q", "quit"), ui.H("?", "help"))
	case stateObjectList:
		hints := []ui.KeyHint{
			ui.H("↑/↓", "navigate"),
			ui.H("Enter", "open"),
			ui.H("p", "preview"),
			ui.H("d", "details"),
		}
		hints = append(hints, colScrollHints(&m.objectTable)...)
		hints = append(hints,
			ui.H("/", "prefix"),
			ui.H("D", "download"),
		)
		if m.allowDelete {
			hints = append(hints, ui.H("x", "delete"))
		}
		if m.objectsNextToken != nil {
			hints = append(hints, ui.H("L", "load more"))
		}
		hints = append(hints,
			ui.H("y", "copy URI"),
			ui.H("o", "console"),
			ui.H("g", "presign"),
			ui.H("s", "sort"),
			ui.H("C", "csv"),
			ui.H("f", "flat"),
			ui.H("r", "refresh"),
			ui.H("Esc", "back"),
		)
		return append(hints, ui.H("~", "debug"), ui.H("i", "about"), ui.H("?", "help"))
	}
	return []ui.KeyHint{ui.H("q", "quit")}
}

// colScrollHints advertises horizontal column scrolling only when the table
// actually has columns hidden off-screen.
func colScrollHints(t *table.Model) []ui.KeyHint {
	if l, r := t.ColScrollInfo(); l+r > 0 {
		return []ui.KeyHint{ui.H("</>", fmt.Sprintf("cols (%d more)", l+r))}
	}
	return nil
}

// tablePanel wraps a table in the shared themed panel, appending the
// horizontal-scroll indicator when columns are hidden off-screen.
func tablePanel(t *table.Model, focused bool) string {
	view := t.View()
	if ind := ui.TableScrollIndicator(t); ind != "" {
		view = lipgloss.JoinVertical(lipgloss.Left, view, ind)
	}
	return ui.TablePanelStyle(focused).Render(view)
}

func (m *Model) bucketListView() string {
	tableSection := tablePanel(&m.bucketTable, m.focus == focusBuckets)
	if len(m.bucketTable.Rows()) == 0 {
		// Listing still in flight (or no buckets at all): show a spinner or an
		// empty-state line instead of a bare table grid.
		body := ui.MutedStyle().Render("No buckets found")
		if m.loading {
			body = m.loadingLine("Loading buckets…")
		}
		tableSection = ui.TablePanelStyle(m.focus == focusBuckets).Render(body)
	}

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

		metaText = m.loadingLine("Loading bucket details…")
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

	// Breadcrumb of the current location: bucket plus prefix components,
	// left-truncated so the trailing components stay visible.
	crumbW := max(16, m.tableViewWidth()-lipgloss.Width(headerRight)-7)
	bucketHeader := lipgloss.JoinHorizontal(lipgloss.Top,
		ui.BadgeStyle().Render(breadcrumb(m.bucket, m.prefix, crumbW)),
		"   ",
		headerRight,
	)

	prefixSection := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Render("Prefix: "),
		m.prefixInput.View(),
	)

	tableSection := tablePanel(&m.objectTable, m.focus == focusObjects)
	if len(m.objectMaps) == 0 {
		// Initial fetch in flight or a genuinely empty listing: show a spinner
		// or an empty-state line instead of a bare table grid.
		body := ui.MutedStyle().Render("No objects under this prefix")
		if m.loading {
			body = m.loadingLine("Loading objects…")
		}
		tableSection = ui.TablePanelStyle(m.focus == focusObjects).Render(body)
	} else if m.objectsNextToken != nil {
		tableSection = lipgloss.JoinVertical(lipgloss.Left,
			tableSection,
			ui.MutedStyle().Render("More objects available · press L"),
		)
	}

	// Details Panel — always render two fixed-size boxes so nothing below shifts.
	const detailsHeight = 10
	boxWidth := max(20, m.width/2-4)

	detailsContent := ui.MutedStyle().Render("Select an object to view details.")
	metaText := ""
	idx := m.objectTable.Cursor()
	if idx >= 0 && idx < len(m.objectMaps) {
		r := m.objectMaps[idx]
		name := r["name"]
		size := r["size"]
		date := r["last_modified"]
		class := r["storage_class"]
		etag := r["etag"]

		isDir := r["type"] == "DIR"

		detailsContent = fmt.Sprintf("Key: %s%s\nSize: %s\nLast Modified: %s\nStorage Class: %s\nETag: %s",
			m.prefix, name, size, date, class, etag)

		if isDir {
			metaText = "Status: N/A"
		} else {
			if m.detailsLoading {
				metaText = m.loadingLine("Loading object metadata…")
			} else if m.selectedDetails == nil {
				metaText = ui.MutedStyle().Render("Press d to load metadata (content-type, encryption, tags, ACL…)")
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
					kmsKey = kmsKey[:20] + "…"
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
		body = m.loadingLine("Loading bucket details…")
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
				policyTrunc = policyTrunc[:80] + "…"
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
			BorderForeground(lipgloss.Color(ui.ColorBorder())).
			Foreground(lipgloss.Color(ui.ColorText())).
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
	errLine := ""
	if m.deleteConfirmErrMsg != "" {
		errLine = ui.ErrorStyle().Render("  ✗ " + m.deleteConfirmErrMsg)
	}
	rows := []string{
		ui.BoldStyle().Render(fmt.Sprintf("DELETE OBJECT: %s", m.deleteKey)),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render("This action is PERMANENT and cannot be undone."),
		"",
		m.deleteConfirm.View(),
	}
	if errLine != "" {
		rows = append(rows, errLine)
	}
	rows = append(rows, "", ui.MutedStyle().Render("Type 'delete' and press Enter to confirm. Esc to cancel."))
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	h := 10
	if errLine != "" {
		h = 12
	}
	return ui.ModalStyle(width, h).Render(content)
}

// s3AboutText explains what the S3 browser is for, shown in the About overlay
// ("i").
const s3AboutText = "This is the dedicated S3 browser. The first screen lists your buckets " +
	"(with details on d); Enter opens a bucket and you navigate its prefixes like " +
	"folders, drilling into objects.\n\n" +
	"On an object you can preview its contents (p) — a CSV or TSV opens in a " +
	"full-screen, scrollable table with auto-detected delimiter (press s to change " +
	"it, w to adjust how many rows are shown, t for raw text). A .gz is decompressed " +
	"and shown by its inner type, and a .tar/.tar.gz/.tgz opens a browser of its " +
	"members so you can open any file inside. You can also copy its S3 URI (y), open it " +
	"in the AWS console (o), generate a 1-hour presigned URL (g) and download it " +
	"(D). Use / to jump to a prefix, f to flatten the listing, s to sort, and L to " +
	"load more when a listing is truncated.\n\n" +
	"Press ? for the full, context-aware list of keyboard shortcuts."

// helpView renders the help overlay. It is context-aware: only the sections
// that apply to the current screen are shown, so the bucket list never
// advertises object shortcuts and vice versa.
func (m *Model) helpView() string {
	sections := []string{
		"Navigation",
		"  ↑/↓, PgUp/PgDn     Move selection",
		"  </>                Scroll table columns (when more columns than fit)",
		"  Enter              Open bucket, prefix, or object preview",
		"  Esc                Back, close preview/help, or clear prefix input",
	}

	title := "S3 Explorer Help"
	switch m.state {
	case stateBucketList, stateBucketDetail:
		title = "S3 Explorer Help — Buckets"
		sections = append(sections,
			"",
			"Buckets",
			"  /                  Search/filter buckets",
			"  d                  Full bucket detail view",
			"  Tab / Shift+Tab    Switch tabs (in detail view)",
			"  r                  Refresh bucket list",
		)
	case stateObjectList:
		title = "S3 Explorer Help — Objects"
		deleteSection := ""
		if m.allowDelete {
			deleteSection = "\n  x                  Delete selected object (requires confirmation)"
		}
		sections = append(sections,
			"",
			"Objects",
			"  /                  Jump to prefix",
			"  p                  Preview object (CSV→table, .gz→decompress, .tar→browse members)",
			"  d                  Load extended metadata for the selected object (on demand)",
			"  y                  Copy S3 URI to clipboard",
			"  o                  Open bucket/object in the AWS console (copies the URL)",
			"  g                  Generate presigned URL (1 hour)",
			"  D                  Download object (to app.downloadDir, default current dir)",
			"  f                  Toggle flat mode (show all objects)",
			"  v                  Toggle versions indicator",
			"  s                  Cycle sort column",
			"  R                  Reverse sort direction",
			"  L                  Load more objects (when the listing is truncated)",
			"  C                  Export current listing to CSV (~/.aws_explorer/exports)",
			"  r                  Refresh object list"+deleteSection,
		)
	}

	sections = append(sections,
		"",
		"Utility",
		"  S                  Settings (theme & colors)",
		"  ~                  Debug: live view of what the tool is doing",
		"  i                  About this page (what it does)",
		"  ?                  Toggle this help",
		"  q, Ctrl+C          Quit",
	)
	// Flatten any multi-line entries (the optional delete row rides on the
	// refresh line) so each shortcut is its own line, then order shortcuts
	// within each section by key while keeping the section grouping.
	lines := strings.Split(strings.Join(sections, "\n"), "\n")
	lines = ui.SortHelpSections(lines)
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return ui.HelpView(title, body, min(88, max(32, m.width-12)))
}

func (m *Model) previewView() string {
	var body string
	if m.previewErr != nil {
		body = "Preview failed: " + summarizeS3Error(m.previewErr)
	} else if m.previewLoading {
		body = m.loadingLine("Loading preview…")
	} else if m.previewContent == "" {
		body = "Object is empty."
	} else {
		bar := ui.VScrollbar(
			m.previewViewport.Height,
			m.previewViewport.TotalLineCount(),
			m.previewViewport.VisibleLineCount(),
			m.previewViewport.YOffset,
		)
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.previewViewport.View(), " ", bar)
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
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Foreground(lipgloss.Color(ui.ColorText())).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", ui.MutedStyle().Render("[↑/↓/PgUp/PgDn] Scroll  [Esc] Close")))
}
