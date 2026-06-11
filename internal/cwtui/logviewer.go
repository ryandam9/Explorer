package cwtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// viewerBackfillLimit caps how many events the initial "entire log" load keeps
// (most recent first, scanned over the 24-hour lookback window). New events
// arrive on top of this via streaming.
const viewerBackfillLimit = 2000

// viewerMaxEvents bounds memory while tailing a busy log for a long time; the
// oldest events are dropped once streaming pushes past this.
const viewerMaxEvents = 10000

// viewerKey identifies what the viewer is showing, so async responses for a
// viewer that has since been closed or re-opened on another stream are dropped.
type viewerKey struct {
	region  string
	group   string
	stream  string // empty => whole group
	pattern string
}

// logViewer is the full-screen log page opened with Enter on an event: the
// whole log (not a single event), live streaming, in-page search, copy and
// export.
type logViewer struct {
	active bool
	key    viewerKey
	title  string

	events []types.FilteredLogEvent
	seen   map[string]bool
	lastTS int64 // newest event timestamp seen (ms)

	lines   []string // wrapped display lines, rebuilt on resize/new events
	offset  int      // index of the first visible line
	follow  bool     // stick to the bottom as new events stream in
	loading bool

	search       textinput.Model
	searchActive bool
	term         string // confirmed/in-progress search term
	matches      []int  // line indices containing term
	matchIdx     int

	wrapW int
}

type viewerEventsMsg struct {
	key     viewerKey
	initial bool
	events  []types.FilteredLogEvent
	err     error
}

type viewerTickMsg struct {
	key viewerKey
}

// open resets the viewer for a new group/stream selection.
func (v *logViewer) open(key viewerKey, title string, wrapW int) {
	v.active = true
	v.key = key
	v.title = title
	v.events = nil
	v.seen = map[string]bool{}
	v.lastTS = 0
	v.lines = nil
	v.offset = 0
	v.follow = true
	v.loading = true
	v.searchActive = false
	v.search.SetValue("")
	v.term = ""
	v.matches = nil
	v.matchIdx = 0
	v.wrapW = wrapW
}

// append merges new events (deduplicated by event ID — the streaming fetch
// window overlaps the last seen timestamp) and rebuilds the display lines.
func (v *logViewer) append(events []types.FilteredLogEvent) {
	added := false
	for _, ev := range events {
		id := aws.ToString(ev.EventId)
		if id != "" && v.seen[id] {
			continue
		}
		if id != "" {
			v.seen[id] = true
		}
		v.events = append(v.events, ev)
		if ts := aws.ToInt64(ev.Timestamp); ts > v.lastTS {
			v.lastTS = ts
		}
		added = true
	}
	if len(v.events) > viewerMaxEvents {
		v.events = v.events[len(v.events)-viewerMaxEvents:]
	}
	if added {
		v.rebuild(v.wrapW)
	}
}

// rebuild flattens events into wrapped display lines and recomputes search
// matches. Multi-line messages keep their line breaks; continuation lines are
// indented under the timestamp.
func (v *logViewer) rebuild(wrapW int) {
	if wrapW < 20 {
		wrapW = 20
	}
	v.wrapW = wrapW
	v.lines = v.lines[:0]
	const indent = "    "
	for _, ev := range v.events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		prefix := "[" + t.Format("2006-01-02 15:04:05.000") + "] "
		msg := strings.TrimRight(aws.ToString(ev.Message), "\n")
		for i, raw := range strings.Split(msg, "\n") {
			line := indent + raw
			if i == 0 {
				line = prefix + raw
			}
			v.lines = append(v.lines, wrapLine(line, wrapW, indent)...)
		}
	}
	v.computeMatches()
	v.clampOffset()
}

// wrapLine hard-wraps a line to width, indenting wrapped continuations.
func wrapLine(line string, width int, indent string) []string {
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var out []string
	out = append(out, string(runes[:width]))
	rest := runes[width:]
	contW := width - len([]rune(indent))
	if contW < 10 {
		contW = 10
	}
	for len(rest) > 0 {
		n := contW
		if n > len(rest) {
			n = len(rest)
		}
		out = append(out, indent+string(rest[:n]))
		rest = rest[n:]
	}
	return out
}

func (v *logViewer) computeMatches() {
	v.matches = v.matches[:0]
	term := strings.ToLower(v.term)
	if term == "" {
		v.matchIdx = 0
		return
	}
	for i, l := range v.lines {
		if strings.Contains(strings.ToLower(l), term) {
			v.matches = append(v.matches, i)
		}
	}
	if v.matchIdx >= len(v.matches) {
		v.matchIdx = 0
	}
}

// nextMatch moves to the next/previous match and returns the line to scroll to
// (-1 when there are no matches).
func (v *logViewer) nextMatch(dir int) int {
	if len(v.matches) == 0 {
		return -1
	}
	v.matchIdx = (v.matchIdx + dir + len(v.matches)) % len(v.matches)
	return v.matches[v.matchIdx]
}

// jumpToFirstMatchFrom selects the first match at or after the given line,
// wrapping to the first match overall.
func (v *logViewer) jumpToFirstMatchFrom(line int) int {
	if len(v.matches) == 0 {
		return -1
	}
	v.matchIdx = 0
	for i, m := range v.matches {
		if m >= line {
			v.matchIdx = i
			break
		}
	}
	return v.matches[v.matchIdx]
}

func (v *logViewer) scrollBy(delta, bodyH int) {
	v.offset += delta
	v.clampOffsetFor(bodyH)
}

func (v *logViewer) scrollToBottom(bodyH int) {
	v.offset = max(0, len(v.lines)-bodyH)
}

// centerOn scrolls so that the given line sits roughly mid-screen.
func (v *logViewer) centerOn(line, bodyH int) {
	v.offset = line - bodyH/2
	v.clampOffsetFor(bodyH)
}

func (v *logViewer) clampOffset() {
	if v.offset < 0 {
		v.offset = 0
	}
	if v.offset >= len(v.lines) {
		v.offset = max(0, len(v.lines)-1)
	}
}

func (v *logViewer) clampOffsetFor(bodyH int) {
	maxOff := max(0, len(v.lines)-bodyH)
	if v.offset > maxOff {
		v.offset = maxOff
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// ---- model integration -----------------------------------------------------

// viewerWrapWidth is the usable text width inside the viewer's border/padding.
func (m *model) viewerWrapWidth() int {
	return max(20, m.width-6)
}

// viewerBodyHeight is how many log lines fit between the viewer header and the
// status bar.
func (m *model) viewerBodyHeight() int {
	return max(5, m.height-8)
}

// openViewer opens the full log page for the current group/stream selection.
func (m *model) openViewer(cmds *[]tea.Cmd) {
	grp, ok := m.selectedGroup()
	if !ok {
		return
	}
	key := viewerKey{
		region:  grp.Region,
		group:   aws.ToString(grp.LogGroupName),
		pattern: m.eventSearch.Value(),
	}
	title := key.group
	if !m.groupLevelSearch && len(m.filteredStreams) > 0 {
		key.stream = aws.ToString(m.filteredStreams[m.selectedStreamIdx].LogStreamName)
		title = key.stream
	}
	title += " [" + key.region + "]"

	m.watchMode = false
	m.viewer.open(key, title, m.viewerWrapWidth())
	*cmds = append(*cmds, m.loadViewerEventsCmd(true), m.viewerTickCmd())
}

func (m *model) loadViewerEventsCmd(initial bool) tea.Cmd {
	key := m.viewer.key
	since := m.viewer.lastTS
	if initial || since == 0 {
		since = time.Now().Add(-24 * time.Hour).UnixMilli()
	}
	return func() tea.Msg {
		events, err := m.client.GetLogEventsSince(m.ctx, key.region, key.group, key.stream, key.pattern, since, viewerBackfillLimit)
		return viewerEventsMsg{key: key, initial: initial, events: events, err: err}
	}
}

func (m *model) viewerTickCmd() tea.Cmd {
	key := m.viewer.key
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return viewerTickMsg{key: key} })
}

// handleViewerEvents applies an async event batch to the viewer, dropping
// stale responses for closed/re-targeted viewers.
func (m *model) handleViewerEvents(msg viewerEventsMsg, cmds *[]tea.Cmd) {
	if !m.viewer.active || msg.key != m.viewer.key {
		return
	}
	if msg.initial {
		m.viewer.loading = false
	}
	if msg.err != nil {
		// Keep the viewer open on streaming hiccups; surface the error briefly.
		m.setToast("Log fetch failed: " + msg.err.Error())
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	m.viewer.append(msg.events)
	if m.viewer.follow {
		m.viewer.scrollToBottom(m.viewerBodyHeight())
	}
}

// handleViewerKeys processes all keyboard input while the viewer is open.
func (m *model) handleViewerKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	v := &m.viewer
	bodyH := m.viewerBodyHeight()

	if v.searchActive {
		switch msg.String() {
		case "enter":
			v.searchActive = false
			if line := v.jumpToFirstMatchFrom(v.offset); line >= 0 {
				v.follow = false
				v.centerOn(line, bodyH)
			}
		case "esc":
			v.searchActive = false
			v.search.SetValue("")
			v.term = ""
			v.computeMatches()
		default:
			var cmd tea.Cmd
			v.search, cmd = v.search.Update(msg)
			*cmds = append(*cmds, cmd)
			v.term = v.search.Value()
			v.computeMatches()
		}
		return
	}

	switch msg.String() {
	case "esc", "q":
		v.active = false

	case "up", "k":
		v.follow = false
		v.scrollBy(-1, bodyH)
	case "down", "j":
		v.scrollBy(1, bodyH)
	case "pgup", "ctrl+u":
		v.follow = false
		v.scrollBy(-bodyH, bodyH)
	case "pgdown", "ctrl+d":
		v.scrollBy(bodyH, bodyH)
	case "g", "home":
		v.follow = false
		v.offset = 0
	case "G", "end":
		v.follow = true
		v.scrollToBottom(bodyH)
	case "f":
		v.follow = !v.follow
		if v.follow {
			v.scrollToBottom(bodyH)
		}

	case "/":
		v.searchActive = true
		v.search.Focus()
	case "n":
		if line := v.nextMatch(1); line >= 0 {
			v.follow = false
			v.centerOn(line, bodyH)
		}
	case "N":
		if line := v.nextMatch(-1); line >= 0 {
			v.follow = false
			v.centerOn(line, bodyH)
		}

	case "y":
		if len(v.events) == 0 {
			break
		}
		_ = clipboard.WriteAll(formatEvents(v.events))
		m.setToast(fmt.Sprintf("Copied %d log events to clipboard", len(v.events)))
		*cmds = append(*cmds, toastCmd(3*time.Second))

	case "s":
		streamLabel := "all_streams"
		if v.key.stream != "" {
			streamLabel = sanitizeFilename(v.key.stream)
		}
		path, err := exportEvents(v.events, sanitizeFilename(v.key.region+"-"+v.key.group), streamLabel)
		if err != nil {
			m.setToast("Export failed: " + err.Error())
		} else {
			m.setToast("Exported logs to " + path)
		}
		*cmds = append(*cmds, toastCmd(4*time.Second))
	}
}

// renderViewer draws the full-screen log page.
func (m *model) renderViewer() string {
	v := &m.viewer
	bodyH := m.viewerBodyHeight()

	headingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	liveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess())).Bold(true)

	title := " Log: " + v.title
	if len(title) > m.width-20 {
		title = title[:max(0, m.width-23)] + "..."
	}
	header := headingStyle.Render(title)
	if v.follow {
		header += "  " + liveStyle.Render("● LIVE")
	} else {
		header += "  " + mutedStyle.Render("⏸ paused (G to resume tail)")
	}

	var searchLine string
	if v.searchActive {
		searchLine = " Find: " + v.search.View()
	} else if v.term != "" {
		pos := 0
		if len(v.matches) > 0 {
			pos = v.matchIdx + 1
		}
		searchLine = fmt.Sprintf(" Find: %s  (%d/%d matches, n/N to jump)",
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(v.term),
			pos, len(v.matches))
	} else {
		searchLine = mutedStyle.Render("  (Press / to search within the log)")
	}

	var b strings.Builder
	b.WriteString(header + "\n")
	b.WriteString(searchLine + "\n\n")

	if v.loading {
		b.WriteString(fmt.Sprintf("  %s Loading full log…\n", m.spinner.View()))
	} else if len(v.lines) == 0 {
		b.WriteString("  No log events in the last 24 hours. Streaming for new events…\n")
	} else {
		end := v.offset + bodyH
		if end > len(v.lines) {
			end = len(v.lines)
		}
		currentMatch := -1
		if v.term != "" && len(v.matches) > 0 {
			currentMatch = v.matches[v.matchIdx]
		}
		for i := v.offset; i < end; i++ {
			marker := "  "
			if i == currentMatch {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render("▸ ")
			}
			b.WriteString(marker + m.styleViewerLine(v.lines[i], v.term) + "\n")
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Width(max(20, m.width-2)).
		Height(m.height - 4).
		Render(b.String())

	pos := "top"
	if len(v.lines) > 0 {
		bottom := min(v.offset+bodyH, len(v.lines))
		pos = fmt.Sprintf("lines %d-%d of %d", v.offset+1, bottom, len(v.lines))
	}
	statusText := fmt.Sprintf("Region: %s  ·  Events: %d  ·  %s", v.key.region, len(v.events), pos)
	if v.key.pattern != "" {
		statusText += "  ·  Pattern: " + v.key.pattern
	}

	return box + "\n" + ui.StatusBar(m.width, statusText, m.getHelpHints())
}

// styleViewerLine colors a log line by severity and highlights search matches.
func (m *model) styleViewerLine(line, term string) string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	low := strings.ToLower(line)
	if strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(low, "panic") {
		base = base.Foreground(lipgloss.Color(ui.ColorError()))
	} else if strings.Contains(low, "warn") {
		base = base.Foreground(lipgloss.Color(ui.ColorWarning()))
	}

	if term == "" {
		return base.Render(line)
	}

	matchStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorHighlight())).
		Foreground(lipgloss.Color(ui.ColorHighlightText()))

	var out strings.Builder
	lowTerm := strings.ToLower(term)
	rest := line
	for {
		idx := strings.Index(strings.ToLower(rest), lowTerm)
		if idx < 0 {
			out.WriteString(base.Render(rest))
			break
		}
		out.WriteString(base.Render(rest[:idx]))
		out.WriteString(matchStyle.Render(rest[idx : idx+len(term)]))
		rest = rest[idx+len(term):]
	}
	return out.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
