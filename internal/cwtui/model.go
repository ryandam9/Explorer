package cwtui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/consolelink"
	"github.com/ryandam9/aws_explorer/internal/debugpane"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

type focusArea int

const (
	focusGroups focusArea = iota
	focusStreams
	focusEvents
)

type activeView int

const (
	viewStreams activeView = iota
	viewEvents
)

type model struct {
	ctx        context.Context
	awsCfg     *config.AWSConfig
	regions    []string
	allRegions bool
	configPath string
	appCfg     *config.Config
	client     *CWLogsClient

	// Window dimension
	width  int
	height int

	// Navigation & State
	focus focusArea
	view  activeView

	// Log Groups Panel
	groups            []LogGroup
	filteredGroups    []LogGroup
	selectedGroupIdx  int
	groupSearch       textinput.Model
	groupSearchActive bool
	groupsLoading     bool

	// Log Streams Panel
	streams            []types.LogStream
	filteredStreams    []types.LogStream
	selectedStreamIdx  int
	streamSearch       textinput.Model
	streamSearchActive bool
	streamsLoading     bool

	// Log Events Panel
	events            []types.FilteredLogEvent
	selectedEventIdx  int
	eventSearch       textinput.Model
	eventSearchActive bool
	eventsLoading     bool
	groupLevelSearch  bool // If true, queries entire group instead of specific stream

	// Full log viewer (opened with Enter on an event)
	viewer logViewer

	// Watch Mode (Live tailing)
	watchMode bool

	// showAbout toggles the "what is this page" overlay ("i"), shown over the
	// group/stream browser.
	showAbout bool

	// TUI Utilities
	spinner  spinner.Model
	err      error
	toast    string
	toastExp time.Time

	debug debugpane.Model // "~" live activity overlay
}

// Msg types
type groupsMsg struct {
	groups []LogGroup
	err    error
}

type streamsMsg struct {
	groupName string
	region    string
	streams   []types.LogStream
	err       error
}

type eventsMsg struct {
	events []types.FilteredLogEvent
	err    error
}

type clearToastMsg struct{}
type watchTickMsg struct{}

// NewModel builds the CloudWatch Logs explorer over one or more regions (all
// enabled regions when allRegions is true). groupFilter, streamFilter and
// eventPattern pre-populate the three search inputs (the event pattern is
// applied server-side on the first event query).
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool, configPath string, appCfg *config.Config, groupFilter, streamFilter, eventPattern string) (tea.Model, error) {
	client, err := NewCWLogsClient(ctx, awsCfg, regions, allRegions)
	if err != nil {
		return nil, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	gSearch := textinput.New()
	gSearch.Placeholder = "Filter log groups…"
	gSearch.Width = 30

	sSearch := textinput.New()
	sSearch.Placeholder = "Filter log streams…"
	sSearch.Width = 30

	eSearch := textinput.New()
	eSearch.Placeholder = "CloudWatch pattern (e.g. ERROR, panic)…"
	eSearch.Width = 40

	vSearch := textinput.New()
	vSearch.Placeholder = "Find in log…"
	vSearch.Width = 40

	vGrep := textinput.New()
	vGrep.Placeholder = "grep regex (smart case; e.g. error|timeout)…"
	vGrep.Width = 40

	gSearch.SetValue(groupFilter)
	sSearch.SetValue(streamFilter)
	eSearch.SetValue(eventPattern)

	m := &model{
		ctx:          ctx,
		awsCfg:       awsCfg,
		regions:      client.Regions(),
		allRegions:   allRegions,
		configPath:   configPath,
		appCfg:       appCfg,
		client:       client,
		focus:        focusGroups,
		view:         viewStreams,
		spinner:      s,
		groupSearch:  gSearch,
		streamSearch: sSearch,
		eventSearch:  eSearch,
		viewer:       logViewer{search: vSearch, grepInput: vGrep},
	}

	return m, nil
}

func (m *model) Init() tea.Cmd {
	m.groupsLoading = true
	return tea.Batch(
		m.spinner.Tick,
		m.loadGroupsCmd(""),
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// While the debug overlay is open, it consumes key/mouse input; every other
	// message falls through so loads keep streaming underneath.
	if m.debug.Visible() {
		if m.debug.HandleInput(msg) {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.viewer.active {
			m.viewer.rebuild(m.viewerWrapWidth())
		}

	case spinner.TickMsg:
		m.debug.Refresh()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case clearToastMsg:
		m.toast = ""

	case watchTickMsg:
		if m.watchMode && m.view == viewEvents {
			cmds = append(cmds, m.loadEventsCmd())
			cmds = append(cmds, m.watchTickCmd())
		}

	case groupsMsg:
		m.groupsLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.groups = msg.groups
			m.filterGroups()
		}

	case streamsMsg:
		// Drop responses for groups no longer selected (eager loads while
		// arrowing through the group list race each other). The same group
		// name can exist in several regions, so match on both.
		sel, ok := m.selectedGroup()
		if !ok ||
			msg.groupName != aws.ToString(sel.LogGroupName) ||
			msg.region != sel.Region {
			return m, tea.Batch(cmds...)
		}
		m.streamsLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.streams = msg.streams
			m.filterStreams()
		}

	case eventsMsg:
		m.eventsLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.events = msg.events
			// Keep selected index in bounds
			if m.selectedEventIdx >= len(m.events) {
				m.selectedEventIdx = max(0, len(m.events)-1)
			}
		}

	case viewerEventsMsg:
		m.handleViewerEvents(msg, &cmds)

	case viewerTickMsg:
		// Stream new events while the viewer stays open on the same target.
		if m.viewer.active && msg.key == m.viewer.key {
			cmds = append(cmds, m.loadViewerEventsCmd(false), m.viewerTickCmd())
		}

	case tea.KeyMsg:
		// Error screen: Enter/Esc clears the error and retries, q quits.
		if m.err != nil {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.err = nil
				if len(m.groups) == 0 {
					m.groupsLoading = true
					cmds = append(cmds, m.loadGroupsCmd(""))
				}
			}
			return m, tea.Batch(cmds...)
		}

		// About overlay: static text, any key closes it. Checked before the
		// viewer/search guards since it floats over the browser only.
		if m.showAbout {
			m.showAbout = false
			return m, tea.Batch(cmds...)
		}

		// Full log viewer captures all keys while open
		if m.viewer.active {
			m.handleViewerKeys(msg, &cmds)
			return m, tea.Batch(cmds...)
		}

		// If search inputs are active, direct keys to them
		if m.groupSearchActive && m.focus == focusGroups {
			switch msg.String() {
			case "enter":
				m.groupSearchActive = false
				m.filterGroups()
			case "esc":
				m.groupSearchActive = false
				m.groupSearch.SetValue("")
				m.filterGroups()
			default:
				var cmd tea.Cmd
				m.groupSearch, cmd = m.groupSearch.Update(msg)
				cmds = append(cmds, cmd)
				m.filterGroups()
			}
			return m, tea.Batch(cmds...)
		}

		if m.streamSearchActive && m.focus == focusStreams {
			switch msg.String() {
			case "enter":
				m.streamSearchActive = false
				m.filterStreams()
			case "esc":
				m.streamSearchActive = false
				m.streamSearch.SetValue("")
				m.filterStreams()
			default:
				var cmd tea.Cmd
				m.streamSearch, cmd = m.streamSearch.Update(msg)
				cmds = append(cmds, cmd)
				m.filterStreams()
			}
			return m, tea.Batch(cmds...)
		}

		if m.eventSearchActive && m.focus == focusEvents {
			switch msg.String() {
			case "enter":
				m.eventSearchActive = false
				m.eventsLoading = true
				cmds = append(cmds, m.loadEventsCmd())
			case "esc":
				m.eventSearchActive = false
				m.eventSearch.SetValue("")
				m.eventsLoading = true
				cmds = append(cmds, m.loadEventsCmd())
			default:
				var cmd tea.Cmd
				m.eventSearch, cmd = m.eventSearch.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Global navigation
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.cycleFocus(true)
		case "shift+tab":
			m.cycleFocus(false)

		case "up", "k":
			cmds = append(cmds, m.navigateList(-1))
		case "down", "j":
			cmds = append(cmds, m.navigateList(1))

		case "/":
			m.activateSearch()

		case "enter":
			m.handleSelection(&cmds)

		case "esc", "backspace":
			m.handleBack(&cmds)

		case "y":
			m.handleCopy(&cmds)

		case "o":
			// Console URL for the selected log group: copy, and open in a
			// browser when the session is local.
			if grp, ok := m.selectedGroup(); ok && grp.LogGroupName != nil {
				url := consolelink.LogGroupURL(grp.Region, *grp.LogGroupName)
				_ = clipboard.WriteAll(url)
				if consolelink.CanOpenBrowser() && consolelink.Open(url) == nil {
					m.setToast("Opened in browser · copied console URL")
				} else {
					m.setToast("Copied console URL")
				}
				cmds = append(cmds, toastCmd(3*time.Second))
			}

		case "s":
			m.handleExport(&cmds)

		case ui.KeyDebug:
			m.debug.Open(m.width, m.height)

		case ui.KeyAbout:
			m.showAbout = true

		case "W":
			if m.view == viewEvents {
				m.watchMode = !m.watchMode
				if m.watchMode {
					m.setToast("Live tail watch mode active")
					cmds = append(cmds, toastCmd(3*time.Second), m.watchTickCmd())
				} else {
					m.setToast("Live tail watch mode deactivated")
					cmds = append(cmds, toastCmd(3*time.Second))
				}
			}

		case "G":
			if len(m.filteredGroups) > 0 {
				m.groupLevelSearch = !m.groupLevelSearch
				m.view = viewEvents
				m.focus = focusEvents
				m.eventsLoading = true
				m.watchMode = false
				cmds = append(cmds, m.loadEventsCmd())
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) cycleFocus(forward bool) {
	if m.view == viewStreams {
		if forward {
			if m.focus == focusGroups {
				m.focus = focusStreams
			} else {
				m.focus = focusGroups
			}
		} else {
			if m.focus == focusStreams {
				m.focus = focusGroups
			} else {
				m.focus = focusStreams
			}
		}
	} else { // viewEvents
		if forward {
			if m.focus == focusGroups {
				m.focus = focusEvents
			} else {
				m.focus = focusGroups
			}
		} else {
			if m.focus == focusEvents {
				m.focus = focusGroups
			} else {
				m.focus = focusEvents
			}
		}
	}
}

func (m *model) navigateList(dir int) tea.Cmd {
	switch m.focus {
	case focusGroups:
		if len(m.filteredGroups) == 0 {
			return nil
		}
		m.selectedGroupIdx = (m.selectedGroupIdx + dir + len(m.filteredGroups)) % len(m.filteredGroups)
		// Eagerly query streams for the newly selected group; the streamsMsg
		// handler drops responses for groups that are no longer selected.
		m.streamsLoading = true
		m.streams = nil
		m.filteredStreams = nil
		m.selectedStreamIdx = 0
		m.events = nil
		m.selectedEventIdx = 0
		m.view = viewStreams
		return m.loadStreamsCmd()
	case focusStreams:
		if len(m.filteredStreams) == 0 {
			return nil
		}
		m.selectedStreamIdx = (m.selectedStreamIdx + dir + len(m.filteredStreams)) % len(m.filteredStreams)
	case focusEvents:
		if len(m.events) == 0 {
			return nil
		}
		m.selectedEventIdx = (m.selectedEventIdx + dir + len(m.events)) % len(m.events)
	}
	return nil
}

func (m *model) activateSearch() {
	switch m.focus {
	case focusGroups:
		m.groupSearchActive = true
		m.groupSearch.Focus()
	case focusStreams:
		m.streamSearchActive = true
		m.streamSearch.Focus()
	case focusEvents:
		m.eventSearchActive = true
		m.eventSearch.Focus()
	}
}

func (m *model) handleSelection(cmds *[]tea.Cmd) {
	switch m.focus {
	case focusGroups:
		if len(m.filteredGroups) == 0 {
			return
		}
		m.streamsLoading = true
		m.view = viewStreams
		m.focus = focusStreams
		*cmds = append(*cmds, m.loadStreamsCmd())
	case focusStreams:
		if len(m.filteredStreams) == 0 {
			return
		}
		m.eventsLoading = true
		m.view = viewEvents
		m.focus = focusEvents
		m.groupLevelSearch = false
		m.watchMode = false
		*cmds = append(*cmds, m.loadEventsCmd())
	case focusEvents:
		if len(m.events) == 0 {
			return
		}
		m.openViewer(cmds)
	}
}

func (m *model) handleBack(cmds *[]tea.Cmd) {
	m.watchMode = false
	if m.focus == focusEvents {
		if m.groupLevelSearch {
			m.groupLevelSearch = false
			m.view = viewStreams
			m.focus = focusGroups
		} else {
			m.view = viewStreams
			m.focus = focusStreams
		}
	} else if m.focus == focusStreams {
		m.view = viewStreams
		m.focus = focusGroups
	}
}

func (m *model) handleCopy(cmds *[]tea.Cmd) {
	if m.focus == focusEvents && len(m.events) > 0 {
		msg := aws.ToString(m.events[m.selectedEventIdx].Message)
		_ = clipboard.WriteAll(msg)
		m.setToast("Copied log event to clipboard")
		*cmds = append(*cmds, toastCmd(3*time.Second))
	}
}

func (m *model) handleExport(cmds *[]tea.Cmd) {
	grpName := "unknown-group"
	if grp, ok := m.selectedGroup(); ok {
		grpName = sanitizeFilename(grp.Region + "-" + aws.ToString(grp.LogGroupName))
	}
	streamName := "all_streams"
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		streamName = sanitizeFilename(aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName))
	}

	path, err := exportEvents(m.events, grpName, streamName)
	if err != nil {
		m.setToast("Export failed: " + err.Error())
	} else {
		m.setToast("Exported logs to " + path)
	}
	*cmds = append(*cmds, toastCmd(4*time.Second))
}

// formatEvents renders events as timestamped plain-text lines, the shared
// format for clipboard copies and file exports.
func formatEvents(events []types.FilteredLogEvent) string {
	var sb strings.Builder
	for _, ev := range events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		sb.WriteString(fmt.Sprintf("[%s] %s\n", t.Format("2006-01-02 15:04:05.000"), aws.ToString(ev.Message)))
	}
	return sb.String()
}

// exportEvents writes events to ~/.aws_explorer/logs and returns the path.
func exportEvents(events []types.FilteredLogEvent, grpLabel, streamLabel string) (string, error) {
	if len(events) == 0 {
		return "", fmt.Errorf("no events to export")
	}
	return exportText(formatEvents(events), grpLabel, streamLabel)
}

// exportText writes already-rendered log text (e.g. the grep-filtered lines)
// to ~/.aws_explorer/logs and returns the path.
func exportText(content, grpLabel, streamLabel string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("no log lines to export")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home: %w", err)
	}
	dir := filepath.Join(home, ".aws_explorer", "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("cw-logs-%s-%s-%s.log", grpLabel, streamLabel, time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func (m *model) filterGroups() {
	term := strings.ToLower(m.groupSearch.Value())
	if term == "" {
		m.filteredGroups = m.groups
	} else {
		var list []LogGroup
		for _, g := range m.groups {
			if strings.Contains(strings.ToLower(aws.ToString(g.LogGroupName)), term) ||
				strings.Contains(g.Region, term) {
				list = append(list, g)
			}
		}
		m.filteredGroups = list
	}

	if m.selectedGroupIdx >= len(m.filteredGroups) {
		m.selectedGroupIdx = max(0, len(m.filteredGroups)-1)
	}
}

func (m *model) filterStreams() {
	term := strings.ToLower(m.streamSearch.Value())
	if term == "" {
		m.filteredStreams = m.streams
	} else {
		var list []types.LogStream
		for _, s := range m.streams {
			if strings.Contains(strings.ToLower(aws.ToString(s.LogStreamName)), term) {
				list = append(list, s)
			}
		}
		m.filteredStreams = list
	}

	if m.selectedStreamIdx >= len(m.filteredStreams) {
		m.selectedStreamIdx = max(0, len(m.filteredStreams)-1)
	}
}

func (m *model) loadGroupsCmd(prefix string) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Listing CloudWatch log groups", "prefix", prefix)
		groups, err := m.client.ListLogGroups(m.ctx, prefix)
		if err != nil {
			slog.Warn("Listing log groups failed", "error", err.Error())
		} else {
			slog.Info("Listed CloudWatch log groups", "count", len(groups))
		}
		return groupsMsg{groups: groups, err: err}
	}
}

// selectedGroup returns the highlighted log group, or false when the filtered
// list is empty.
func (m *model) selectedGroup() (LogGroup, bool) {
	if len(m.filteredGroups) == 0 {
		return LogGroup{}, false
	}
	return m.filteredGroups[m.selectedGroupIdx], true
}

// PageTitle names the current screen for the terminal window/tab title, so
// every page has a unique, shareable name (see ui.WithWindowTitle).
func (m *model) PageTitle() string {
	const base = "CloudWatch Logs"
	if m.viewer.active {
		return base + " › " + m.viewer.title
	}
	grp, ok := m.selectedGroup()
	if !ok || m.focus == focusGroups {
		return base + " › Log groups"
	}
	title := base + " › " + aws.ToString(grp.LogGroupName)
	if m.focus == focusEvents && !m.groupLevelSearch &&
		m.selectedStreamIdx < len(m.filteredStreams) {
		title += " › " + aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName)
	}
	return title
}

func (m *model) loadStreamsCmd() tea.Cmd {
	grp, ok := m.selectedGroup()
	if !ok {
		return nil
	}
	grpName := aws.ToString(grp.LogGroupName)
	region := grp.Region
	return func() tea.Msg {
		slog.Info("Listing log streams", "group", grpName, "region", region)
		streams, err := m.client.ListLogStreams(m.ctx, region, grpName, "")
		if err != nil {
			slog.Warn("Listing log streams failed", "group", grpName, "region", region, "error", err.Error())
		}
		return streamsMsg{groupName: grpName, region: region, streams: streams, err: err}
	}
}

func (m *model) loadEventsCmd() tea.Cmd {
	grp, ok := m.selectedGroup()
	if !ok {
		return nil
	}
	grpName := aws.ToString(grp.LogGroupName)
	region := grp.Region
	var streamName string
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		streamName = aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName)
	}
	pattern := m.eventSearch.Value()

	return func() tea.Msg {
		slog.Info("Fetching log events", "group", grpName, "region", region, "stream", streamName, "pattern", pattern)
		events, err := m.client.GetLogEvents(m.ctx, region, grpName, streamName, pattern, 100)
		if err != nil {
			slog.Warn("Fetching log events failed", "group", grpName, "region", region, "error", err.Error())
		}
		return eventsMsg{events: events, err: err}
	}
}

func (m *model) watchTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
}

func (m *model) View() string {
	if m.err != nil {
		return m.debug.Overlay(m.renderErrorView(), m.width, m.height)
	}

	if m.viewer.active {
		return m.debug.Overlay(m.applyToast(m.renderViewer()), m.width, m.height)
	}

	var sb strings.Builder

	// Spotlight the active region scope at the top when not in all-regions
	// mode, so a single-region session can't be mistaken for the whole account.
	if badge := ui.RegionBadge(m.regions, m.allRegions); badge != "" {
		sb.WriteString(badge + "\n")
	}

	// Main Layout: Sidebar & Content Panel
	sidebarW := 42
	contentW := m.width - sidebarW - 4
	if contentW < 20 {
		contentW = 20
	}

	sidebar := m.renderSidebar(sidebarW)
	var content string

	if m.view == viewStreams {
		content = m.renderStreamsPanel(contentW)
	} else {
		content = m.renderEventsPanel(contentW)
	}

	mainLayout := lipgloss.JoinHorizontal(
		lipgloss.Top,
		sidebar,
		lipgloss.NewStyle().Width(2).Render(" "),
		content,
	)

	sb.WriteString(mainLayout + "\n")

	// Status bar
	regionLabel := "all (" + fmt.Sprintf("%d", len(m.regions)) + " regions)"
	if len(m.regions) == 1 {
		regionLabel = m.regions[0]
	}
	statusText := fmt.Sprintf("Region: %s  ·  Groups: %d  ·  Streams: %d  ·  Events: %d",
		regionLabel, len(m.filteredGroups), len(m.filteredStreams), len(m.events))
	if m.watchMode {
		statusText += "  ·  [WATCH ACTIVE]"
	}

	sb.WriteString(ui.StatusBar(m.width, statusText, m.getHelpHints()))

	frame := m.applyToast(sb.String())
	if m.showAbout {
		frame = ui.OverlayCenterBlank(ui.AboutView("About — CloudWatch Logs", cwAboutText, ui.AboutWidth(m.width)), m.width, m.height)
	}
	return m.debug.Overlay(frame, m.width, m.height)
}

// cwAboutText explains what the CloudWatch Logs TUI is for, shown in the About
// overlay ("i").
const cwAboutText = "This is the CloudWatch Logs explorer. The sidebar lists log groups; pick " +
	"one to see its streams, and open a stream (or the whole group) into a " +
	"full-screen, live-tailing log page.\n\n" +
	"In the log page you can search within the log (/), grep-filter lines (&), " +
	"pretty-print embedded JSON (J), follow new events (f), and copy or export " +
	"what you see. Lines are tinted by severity so errors stand out.\n\n" +
	"You often arrive here by pressing L on a resource in the Summary or VPC " +
	"explorer, which pre-filters to that resource's log group. The status bar " +
	"shows the keys usable right now."

// applyToast paints the active toast notification over the rendered view.
func (m *model) applyToast(rendered string) string {
	if m.toast == "" || !time.Now().Before(m.toastExp) {
		return rendered
	}
	toastRendered := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorSuccess())).
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Padding(0, 2).
		Bold(true).
		Render("✓ " + m.toast)
	lines := strings.Split(rendered, "\n")
	if len(lines) >= 2 {
		tl := lipgloss.PlaceHorizontal(m.width, lipgloss.Right, toastRendered)
		lines[1] = tl
		rendered = strings.Join(lines, "\n")
	}
	return rendered
}

func (m *model) renderSidebar(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true)

	b.WriteString(headingStyle.Render(" Log groups") + "\n")

	if m.groupSearchActive {
		b.WriteString(" " + m.groupSearch.View() + "\n")
	} else {
		b.WriteString("  (Press / to filter)\n")
	}

	b.WriteString("\n")

	if m.groupsLoading {
		b.WriteString(fmt.Sprintf("  %s Loading log groups…\n", m.spinner.View()))
	} else if len(m.filteredGroups) == 0 {
		b.WriteString("  No log groups found.\n")
	} else {
		// Scrolled listing window
		visibleHeight := m.height - 8
		if visibleHeight < 5 {
			visibleHeight = 5
		}
		start, end := getVisibleRange(m.selectedGroupIdx, len(m.filteredGroups), visibleHeight)

		// Single region: show stored size. Multiple regions: the region
		// column matters more than size in the narrow sidebar.
		multiRegion := len(m.regions) > 1
		metaW := 6
		if multiRegion {
			metaW = 14
		}

		for i := start; i < end; i++ {
			g := m.filteredGroups[i]
			name := aws.ToString(g.LogGroupName)
			// Truncate name to fit
			maxNameW := width - metaW - 5
			if len(name) > maxNameW {
				name = name[len(name)-maxNameW+3:] // keep the end as it has the function/service names
				name = "..." + name
			}

			meta := humanize.Bytes(uint64(aws.ToInt64(g.StoredBytes)))
			if multiRegion {
				meta = g.Region
			}
			item := fmt.Sprintf(" %-*s %*s", maxNameW, name, metaW, meta)

			if i == m.selectedGroupIdx && m.focus == focusGroups {
				b.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(ui.ColorHighlight())).
					Foreground(lipgloss.Color(ui.ColorHighlightText())).
					Render("> "+item) + "\n")
			} else if i == m.selectedGroupIdx {
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color(ui.ColorHeading())).
					Render("• "+item) + "\n")
			} else {
				b.WriteString("  " + item + "\n")
			}
		}
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.getBorderColor(focusGroups))).
		Width(width).
		Height(m.height - 4)

	return borderStyle.Render(b.String())
}

func (m *model) renderStreamsPanel(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true)

	if grp, ok := m.selectedGroup(); ok {
		b.WriteString(headingStyle.Render(" Log streams: "+aws.ToString(grp.LogGroupName)+" ["+grp.Region+"]") + "\n")
	} else {
		b.WriteString(headingStyle.Render(" Log streams") + "\n")
	}

	if m.streamSearchActive {
		b.WriteString(" " + m.streamSearch.View() + "\n")
	} else {
		b.WriteString("  (Press / to filter streams)\n")
	}

	b.WriteString("\n")

	if m.streamsLoading {
		b.WriteString(fmt.Sprintf("  %s Loading log streams…\n", m.spinner.View()))
	} else if len(m.filteredStreams) == 0 {
		b.WriteString("  No log streams found.\n")
	} else {
		visibleHeight := m.height - 8
		if visibleHeight < 5 {
			visibleHeight = 5
		}
		start, end := getVisibleRange(m.selectedStreamIdx, len(m.filteredStreams), visibleHeight)

		for i := start; i < end; i++ {
			s := m.filteredStreams[i]
			name := aws.ToString(s.LogStreamName)
			if len(name) > width-25 {
				name = name[:width-28] + "..."
			}

			lastTime := time.Unix(0, aws.ToInt64(s.LastEventTimestamp)*int64(time.Millisecond))
			timeStr := lastTime.Format("2006-01-02 15:04:05")
			if aws.ToInt64(s.LastEventTimestamp) == 0 {
				timeStr = "No events"
			}

			item := fmt.Sprintf(" %-*s  %s", width-25, name, timeStr)

			if i == m.selectedStreamIdx && m.focus == focusStreams {
				b.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(ui.ColorHighlight())).
					Foreground(lipgloss.Color(ui.ColorHighlightText())).
					Render("> "+item) + "\n")
			} else {
				b.WriteString("  " + item + "\n")
			}
		}
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.getBorderColor(focusStreams))).
		Width(width).
		Height(m.height - 4)

	return borderStyle.Render(b.String())
}

func (m *model) renderEventsPanel(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true)

	grpName, grpRegion := "", ""
	if grp, ok := m.selectedGroup(); ok {
		grpName = aws.ToString(grp.LogGroupName)
		grpRegion = " [" + grp.Region + "]"
	}
	title := " Events: " + grpName + grpRegion
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		title = " Stream events: " + aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName) + grpRegion
	}
	if len(title) > width-10 {
		title = title[:width-13] + "..."
	}
	b.WriteString(headingStyle.Render(title) + "\n")

	if m.eventSearchActive {
		b.WriteString(" Query pattern: " + m.eventSearch.View() + "\n")
	} else {
		b.WriteString("  Pattern filter: " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(m.eventSearch.Value()) + "  (Press / to set serverside query pattern)\n")
	}

	b.WriteString("\n")

	if m.eventsLoading {
		b.WriteString(fmt.Sprintf("  %s Filtering and loading log events…\n", m.spinner.View()))
	} else if len(m.events) == 0 {
		b.WriteString("  No matching log events found in this window.\n")
	} else {
		visibleHeight := m.height - 8
		if visibleHeight < 5 {
			visibleHeight = 5
		}
		start, end := getVisibleRange(m.selectedEventIdx, len(m.events), visibleHeight)

		for i := start; i < end; i++ {
			ev := m.events[i]
			t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
			timeStr := t.Format("15:04:05")

			msg := aws.ToString(ev.Message)
			msg = strings.ReplaceAll(msg, "\n", " ") // flatten newlines for inline view
			// Soft truncation to fit panel width
			maxMsgW := width - 15
			if len(msg) > maxMsgW {
				msg = msg[:maxMsgW] + "..."
			}

			item := fmt.Sprintf("[%s] %s", timeStr, msg)

			if i == m.selectedEventIdx && m.focus == focusEvents {
				b.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(ui.ColorHighlight())).
					Foreground(lipgloss.Color(ui.ColorHighlightText())).
					Render("> "+item) + "\n")
			} else {
				// Highlight errors/warnings in the stream
				style := lipgloss.NewStyle()
				lowMsg := strings.ToLower(msg)
				if strings.Contains(lowMsg, "error") || strings.Contains(lowMsg, "fail") || strings.Contains(lowMsg, "panic") {
					style = style.Foreground(lipgloss.Color(ui.ColorError()))
				} else if strings.Contains(lowMsg, "warn") {
					style = style.Foreground(lipgloss.Color(ui.ColorWarning()))
				} else {
					style = style.Foreground(lipgloss.Color(ui.ColorText()))
				}
				b.WriteString("  " + style.Render(item) + "\n")
			}
		}
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.getBorderColor(focusEvents))).
		Width(width).
		Height(m.height - 4)

	return borderStyle.Render(b.String())
}

func (m *model) renderErrorView() string {
	var b strings.Builder
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).Render("  CloudWatch Logs explorer exception") + "\n\n")
	b.WriteString(fmt.Sprintf("  An error occurred: %v\n\n", m.err))
	b.WriteString("  Shortcuts:\n")
	b.WriteString("    Enter, Esc  - Attempt to return or retry\n")
	b.WriteString("    q, Ctrl+C   - Quit\n")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorError())).
		Width(m.width - 4).
		Height(m.height - 4)

	return borderStyle.Render(b.String())
}

func (m *model) getBorderColor(area focusArea) string {
	if m.focus == area {
		return ui.ColorBorderFocus()
	}
	return ui.ColorBorder()
}

func (m *model) getHelpHints() []ui.KeyHint {
	var hints []ui.KeyHint

	if m.viewer.active {
		if m.viewer.searchActive {
			return []ui.KeyHint{
				ui.H("Enter", "jump to match"),
				ui.H("Esc", "cancel"),
			}
		}
		if m.viewer.grepActive {
			return []ui.KeyHint{
				ui.H("type", "regex filter"),
				ui.H("Enter", "keep filter"),
				ui.H("Esc", "clear"),
			}
		}
		copyHint, exportHint := "copy all", "export"
		if m.viewer.grepRe != nil {
			copyHint, exportHint = "copy matches", "export matches"
		}
		return []ui.KeyHint{
			ui.H("↑/↓", "scroll"),
			ui.H("PgUp/PgDn", "page"),
			ui.H("/", "find"),
			ui.H("&", "grep"),
			ui.H("n/N", "next/prev"),
			ui.H("G", "tail"),
			ui.H("f", "follow"),
			ui.H("J", "format json"),
			ui.H("y", copyHint),
			ui.H("s", exportHint),
			ui.H("Esc", "close"),
		}
	}

	switch m.focus {
	case focusGroups:
		hints = append(hints,
			ui.H("↑/↓", "groups"),
			ui.H("Enter", "select"),
			ui.H("/", "filter"),
			ui.H("G", "search entire group"),
			ui.H("o", "console"),
		)
	case focusStreams:
		hints = append(hints,
			ui.H("↑/↓", "streams"),
			ui.H("Enter", "events"),
			ui.H("/", "filter"),
			ui.H("Esc", "back"),
		)
	case focusEvents:
		hints = append(hints,
			ui.H("↑/↓", "events"),
			ui.H("Enter", "full log"),
			ui.H("/", "pattern"),
			ui.H("W", "tail watch"),
			ui.H("y", "copy"),
			ui.H("s", "export"),
			ui.H("Esc", "back"),
		)
	}

	hints = append(hints,
		ui.H("Tab", "panel"),
		ui.H("~", "debug"),
		ui.H("i", "about"),
		ui.H("q", "quit"),
	)
	return hints
}

// Helper math and slice functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func getVisibleRange(current, total, maxVisible int) (int, int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start := current - half
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
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
