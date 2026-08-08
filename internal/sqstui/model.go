package sqstui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/debugpane"
	"github.com/ryandam9/aws_explorer/internal/downloads"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

type activeView int

const (
	viewOverview activeView = iota
	viewMessages
)

// metricsRefreshFloor is the minimum interval between metric fetches for one
// queue. GetMetricData is a paid API, so m within the floor re-shows the
// cached series instead of re-querying (the Cost Explorer rate-floor rule).
const metricsRefreshFloor = time.Minute

// metricsEntry caches one queue's sparkline series with its fetch time (for
// the refresh floor) and any fetch error (surfaced, never blanked).
type metricsEntry struct {
	fetchedAt time.Time
	series    []MetricSeries
	err       error
}

type model struct {
	ctx        context.Context
	awsCfg     *config.AWSConfig
	regions    []string
	allRegions bool
	configPath string
	appCfg     *config.Config
	client     *Client

	width  int
	height int

	// Queue list (sidebar)
	queues        []Queue
	filtered      []Queue
	selectedIdx   int
	search        textinput.Model
	searchActive  bool
	queuesLoading bool

	// Per-queue detail, cached by region|url. detailLoading tracks only the
	// currently selected queue; stale responses still land in the cache.
	details       map[string]QueueDetail
	detailLoading bool

	view activeView

	// Message peek. peekConfirm gates the fetch behind an explicit
	// side-effect-stating confirmation; peekQueue pins which queue the
	// messages view (and any in-flight peek) belongs to.
	peekConfirm bool
	peekLoading bool
	peekQueue   Queue
	messages    []types.Message
	msgTable    table.Model

	// Message record overlay (v): one message vertically, full values.
	recordActive bool
	recordVP     viewport.Model
	recordText   string

	// Metric sparklines (m), cached per queue with a refresh floor.
	metrics        map[string]metricsEntry
	metricsVisible bool
	metricsLoading bool

	// Consumer-log jump (L): resolving event source mappings before the
	// ExecProcess hand-off.
	jumpLoading bool

	showAbout bool
	showHelp  bool

	spinner  spinner.Model
	err      error
	toast    string
	toastExp time.Time

	debug debugpane.Model
}

// Msg types
type queuesMsg struct {
	queues []Queue
	err    error
}

type detailMsg struct {
	key    string
	detail QueueDetail
}

type peekMsg struct {
	key  string
	msgs []types.Message
	err  error
}

type metricsMsg struct {
	key    string
	series []MetricSeries
	err    error
}

type consumersMsg struct {
	key       string
	region    string
	consumers []Consumer
	err       error
}

type cwJumpDoneMsg struct{ err error }

type clearToastMsg struct{}

// NewModel builds the SQS explorer over one or more regions (all enabled
// regions when allRegions is true). queueFilter pre-populates the sidebar
// search and is also applied server-side as a ListQueues name prefix.
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool, configPath string, appCfg *config.Config, queueFilter string) (tea.Model, error) {
	client, err := NewClient(ctx, awsCfg, regions, allRegions)
	if err != nil {
		return nil, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	search := textinput.New()
	search.Placeholder = "Filter queues…"
	search.Width = 30
	search.SetValue(queueFilter)

	return &model{
		ctx:        ctx,
		awsCfg:     awsCfg,
		regions:    client.Regions(),
		allRegions: allRegions,
		configPath: configPath,
		appCfg:     appCfg,
		client:     client,
		spinner:    s,
		search:     search,
		details:    map[string]QueueDetail{},
		metrics:    map[string]metricsEntry{},
	}, nil
}

func (m *model) Init() tea.Cmd {
	m.queuesLoading = true
	return tea.Batch(m.spinner.Tick, m.loadQueuesCmd())
}

// detailKey identifies a queue across regions (the same queue name can exist
// in several regions).
func detailKey(q Queue) string {
	return q.Region + "|" + q.URL
}

// selectedQueue returns the highlighted queue, or false when the filtered
// list is empty.
func (m *model) selectedQueue() (Queue, bool) {
	if len(m.filtered) == 0 || m.selectedIdx >= len(m.filtered) {
		return Queue{}, false
	}
	return m.filtered[m.selectedIdx], true
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// While the debug overlay is open it consumes key/mouse input; every
	// other message falls through so loads keep streaming underneath.
	if m.debug.Visible() {
		if m.debug.HandleInput(msg) {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		m.debug.Refresh()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		m.toast = ""

	case queuesMsg:
		m.queuesLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.queues = msg.queues
			m.filterQueues()
			if q, ok := m.selectedQueue(); ok {
				m.detailLoading = true
				cmds = append(cmds, m.loadDetailCmd(q))
			}
		}

	case detailMsg:
		// Cache every response (arrowing through queues races several
		// fetches); only the selected queue's response clears the spinner.
		m.details[msg.key] = msg.detail
		if q, ok := m.selectedQueue(); ok && detailKey(q) == msg.key {
			m.detailLoading = false
		}

	case peekMsg:
		if detailKey(m.peekQueue) != msg.key {
			return m, tea.Batch(cmds...) // stale peek for a de-selected queue
		}
		m.peekLoading = false
		if msg.err != nil {
			m.setToast("Peek failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
		} else {
			m.messages = msg.msgs
			m.buildMessagesTable()
			m.view = viewMessages
		}

	case metricsMsg:
		m.metricsLoading = false
		m.metrics[msg.key] = metricsEntry{fetchedAt: time.Now(), series: msg.series, err: msg.err}

	case consumersMsg:
		m.jumpLoading = false
		if q, ok := m.selectedQueue(); !ok || detailKey(q) != msg.key {
			return m, tea.Batch(cmds...)
		}
		if msg.err != nil {
			m.setToast("Consumer lookup failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
			break
		}
		if len(msg.consumers) == 0 {
			// Only Lambda event source mappings are discoverable; say so
			// rather than letting "not found" read as "nothing consumes it".
			m.setToast("No Lambda consumers found (other consumer types are not discoverable)")
			cmds = append(cmds, toastCmd(4*time.Second))
			break
		}
		if len(msg.consumers) > 1 {
			m.setToast(fmt.Sprintf("%d Lambda consumers — opening logs for %s", len(msg.consumers), msg.consumers[0].FunctionName))
			cmds = append(cmds, toastCmd(4*time.Second))
		}
		cmds = append(cmds, m.jumpToLogsCmd(msg.region, "/aws/lambda/"+msg.consumers[0].FunctionName))

	case cwJumpDoneMsg:
		if msg.err != nil {
			m.setToast("CloudWatch Logs jump failed: " + msg.err.Error())
			cmds = append(cmds, toastCmd(4*time.Second))
		}

	case tea.KeyMsg:
		// Error screen: Enter/Esc clears the error and retries, q quits.
		if m.err != nil {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.err = nil
				if len(m.queues) == 0 {
					m.queuesLoading = true
					cmds = append(cmds, m.loadQueuesCmd())
				}
			}
			return m, tea.Batch(cmds...)
		}

		// About/help overlays: static text, any key closes them.
		if m.showAbout {
			m.showAbout = false
			return m, tea.Batch(cmds...)
		}
		if m.showHelp {
			m.showHelp = false
			return m, tea.Batch(cmds...)
		}

		// Message record view owns its scroll/copy/close keys.
		if m.recordActive {
			m.handleRecordKeys(msg, &cmds)
			return m, tea.Batch(cmds...)
		}

		// Peek confirmation: an explicit yes runs the peek; anything else
		// backs out. No other key may fall through a consent prompt.
		if m.peekConfirm {
			switch msg.String() {
			case "y", "enter":
				m.peekConfirm = false
				if q, ok := m.selectedQueue(); ok {
					m.peekQueue = q
					m.peekLoading = true
					cmds = append(cmds, m.peekCmd(q))
				}
			default:
				m.peekConfirm = false
			}
			return m, tea.Batch(cmds...)
		}

		if m.searchActive {
			switch msg.String() {
			case "enter":
				m.searchActive = false
				m.filterQueues()
				cmds = append(cmds, m.eagerDetailCmd())
			case "esc":
				m.searchActive = false
				m.search.SetValue("")
				m.filterQueues()
				cmds = append(cmds, m.eagerDetailCmd())
			default:
				var cmd tea.Cmd
				m.search, cmd = m.search.Update(msg)
				cmds = append(cmds, cmd)
				m.filterQueues()
			}
			return m, tea.Batch(cmds...)
		}

		if m.view == viewMessages {
			m.handleMessagesKeys(msg, &cmds)
			return m, tea.Batch(cmds...)
		}

		m.handleOverviewKeys(msg, &cmds)
	}

	return m, tea.Batch(cmds...)
}

// handleOverviewKeys processes input while the queue browser/overview is
// active.
func (m *model) handleOverviewKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		*cmds = append(*cmds, tea.Quit)

	case "up", "k":
		m.moveSelection(-1, cmds)
	case "down", "j":
		m.moveSelection(1, cmds)

	case "/":
		m.searchActive = true
		m.search.Focus()

	case "P":
		if _, ok := m.selectedQueue(); ok {
			m.peekConfirm = true
		}

	case "d":
		m.jumpToDLQ(cmds)

	case "m":
		m.toggleMetrics(cmds)

	case "L":
		m.startConsumerJump(cmds)

	case "o":
		if q, ok := m.selectedQueue(); ok {
			if arn := m.queueARN(q); arn != "" {
				if url, ok := consolelink.FromARN(arn); ok {
					_ = clipboard.WriteAll(url)
					if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
						m.setToast("Opened in browser · copied console URL")
					} else {
						m.setToast("Copied console URL")
					}
					*cmds = append(*cmds, toastCmd(3*time.Second))
				}
			}
		}

	case "y":
		if q, ok := m.selectedQueue(); ok {
			_ = clipboard.WriteAll(q.URL)
			m.setToast("Copied queue URL to clipboard")
			*cmds = append(*cmds, toastCmd(3*time.Second))
		}

	case "r":
		if q, ok := m.selectedQueue(); ok {
			m.detailLoading = true
			delete(m.metrics, detailKey(q)) // a manual refresh may re-fetch metrics too
			*cmds = append(*cmds, m.loadDetailCmd(q))
			if m.metricsVisible {
				m.metricsLoading = true
				*cmds = append(*cmds, m.loadMetricsCmd(q))
			}
		}

	case "R":
		m.queuesLoading = true
		*cmds = append(*cmds, m.loadQueuesCmd())

	case ui.KeyDebug:
		m.debug.Open(m.width, m.height)
	case ui.KeyAbout:
		m.showAbout = true
	case ui.KeyHelp:
		m.showHelp = true
	}
}

// handleMessagesKeys processes input while the peeked-messages view is open.
func (m *model) handleMessagesKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		*cmds = append(*cmds, tea.Quit)
	case "esc", "backspace":
		m.view = viewOverview
	case "up", "k":
		m.msgTable.MoveUp(1)
	case "down", "j":
		m.msgTable.MoveDown(1)
	case "pgup", "ctrl+u":
		m.msgTable.MoveUp(10)
	case "pgdown", "ctrl+d":
		m.msgTable.MoveDown(10)
	case "g", "home":
		m.msgTable.GotoTop()
	case "G", "end":
		m.msgTable.GotoBottom()
	case "v", "enter":
		if cur := m.msgTable.Cursor(); cur >= 0 && cur < len(m.messages) {
			m.openMessageRecord(m.messages[cur])
		}
	case "y":
		if cur := m.msgTable.Cursor(); cur >= 0 && cur < len(m.messages) {
			_ = clipboard.WriteAll(aws.ToString(m.messages[cur].Body))
			m.setToast("Copied message body to clipboard")
			*cmds = append(*cmds, toastCmd(3*time.Second))
		}
	case "s":
		m.exportMessages(cmds)
	case "P":
		// Re-peek the same queue (another sample, another receive-count
		// increment) — back through the confirmation.
		m.view = viewOverview
		m.peekConfirm = true
	case ui.KeyDebug:
		m.debug.Open(m.width, m.height)
	case ui.KeyHelp:
		m.showHelp = true
	}
}

// handleRecordKeys processes keys while the message record view is open.
func (m *model) handleRecordKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v", "enter":
		m.recordActive = false
	case "up", "k":
		m.recordVP.ScrollUp(1)
	case "down", "j":
		m.recordVP.ScrollDown(1)
	case "pgup", "ctrl+u":
		m.recordVP.ScrollUp(m.recordVP.Height)
	case "pgdown", "ctrl+d":
		m.recordVP.ScrollDown(m.recordVP.Height)
	case "y":
		_ = clipboard.WriteAll(m.recordText)
		m.setToast("Copied message record to clipboard")
		*cmds = append(*cmds, toastCmd(3*time.Second))
	case ui.KeyHelp:
		m.showHelp = true
	}
}

// moveSelection navigates the queue list and eagerly loads the newly selected
// queue's detail (cache-first; stale responses are dropped by key).
func (m *model) moveSelection(dir int, cmds *[]tea.Cmd) {
	if len(m.filtered) == 0 {
		return
	}
	m.selectedIdx = (m.selectedIdx + dir + len(m.filtered)) % len(m.filtered)
	*cmds = append(*cmds, m.eagerDetailCmd())
}

// eagerDetailCmd fetches the selected queue's detail unless it is already
// cached (never re-fetch on cursor movement over a visited queue).
func (m *model) eagerDetailCmd() tea.Cmd {
	q, ok := m.selectedQueue()
	if !ok {
		return nil
	}
	if _, cached := m.details[detailKey(q)]; cached {
		m.detailLoading = false
		return nil
	}
	m.detailLoading = true
	return m.loadDetailCmd(q)
}

func (m *model) filterQueues() {
	term := strings.ToLower(m.search.Value())
	if term == "" {
		m.filtered = m.queues
	} else {
		var list []Queue
		for _, q := range m.queues {
			if strings.Contains(strings.ToLower(q.Name), term) || strings.Contains(q.Region, term) {
				list = append(list, q)
			}
		}
		m.filtered = list
	}
	if m.selectedIdx >= len(m.filtered) {
		m.selectedIdx = max(0, len(m.filtered)-1)
	}
}

// queueARN returns the selected queue's ARN from its cached attributes, or
// the ARN derived from the URL when attributes are unavailable.
func (m *model) queueARN(q Queue) string {
	if d, ok := m.details[detailKey(q)]; ok && d.Attrs != nil {
		if arn := d.Attrs["QueueArn"]; arn != "" {
			return arn
		}
	}
	return awsutil.SQSARNFromURL(q.URL)
}

// jumpToDLQ moves the selection to the queue's redrive target, when it has
// one and that queue is in the loaded list.
func (m *model) jumpToDLQ(cmds *[]tea.Cmd) {
	q, ok := m.selectedQueue()
	if !ok {
		return
	}
	d, ok := m.details[detailKey(q)]
	if !ok || d.Attrs == nil {
		m.setToast("Queue attributes not loaded yet")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	rd, ok := parseRedrive(d.Attrs["RedrivePolicy"])
	if !ok {
		m.setToast("This queue has no redrive policy (no DLQ)")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	target := queueNameFromARN(rd.TargetARN)
	for i, cand := range m.filtered {
		if cand.Name == target && cand.Region == q.Region {
			m.selectedIdx = i
			*cmds = append(*cmds, m.eagerDetailCmd())
			return
		}
	}
	m.setToast("DLQ " + target + " is not in the current list (check region scope / filter)")
	*cmds = append(*cmds, toastCmd(4*time.Second))
}

// toggleMetrics shows/hides the metric sparklines, fetching (or refreshing)
// them only when the cached series is older than the refresh floor.
func (m *model) toggleMetrics(cmds *[]tea.Cmd) {
	q, ok := m.selectedQueue()
	if !ok {
		return
	}
	if m.metricsVisible {
		m.metricsVisible = false
		return
	}
	m.metricsVisible = true
	if entry, ok := m.metrics[detailKey(q)]; ok && time.Since(entry.fetchedAt) < metricsRefreshFloor {
		return // fresh enough — paid API, floored refresh
	}
	m.metricsLoading = true
	*cmds = append(*cmds, m.loadMetricsCmd(q))
}

// startConsumerJump resolves the queue's Lambda consumers and, when one is
// found, hands the terminal to the CloudWatch Logs TUI for its log group.
func (m *model) startConsumerJump(cmds *[]tea.Cmd) {
	q, ok := m.selectedQueue()
	if !ok || m.jumpLoading {
		return
	}
	arn := m.queueARN(q)
	if arn == "" {
		m.setToast("Queue ARN unknown — cannot look up consumers")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	m.jumpLoading = true
	key := detailKey(q)
	region := q.Region
	*cmds = append(*cmds, func() tea.Msg {
		consumers, err := m.client.ListConsumers(m.ctx, region, arn)
		return consumersMsg{key: key, region: region, consumers: consumers, err: err}
	})
}

// jumpToLogsCmd suspends this TUI and runs the CloudWatch Logs TUI as a child
// of this same binary, pre-filtered to group in region. tea.ExecProcess hands
// over the terminal and restores it on exit, so quitting the log view returns
// here with state intact.
func (m *model) jumpToLogsCmd(region, group string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return cwJumpDoneMsg{err: err} }
	}
	args := []string{"cw", "--group", group}
	if region != "" {
		args = append(args, "--region", region)
	}
	if m.awsCfg != nil && m.awsCfg.Profile != "" {
		args = append(args, "--profile", m.awsCfg.Profile)
	}
	if cp := ui.ConfigArgPath(m.configPath); cp != "" {
		args = append(args, "--config", cp)
	}
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return cwJumpDoneMsg{err: err}
	})
}

// Commands

func (m *model) loadQueuesCmd() tea.Cmd {
	prefix := m.search.Value()
	return func() tea.Msg {
		slog.Info("Listing SQS queues", "regions", len(m.regions), "prefix", prefix)
		queues, err := m.client.ListQueues(m.ctx, prefix)
		if err != nil {
			slog.Warn("Listing SQS queues failed", "error", err.Error())
		} else {
			slog.Info("Listed SQS queues", "count", len(queues))
		}
		return queuesMsg{queues: queues, err: err}
	}
}

func (m *model) loadDetailCmd(q Queue) tea.Cmd {
	key := detailKey(q)
	return func() tea.Msg {
		slog.Info("Fetching SQS queue detail", "queue", q.Name, "region", q.Region)
		return detailMsg{key: key, detail: m.client.FetchDetail(m.ctx, q.Region, q.URL)}
	}
}

func (m *model) peekCmd(q Queue) tea.Cmd {
	key := detailKey(q)
	return func() tea.Msg {
		// The message bodies themselves are never logged — they may hold
		// secrets or PII; only the count is.
		slog.Info("Peeking SQS messages", "queue", q.Name, "region", q.Region)
		msgs, err := m.client.PeekMessages(m.ctx, q.Region, q.URL)
		if err != nil {
			slog.Warn("SQS peek failed", "queue", q.Name, "error", err.Error())
		} else {
			slog.Info("SQS peek sampled messages", "queue", q.Name, "count", len(msgs))
		}
		return peekMsg{key: key, msgs: msgs, err: err}
	}
}

func (m *model) loadMetricsCmd(q Queue) tea.Cmd {
	key := detailKey(q)
	return func() tea.Msg {
		slog.Info("Fetching SQS queue metrics", "queue", q.Name, "region", q.Region)
		series, err := m.client.FetchQueueMetrics(m.ctx, q.Region, q.Name)
		if err != nil {
			slog.Warn("SQS metrics fetch failed", "queue", q.Name, "error", err.Error())
		}
		return metricsMsg{key: key, series: series, err: err}
	}
}

// buildMessagesTable (re)creates the peek table from the sampled messages.
func (m *model) buildMessagesTable() {
	m.msgTable = table.New(
		table.WithColumns(messageTableColumns()),
		table.WithRows(messageTableRows(m.messages)),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1),
	)
}

// openMessageRecord opens the record overlay for one sampled message.
func (m *model) openMessageRecord(msg types.Message) {
	w := m.recordOverlayWidth()
	h := m.height - 10
	if h < 5 {
		h = 5
	}
	m.recordText = messageRecordText(msg)
	var lines []string
	indent := strings.Repeat(" ", 4)
	for _, line := range strings.Split(m.recordText, "\n") {
		lines = append(lines, wrapLine(sanitizeLine(line), w, indent)...)
	}
	m.recordVP = viewport.New(w, h)
	m.recordVP.SetContent(strings.Join(lines, "\n"))
	m.recordActive = true
}

func (m *model) recordOverlayWidth() int {
	w := m.width - 12
	if w > 110 {
		w = 110
	}
	if w < 30 {
		w = 30
	}
	return w
}

// exportMessages writes the sampled messages to the downloads directory.
func (m *model) exportMessages(cmds *[]tea.Cmd) {
	if len(m.messages) == 0 {
		m.setToast("No messages to export")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	dir, err := downloads.Dir()
	if err != nil {
		m.setToast("Export failed: " + err.Error())
		*cmds = append(*cmds, toastCmd(4*time.Second))
		return
	}
	label := sanitizeFilename(m.peekQueue.Region + "-" + m.peekQueue.Name)
	path := filepath.Join(dir, fmt.Sprintf("sqs-peek-%s-%s.log", label, time.Now().Format("20060102-150405")))
	if err := os.WriteFile(path, []byte(formatMessages(m.messages)), 0644); err != nil {
		m.setToast("Export failed: " + err.Error())
	} else {
		m.setToast("Exported messages to " + path)
	}
	*cmds = append(*cmds, toastCmd(4*time.Second))
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ":", "-")
	return s
}

func toastCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func (m *model) setToast(msg string) {
	m.toast = msg
	m.toastExp = time.Now().Add(3 * time.Second)
}

// PageTitle names the current screen for the terminal window/tab title.
func (m *model) PageTitle() string {
	const base = "SQS Queues"
	q, ok := m.selectedQueue()
	if !ok {
		return base
	}
	if m.view == viewMessages {
		return base + " › " + m.peekQueue.Name + " › messages"
	}
	return base + " › " + q.Name
}
