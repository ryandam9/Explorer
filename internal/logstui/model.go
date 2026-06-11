// Package logstui implements an interactive browser for CloudWatch Logs:
// a log-group list on the left and a log-event viewer on the right, fetching
// events on demand via FilterLogEvents (one page at a time — the API allows
// only ~5 requests/second per account/region).
package logstui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/aws_explorer/internal/logs"
	"github.com/user/aws_explorer/internal/table"
	"github.com/user/aws_explorer/internal/ui"
)

// logsAPI is the slice of the CloudWatch Logs client the TUI needs; tests
// substitute a fake.
type logsAPI interface {
	cloudwatchlogs.DescribeLogGroupsAPIClient
	cloudwatchlogs.FilterLogEventsAPIClient
}

type focusArea int

const (
	focusGroups focusArea = iota
	focusEvents
	focusGroupSearch
	focusPattern
)

// timeWindows are the selectable look-back windows, cycled with "t".
var timeWindows = []time.Duration{
	15 * time.Minute,
	time.Hour,
	3 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	3 * 24 * time.Hour,
	7 * 24 * time.Hour,
}

// ── Messages ─────────────────────────────────────────────────────────────────

type groupsMsg struct {
	groups []logs.Group
	err    error
}

type eventsMsg struct {
	group  string
	page   logs.Page
	append bool
	err    error
}

// ── Model ────────────────────────────────────────────────────────────────────

// Model is the Bubble Tea model for the logs browser.
type Model struct {
	ctx    context.Context
	client logsAPI
	region string

	width  int
	height int
	focus  focusArea

	// Group list
	groups      []logs.Group
	groupSearch textinput.Model
	groupsTable table.Model

	// Event viewer
	currentGroup string
	events       []logs.Event
	nextToken    *string
	windowIdx    int
	pattern      string
	patternInput textinput.Model
	viewport     viewport.Model

	spinner   spinner.Model
	loading   bool
	statusMsg string
	errMsg    string
}

// NewModel builds the logs TUI. When initialGroup is non-empty its events are
// fetched immediately. initialSince picks the closest look-back window.
func NewModel(ctx context.Context, client logsAPI, region, themeName, initialGroup string,
	initialSince time.Duration, initialPattern string) Model {

	if idx, ok := ui.LookupTheme(themeName); ok {
		ui.SetActiveTheme(idx)
	}

	gt := table.New(
		table.WithColumns([]table.Column{
			{Title: "LOG GROUP", Width: 38},
			{Title: "RETENTION", Width: 10},
			{Title: "STORED", Width: 9},
		}),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
	)

	search := textinput.New()
	search.Placeholder = "filter groups…"
	search.CharLimit = 128

	pat := textinput.New()
	pat.Placeholder = `filter pattern, e.g. ERROR or { $.level = "error" }`
	pat.CharLimit = 256
	pat.SetValue(initialPattern)

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))),
	)

	m := Model{
		ctx:          ctx,
		client:       client,
		region:       region,
		groupsTable:  gt,
		groupSearch:  search,
		patternInput: pat,
		currentGroup: initialGroup,
		windowIdx:    closestWindow(initialSince),
		pattern:      initialPattern,
		viewport:     viewport.New(80, 20),
		spinner:      sp,
		loading:      true,
	}
	return m
}

// closestWindow returns the index of the smallest window that covers d, so a
// requested look-back is never silently shortened.
func closestWindow(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	for i, w := range timeWindows {
		if w >= d {
			return i
		}
	}
	return len(timeWindows) - 1
}

func (m Model) window() time.Duration { return timeWindows[m.windowIdx] }

// ── Commands ─────────────────────────────────────────────────────────────────

func (m Model) loadGroupsCmd() tea.Cmd {
	ctx, client, region := m.ctx, m.client, m.region
	return func() tea.Msg {
		groups, err := logs.ListGroups(ctx, client, region)
		return groupsMsg{groups: groups, err: err}
	}
}

func (m Model) fetchEventsCmd(group string, token *string, appendEvents bool) tea.Cmd {
	ctx, client := m.ctx, m.client
	in := logs.FetchInput{
		Group:     group,
		Start:     time.Now().Add(-m.window()),
		Pattern:   m.pattern,
		NextToken: token,
	}
	return func() tea.Msg {
		page, err := logs.FetchEvents(ctx, client, in)
		return eventsMsg{group: group, page: page, append: appendEvents, err: err}
	}
}

// Init starts the group listing (and the initial event fetch, if requested).
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.loadGroupsCmd()}
	if m.currentGroup != "" {
		cmds = append(cmds, m.fetchEventsCmd(m.currentGroup, nil, false))
	}
	return tea.Batch(cmds...)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case groupsMsg:
		m.loading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			if len(msg.groups) > 0 {
				m.errMsg += " (partial group list kept)"
			}
		}
		m.groups = msg.groups
		m.refreshGroupRows()
		return m, nil

	case eventsMsg:
		m.loading = false
		if msg.group != m.currentGroup {
			return m, nil // stale response from a previous selection
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		if msg.append {
			m.events = append(m.events, msg.page.Events...)
		} else {
			m.events = msg.page.Events
		}
		m.nextToken = msg.page.NextToken
		atBottom := m.viewport.AtBottom()
		m.viewport.SetContent(m.renderEvents())
		if msg.append || atBottom {
			m.viewport.GotoBottom()
		}
		m.statusMsg = fmt.Sprintf("%d events", len(m.events))
		if m.nextToken != nil {
			m.statusMsg += " · more available (m)"
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Text inputs capture most keys while focused.
	switch m.focus {
	case focusGroupSearch:
		switch msg.String() {
		case "enter", "esc":
			m.focus = focusGroups
			m.groupSearch.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.groupSearch, cmd = m.groupSearch.Update(msg)
		m.refreshGroupRows()
		return m, cmd

	case focusPattern:
		switch msg.String() {
		case "enter":
			m.pattern = m.patternInput.Value()
			m.focus = focusEvents
			m.patternInput.Blur()
			return m.startFetch(false)
		case "esc":
			m.patternInput.SetValue(m.pattern)
			m.focus = focusEvents
			m.patternInput.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.patternInput, cmd = m.patternInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab", "shift+tab":
		if m.focus == focusGroups {
			m.focus = focusEvents
		} else {
			m.focus = focusGroups
		}
		return m, nil

	case "/":
		if m.focus == focusGroups {
			m.focus = focusGroupSearch
			m.groupSearch.Focus()
			return m, textinput.Blink
		}

	case "f":
		m.focus = focusPattern
		m.patternInput.Focus()
		return m, textinput.Blink

	case "t":
		m.windowIdx = (m.windowIdx + 1) % len(timeWindows)
		if m.currentGroup != "" {
			return m.startFetch(false)
		}
		return m, nil

	case "r":
		if m.currentGroup != "" {
			return m.startFetch(false)
		}
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, m.loadGroupsCmd())

	case "m":
		if m.currentGroup != "" && m.nextToken != nil && !m.loading {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetchEventsCmd(m.currentGroup, m.nextToken, true))
		}
		return m, nil

	case "enter":
		if m.focus == focusGroups {
			if g, ok := m.selectedGroup(); ok {
				m.currentGroup = g
				m.focus = focusEvents
				return m.startFetch(false)
			}
		}
		return m, nil
	}

	// Delegate navigation to the focused widget.
	var cmd tea.Cmd
	if m.focus == focusGroups {
		m.groupsTable, cmd = m.groupsTable.Update(msg)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

// startFetch begins a fresh (non-append) event fetch for the current group.
func (m Model) startFetch(appendEvents bool) (tea.Model, tea.Cmd) {
	m.loading = true
	m.errMsg = ""
	if !appendEvents {
		m.events = nil
		m.nextToken = nil
		m.viewport.SetContent("")
		m.statusMsg = ""
	}
	return m, tea.Batch(m.spinner.Tick, m.fetchEventsCmd(m.currentGroup, nil, appendEvents))
}

// selectedGroup returns the log group selected in the table.
func (m Model) selectedGroup() (string, bool) {
	row := m.groupsTable.SelectedRow()
	if len(row) == 0 || row[0] == "" {
		return "", false
	}
	return row[0], true
}

// refreshGroupRows applies the quick filter to the group list.
func (m *Model) refreshGroupRows() {
	q := strings.ToLower(m.groupSearch.Value())
	rows := make([]table.Row, 0, len(m.groups))
	for _, g := range m.groups {
		if q != "" && !strings.Contains(strings.ToLower(g.Name), q) {
			continue
		}
		rows = append(rows, table.Row{
			g.Name,
			logs.FormatRetention(g.RetentionDays),
			logs.FormatBytes(g.StoredBytes),
		})
	}
	m.groupsTable.SetRows(rows)
}

// ── Layout / View ────────────────────────────────────────────────────────────

const statusBarHeight = 1

func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	leftW := m.width * 2 / 5
	if leftW > 64 {
		leftW = 64
	}
	if leftW < 28 {
		leftW = 28
	}
	rightW := m.width - leftW - 6 // borders + padding
	if rightW < 20 {
		rightW = 20
	}
	bodyH := m.height - statusBarHeight - 4 // borders + header line
	if bodyH < 4 {
		bodyH = 4
	}
	m.groupsTable.SetWidth(leftW)
	m.groupsTable.SetHeight(bodyH - 1)
	m.viewport.Width = rightW
	m.viewport.Height = bodyH
	m.viewport.SetContent(m.renderEvents())
}

// renderEvents formats the fetched events for the viewport, wrapping long
// messages to the viewport width.
func (m Model) renderEvents() string {
	if m.currentGroup == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("Select a log group and press Enter to fetch its events.")
	}
	if len(m.events) == 0 {
		if m.loading {
			return ""
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No events found in the requested window.")
	}

	tsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	streamStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))

	width := m.viewport.Width
	var b strings.Builder
	for _, e := range m.events {
		ts := e.Timestamp.Format("01-02 15:04:05")
		stream := clipString(e.Stream, 24)
		prefix := ts + "  " + stream + "  "
		indent := strings.Repeat(" ", len(ts)+2)
		msg := strings.TrimRight(e.Message, "\n")
		for i, line := range wrapText(msg, width-len(prefix)) {
			if i == 0 {
				b.WriteString(tsStyle.Render(ts) + "  " + streamStyle.Render(stream) + "  ")
			} else {
				b.WriteString(indent)
			}
			b.WriteString(textStyle.Render(line) + "\n")
		}
	}
	return b.String()
}

// clipString shortens s to max runes with a trailing ellipsis.
func clipString(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// wrapText hard-wraps s to width, preserving embedded newlines. Width <= 0
// disables wrapping.
func wrapText(s string, width int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if width <= 0 || len([]rune(line)) <= width {
			out = append(out, line)
			continue
		}
		r := []rune(line)
		for len(r) > width {
			out = append(out, string(r[:width]))
			r = r[width:]
		}
		out = append(out, string(r))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	headStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))

	// Left pane: group list (+ search box while filtering).
	left := m.groupsTable.View()
	if m.focus == focusGroupSearch || m.groupSearch.Value() != "" {
		left = m.groupSearch.View() + "\n" + left
	}
	leftPane := ui.TablePanelStyle(m.focus == focusGroups || m.focus == focusGroupSearch).Render(left)

	// Right pane: header line + events (or the pattern input while editing).
	title := headStyle.Render("CloudWatch Logs") + mutedStyle.Render(" · "+m.region)
	if m.currentGroup != "" {
		title = headStyle.Render(m.currentGroup) +
			mutedStyle.Render(fmt.Sprintf(" · last %s", formatWindow(m.window())))
		if m.pattern != "" {
			title += mutedStyle.Render(" · filter ") + headStyle.Render(m.pattern)
		}
	}
	if m.loading {
		title += "  " + m.spinner.View()
	}

	body := m.viewport.View()
	if m.focus == focusPattern {
		body = m.patternInput.View() + "\n" + body
	}
	rightPane := ui.TablePanelStyle(m.focus == focusEvents || m.focus == focusPattern).
		Render(title + "\n" + body)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	statusLeft := m.statusMsg
	if m.errMsg != "" {
		statusLeft = errStyle.Render(clipString(m.errMsg, m.width/2))
	}
	bar := ui.StatusBar(m.width, statusLeft, m.keyHints())

	return panes + "\n" + bar
}

// formatWindow renders a look-back window compactly (15m, 1h, 3d).
func formatWindow(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// keyHints lists only the shortcuts usable right now, matching the
// context-aware convention used by the other TUIs.
func (m Model) keyHints() []ui.KeyHint {
	switch m.focus {
	case focusGroupSearch:
		return []ui.KeyHint{ui.H("enter/esc", "done"), ui.H("ctrl+c", "quit")}
	case focusPattern:
		return []ui.KeyHint{ui.H("enter", "apply"), ui.H("esc", "cancel"), ui.H("ctrl+c", "quit")}
	case focusGroups:
		return []ui.KeyHint{
			ui.H("↑/↓", "navigate"), ui.H("enter", "fetch events"), ui.H("/", "filter groups"),
			ui.H("t", "window "+formatWindow(m.window())), ui.H("tab", "events"), ui.H("q", "quit"),
		}
	default:
		hints := []ui.KeyHint{ui.H("↑/↓", "scroll")}
		if m.nextToken != nil {
			hints = append(hints, ui.H("m", "more"))
		}
		hints = append(hints,
			ui.H("r", "refresh"), ui.H("f", "pattern"),
			ui.H("t", "window "+formatWindow(m.window())), ui.H("tab", "groups"), ui.H("q", "quit"))
		return hints
	}
}
