package cwtui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
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

	// formatJSON pretty-prints JSON objects/arrays embedded in log messages
	// (J toggles it). A viewing preference, so it survives re-opening the
	// viewer on another stream.
	formatJSON bool

	search       textinput.Model
	searchActive bool
	term         string // confirmed/in-progress search term
	matches      []int  // line indices containing term
	matchIdx     int

	// Grep filter (&): when grepRe is set, only matching lines are rendered
	// — the in-log equivalent of piping through grep. grepSrc keeps the
	// unwrapped matching lines so copy/export can write exactly what is
	// shown. An invalid in-progress regex is reported via grepErr while the
	// last valid filter stays applied.
	grepActive bool // the grep input has the keyboard
	grepInput  textinput.Model
	grepRe     *regexp.Regexp
	grepErr    string
	grepSrc    []string // unwrapped lines that passed the filter
	grepTotal  int      // unwrapped lines before filtering

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
	v.grepActive = false
	v.grepInput.SetValue("")
	v.grepRe = nil
	v.grepErr = ""
	v.grepSrc = nil
	v.grepTotal = 0
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
	v.grepSrc = v.grepSrc[:0]
	v.grepTotal = 0
	const indent = "    "
	for _, ev := range v.events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		prefix := "[" + t.Format("2006-01-02 15:04:05.000") + "] "
		msg := strings.TrimRight(aws.ToString(ev.Message), "\n")
		if v.formatJSON {
			msg = prettifyJSON(msg)
		}
		for i, raw := range strings.Split(msg, "\n") {
			line := indent + raw
			if i == 0 {
				line = prefix + raw
			}
			v.grepTotal++
			// The grep filter drops whole logical lines before wrapping, so
			// a long matching line keeps all its wrapped continuations.
			if v.grepRe != nil {
				if !v.grepRe.MatchString(line) {
					continue
				}
				v.grepSrc = append(v.grepSrc, line)
			}
			v.lines = append(v.lines, wrapLine(line, wrapW, indent)...)
		}
	}
	v.computeMatches()
	v.clampOffset()
}

// grepVisible reports whether the grep bar occupies a line of the viewer:
// while typing a pattern, and while a filter is applied.
func (v *logViewer) grepVisible() bool {
	return v.grepActive || v.grepRe != nil
}

// setGrep live-applies the grep pattern. An empty pattern clears the filter;
// a pattern that doesn't compile is flagged but keeps the last valid filter
// applied, so a half-typed regex never blanks the screen.
func (v *logViewer) setGrep(pattern string) {
	if strings.TrimSpace(pattern) == "" {
		v.grepRe, v.grepErr = nil, ""
		v.rebuild(v.wrapW)
		return
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		v.grepErr = "invalid regex"
		return
	}
	v.grepRe, v.grepErr = re, ""
	v.rebuild(v.wrapW)
}

// prettifyJSON expands a JSON object or array embedded in a log message into
// an indented block, keeping any leading text (timestamps, levels, request
// IDs) on its own line above it and any trailing text below. Messages without
// a JSON payload are returned unchanged.
func prettifyJSON(msg string) string {
	prefix, raw, suffix, ok := extractJSON(msg)
	var pretty bytes.Buffer
	if !ok || json.Indent(&pretty, raw, "", "  ") != nil {
		return msg
	}
	var parts []string
	if p := strings.TrimSpace(prefix); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, pretty.String())
	if s := strings.TrimSpace(suffix); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// extractJSON finds the first complete JSON object/array in s, returning the
// text before it, the raw JSON, and the text after it. Attempts are capped so
// brace-heavy non-JSON lines don't cost repeated parses.
func extractJSON(s string) (prefix string, raw []byte, suffix string, ok bool) {
	const maxAttempts = 5
	attempts := 0
	for i := 0; i < len(s) && attempts < maxAttempts; i++ {
		if s[i] != '{' && s[i] != '[' {
			continue
		}
		attempts++
		dec := json.NewDecoder(strings.NewReader(s[i:]))
		var msg json.RawMessage
		if dec.Decode(&msg) != nil {
			continue
		}
		// A bare number/string/bool is valid JSON but not worth reformatting.
		if len(msg) < 2 || (msg[0] != '{' && msg[0] != '[') {
			continue
		}
		end := i + int(dec.InputOffset())
		return s[:i], []byte(msg), s[end:], true
	}
	return "", nil, "", false
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
// Two extra columns are reserved for the scrollbar gutter (see renderViewer).
func (m *model) viewerWrapWidth() int {
	return max(20, m.width-8)
}

// viewerBodyHeight is how many log lines fit between the viewer header and the
// status bar.
func (m *model) viewerBodyHeight() int {
	h := max(5, m.height-8)
	if m.viewer.grepVisible() {
		h = max(5, h-1) // the grep bar takes one line
	}
	return h
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

	if v.grepActive {
		switch msg.String() {
		case "enter":
			// Keep the filter applied; an empty pattern means no filter.
			v.grepActive = false
			v.setGrep(v.grepInput.Value())
		case "esc":
			v.grepActive = false
			v.grepInput.SetValue("")
			v.setGrep("")
		default:
			var cmd tea.Cmd
			v.grepInput, cmd = v.grepInput.Update(msg)
			*cmds = append(*cmds, cmd)
			v.setGrep(v.grepInput.Value())
		}
		if v.follow {
			v.scrollToBottom(m.viewerBodyHeight())
		} else {
			v.clampOffsetFor(m.viewerBodyHeight())
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
	case "J":
		// Re-render every message with embedded JSON expanded (or collapsed
		// back to the raw line); the search matches follow the new lines.
		v.formatJSON = !v.formatJSON
		v.rebuild(v.wrapW)
		if v.follow {
			v.scrollToBottom(bodyH)
		} else {
			v.clampOffsetFor(bodyH)
		}

	case "/":
		v.searchActive = true
		v.search.Focus()
	case "&":
		// Grep filter, as in less(1): only lines matching the regex render.
		v.grepActive = true
		v.grepInput.Focus()
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
		// With a grep filter applied, copy exactly the rendered lines;
		// otherwise the full raw events.
		if v.grepRe != nil {
			if len(v.grepSrc) == 0 {
				break
			}
			_ = clipboard.WriteAll(strings.Join(v.grepSrc, "\n") + "\n")
			m.setToast(fmt.Sprintf("Copied %d matching lines to clipboard", len(v.grepSrc)))
			*cmds = append(*cmds, toastCmd(3*time.Second))
			break
		}
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
		grpLabel := sanitizeFilename(v.key.region + "-" + v.key.group)
		var path string
		var err error
		if v.grepRe != nil {
			// Export what is on screen: the grep-filtered lines.
			path, err = exportText(strings.Join(v.grepSrc, "\n")+"\n", grpLabel+"-grep", streamLabel)
		} else {
			path, err = exportEvents(v.events, grpLabel, streamLabel)
		}
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
	if v.formatJSON {
		header += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render("{} json")
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

	var grepLine string
	if v.grepActive {
		grepLine = " Grep: " + v.grepInput.View()
		if v.grepErr != "" {
			grepLine += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render("("+v.grepErr+")")
		}
	} else if v.grepRe != nil {
		grepLine = fmt.Sprintf(" Grep: %s  (%d of %d lines, & to edit, y/s copy/export matches)",
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(v.grepRe.String()),
			len(v.grepSrc), v.grepTotal)
	}

	var b strings.Builder
	b.WriteString(header + "\n")
	b.WriteString(searchLine + "\n")
	if grepLine != "" {
		b.WriteString(grepLine + "\n")
	}
	b.WriteString("\n")

	if v.loading {
		b.WriteString(fmt.Sprintf("  %s Loading full log…\n", m.spinner.View()))
	} else if len(v.lines) == 0 && v.grepRe != nil {
		b.WriteString(fmt.Sprintf("  No lines match the grep filter %s. Esc (in &) clears it.\n", v.grepRe.String()))
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
		// Vertical scrollbar gutter, one column to the right of the log rows.
		// The body rows are padded to a fixed width so the bar lands flush at
		// the box's right edge regardless of how short each line is.
		barLines := strings.Split(ui.VScrollbar(bodyH, len(v.lines), bodyH, v.offset), "\n")
		rowStyle := lipgloss.NewStyle().Width(max(20, m.width-4)).MaxHeight(1)
		for i := v.offset; i < end; i++ {
			marker := "  "
			if i == currentMatch {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render("▸ ")
			}
			row := rowStyle.Render(marker + m.styleViewerLine(v.lines[i], v.term))
			if r := i - v.offset; r < len(barLines) {
				row += " " + barLines[r]
			}
			b.WriteString(row + "\n")
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

// Log-level tokens recognised in a log line, matched as whole words so a
// substring like "info" inside "reinforcement" doesn't tint the line. Error
// keywords are checked first, so a line mentioning an error stands out even
// when it also carries a lower level (e.g. "INFO retry failed").
var (
	logErrorRe = regexp.MustCompile(`(?i)\b(error|errors|err|fatal|panic|exception|fail|failed|failure|critical|crit|alert|emerg)\b`)
	logWarnRe  = regexp.MustCompile(`(?i)\b(warn|warning|deprecated|deprecation)\b`)
	logInfoRe  = regexp.MustCompile(`(?i)\b(info|notice)\b`)
	logDebugRe = regexp.MustCompile(`(?i)\b(debug|trace|verbose)\b`)
)

// logLineColor returns the theme color a log line should be tinted with, based
// on the highest-severity log-level token it contains. Lines with no
// recognisable level render in the normal text color. Pulled out as a pure
// function so the severity mapping is unit-testable without rendering.
func logLineColor(line string) string {
	switch {
	case logErrorRe.MatchString(line):
		return ui.ColorError()
	case logWarnRe.MatchString(line):
		return ui.ColorWarning()
	case logInfoRe.MatchString(line):
		return ui.ColorInfo()
	case logDebugRe.MatchString(line):
		return ui.ColorMuted()
	default:
		return ui.ColorText()
	}
}

// styleViewerLine colors a log line by severity and highlights search matches.
func (m *model) styleViewerLine(line, term string) string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(logLineColor(line)))

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
