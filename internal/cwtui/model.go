package cwtui

import (
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/ui"
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
	region     string
	configPath string
	appCfg     *config.Config
	client     *CWLogsClient

	// Window dimension
	width  int
	height int

	// Navigation & State
	focus      focusArea
	view       activeView
	showDetail bool

	// Log Groups Panel
	groups            []types.LogGroup
	filteredGroups    []types.LogGroup
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

	// Expand Event Modal
	selectedEvent        *types.FilteredLogEvent
	expandedScrollOffset int

	// Watch Mode (Live tailing)
	watchMode bool

	// TUI Utilities
	spinner  spinner.Model
	err      error
	toast    string
	toastExp time.Time
}

// Msg types
type groupsMsg struct {
	groups []types.LogGroup
	err    error
}

type streamsMsg struct {
	groupName string
	streams   []types.LogStream
	err       error
}

type eventsMsg struct {
	events []types.FilteredLogEvent
	err    error
}

type clearToastMsg struct{}
type watchTickMsg struct{}

// NewModel builds the CloudWatch Logs explorer. groupFilter, streamFilter and
// eventPattern pre-populate the three search inputs (the event pattern is
// applied server-side on the first event query).
func NewModel(ctx context.Context, awsCfg *config.AWSConfig, region string, configPath string, appCfg *config.Config, groupFilter, streamFilter, eventPattern string) (tea.Model, error) {
	client, err := NewCWLogsClient(ctx, awsCfg, region)
	if err != nil {
		return nil, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	gSearch := textinput.New()
	gSearch.Placeholder = "Filter log groups..."
	gSearch.Width = 30

	sSearch := textinput.New()
	sSearch.Placeholder = "Filter log streams..."
	sSearch.Width = 30

	eSearch := textinput.New()
	eSearch.Placeholder = "CloudWatch pattern (e.g. ERROR, panic)..."
	eSearch.Width = 40

	gSearch.SetValue(groupFilter)
	sSearch.SetValue(streamFilter)
	eSearch.SetValue(eventPattern)

	m := &model{
		ctx:          ctx,
		awsCfg:       awsCfg,
		region:       region,
		configPath:   configPath,
		appCfg:       appCfg,
		client:       client,
		focus:        focusGroups,
		view:         viewStreams,
		spinner:      s,
		groupSearch:  gSearch,
		streamSearch: sSearch,
		eventSearch:  eSearch,
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

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
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
		// arrowing through the group list race each other).
		if len(m.filteredGroups) == 0 ||
			msg.groupName != aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName) {
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

		// Detail overlay keys
		if m.showDetail {
			switch msg.String() {
			case "esc", "q", "enter":
				m.showDetail = false
			case "up", "k":
				if m.expandedScrollOffset > 0 {
					m.expandedScrollOffset--
				}
			case "down", "j":
				m.expandedScrollOffset++
			case "y":
				if m.selectedEvent != nil {
					_ = clipboard.WriteAll(aws.ToString(m.selectedEvent.Message))
					m.setToast("Copied event message to clipboard")
					cmds = append(cmds, toastCmd(3*time.Second))
				}
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

		case "s":
			m.handleExport(&cmds)

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
		m.selectedEvent = &m.events[m.selectedEventIdx]
		m.showDetail = true
		m.expandedScrollOffset = 0
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
	if len(m.events) == 0 {
		m.setToast("No events to export")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		m.setToast("Failed to find home: " + err.Error())
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	dir := filepath.Join(home, ".aws_explorer", "logs")
	_ = os.MkdirAll(dir, 0755)

	grpName := "unknown-group"
	if len(m.filteredGroups) > 0 {
		grpName = sanitizeFilename(aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName))
	}
	streamName := "all_streams"
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		streamName = sanitizeFilename(aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName))
	}
	filename := fmt.Sprintf("cw-logs-%s-%s-%s.log", grpName, streamName, time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, filename)

	var sb strings.Builder
	for _, ev := range m.events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		sb.WriteString(fmt.Sprintf("[%s] %s\n", t.Format("2006-01-02 15:04:05.000"), aws.ToString(ev.Message)))
	}

	err = os.WriteFile(path, []byte(sb.String()), 0644)
	if err != nil {
		m.setToast("Export failed: " + err.Error())
	} else {
		m.setToast("Exported logs to " + path)
	}
	*cmds = append(*cmds, toastCmd(4*time.Second))
}

func (m *model) filterGroups() {
	term := strings.ToLower(m.groupSearch.Value())
	if term == "" {
		m.filteredGroups = m.groups
	} else {
		var list []types.LogGroup
		for _, g := range m.groups {
			if strings.Contains(strings.ToLower(aws.ToString(g.LogGroupName)), term) {
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
		groups, err := m.client.ListLogGroups(m.ctx, prefix)
		return groupsMsg{groups: groups, err: err}
	}
}

func (m *model) loadStreamsCmd() tea.Cmd {
	if len(m.filteredGroups) == 0 {
		return nil
	}
	grpName := aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName)
	return func() tea.Msg {
		streams, err := m.client.ListLogStreams(m.ctx, grpName, "")
		return streamsMsg{groupName: grpName, streams: streams, err: err}
	}
}

func (m *model) loadEventsCmd() tea.Cmd {
	if len(m.filteredGroups) == 0 {
		return nil
	}
	grpName := aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName)
	var streamName string
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		streamName = aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName)
	}
	pattern := m.eventSearch.Value()

	return func() tea.Msg {
		events, err := m.client.GetLogEvents(m.ctx, grpName, streamName, pattern, 100)
		return eventsMsg{events: events, err: err}
	}
}

func (m *model) watchTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
}

func (m *model) View() string {
	if m.err != nil {
		return m.renderErrorView()
	}

	var sb strings.Builder

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
	statusText := fmt.Sprintf("Region: %s  ·  Groups: %d  ·  Streams: %d  ·  Events: %d",
		m.region, len(m.filteredGroups), len(m.filteredStreams), len(m.events))
	if m.watchMode {
		statusText += "  ·  [WATCH ACTIVE]"
	}

	sb.WriteString(ui.StatusBar(m.width, statusText, m.getHelpHints()))

	// Modals/Overlays
	rendered := sb.String()
	if m.showDetail {
		rendered = m.overlayDetail(rendered)
	}

	if m.toast != "" && time.Now().Before(m.toastExp) {
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
	}

	return rendered
}

func (m *model) renderSidebar(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true)

	b.WriteString(headingStyle.Render(" LOG GROUPS") + "\n")

	if m.groupSearchActive {
		b.WriteString(" " + m.groupSearch.View() + "\n")
	} else {
		b.WriteString("  (Press / to filter)\n")
	}

	b.WriteString("\n")

	if m.groupsLoading {
		b.WriteString(fmt.Sprintf("  %s Loading log groups...\n", m.spinner.View()))
	} else if len(m.filteredGroups) == 0 {
		b.WriteString("  No log groups found.\n")
	} else {
		// Scrolled listing window
		visibleHeight := m.height - 8
		if visibleHeight < 5 {
			visibleHeight = 5
		}
		start, end := getVisibleRange(m.selectedGroupIdx, len(m.filteredGroups), visibleHeight)

		for i := start; i < end; i++ {
			g := m.filteredGroups[i]
			name := aws.ToString(g.LogGroupName)
			// Truncate name to fit
			maxNameW := width - 18
			if len(name) > maxNameW {
				name = name[len(name)-maxNameW:] // keep the end as it has the function/service names
				name = "..." + name
			}

			sizeStr := humanize.Bytes(uint64(aws.ToInt64(g.StoredBytes)))
			item := fmt.Sprintf(" %-30s %6s", name, sizeStr)

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

	if len(m.filteredGroups) > 0 {
		b.WriteString(headingStyle.Render(" LOG STREAMS: "+aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName)) + "\n")
	} else {
		b.WriteString(headingStyle.Render(" LOG STREAMS") + "\n")
	}

	if m.streamSearchActive {
		b.WriteString(" " + m.streamSearch.View() + "\n")
	} else {
		b.WriteString("  (Press / to filter streams)\n")
	}

	b.WriteString("\n")

	if m.streamsLoading {
		b.WriteString(fmt.Sprintf("  %s Loading log streams...\n", m.spinner.View()))
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

	grpName := ""
	if len(m.filteredGroups) > 0 {
		grpName = aws.ToString(m.filteredGroups[m.selectedGroupIdx].LogGroupName)
	}
	title := " EVENTS: " + grpName
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		title = " STREAM EVENTS: " + aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName)
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
		b.WriteString(fmt.Sprintf("  %s Filtering and loading log events...\n", m.spinner.View()))
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
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).Render("  CLOUDWATCH LOGS EXPLORER EXCEPTION") + "\n\n")
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

func (m *model) overlayDetail(bg string) string {
	if m.selectedEvent == nil {
		return bg
	}

	var b strings.Builder
	ev := m.selectedEvent
	t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))

	headingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	b.WriteString(headingStyle.Render("LOG EVENT DETAILS") + "\n")
	b.WriteString(metaStyle.Render("Timestamp : "+t.Format("2006-01-02 15:04:05.000 MST")) + "\n")
	b.WriteString(metaStyle.Render("Stream    : "+aws.ToString(ev.LogStreamName)) + "\n")
	b.WriteString("\n" + headingStyle.Render("MESSAGE:") + "\n\n")

	rawMsg := aws.ToString(ev.Message)
	// Try to pretty-print if it's JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(rawMsg), &parsed); err == nil {
		if indented, err := json.MarshalIndent(parsed, "", "  "); err == nil {
			rawMsg = string(indented)
		}
	}

	b.WriteString(rawMsg + "\n")

	modalW := m.width - 10
	modalH := m.height - 6
	if modalW < 40 {
		modalW = 40
	}
	if modalH < 10 {
		modalH = 10
	}

	// Support simple vertical scroll for massive stacktraces/JSON structures
	lines := strings.Split(b.String(), "\n")
	var scrollable []string
	if m.expandedScrollOffset >= len(lines) {
		m.expandedScrollOffset = max(0, len(lines)-1)
	}
	for i := m.expandedScrollOffset; i < len(lines) && len(scrollable) < modalH-4; i++ {
		scrollable = append(scrollable, lines[i])
	}

	renderedModal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Background(lipgloss.Color(ui.ColorBackground())).
		Padding(1, 2).
		Width(modalW).
		Height(modalH).
		Render(strings.Join(scrollable, "\n"))

	return lipgloss.Place(m.width, m.height-4, lipgloss.Center, lipgloss.Center, renderedModal)
}

func (m *model) getBorderColor(area focusArea) string {
	if m.focus == area {
		return ui.ColorBorderFocus()
	}
	return ui.ColorBorder()
}

func (m *model) getHelpHints() []ui.KeyHint {
	var hints []ui.KeyHint

	if m.showDetail {
		return []ui.KeyHint{
			ui.H("↑/↓", "scroll"),
			ui.H("y", "copy"),
			ui.H("Esc/Enter", "close"),
		}
	}

	switch m.focus {
	case focusGroups:
		hints = append(hints,
			ui.H("↑/↓", "groups"),
			ui.H("Enter", "select"),
			ui.H("/", "filter"),
			ui.H("G", "search entire group"),
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
			ui.H("Enter", "expand"),
			ui.H("/", "pattern"),
			ui.H("W", "tail watch"),
			ui.H("y", "copy"),
			ui.H("s", "export"),
			ui.H("Esc", "back"),
		)
	}

	hints = append(hints,
		ui.H("Tab", "panel"),
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
