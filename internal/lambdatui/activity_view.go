package lambdatui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/sparkline"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The activity view (a on a function): a small form (date, regex, optional
// server-side filter) and then a full-screen report — the day's invocation
// counts from CloudWatch metrics, an hourly sparkline, and a table of the log
// events the regex matched. The log scan is a pull loop: each page's message
// issues the next fetch, so the view fills in live and Esc cancels it.

const (
	actFieldDate = iota
	actFieldPattern
	actFieldFilter
	actFieldCount
)

var actFieldLabels = [actFieldCount]string{"Date", "Regex", "Server filter"}

// matchCellMax bounds a capture-group cell so one long match can't make its
// column the width of the terminal several times over; the footer shows the
// full text.
const matchCellMax = 120

// activityMessageMin is the narrowest the MESSAGE column is squeezed to. On a
// terminal too narrow for that, the column keeps this width and the table
// scrolls horizontally (< >) rather than wrapping the message into a sliver.
const activityMessageMin = 40

// activityWrapMax caps how many table lines one match's message may take, so a
// multi-kilobyte line or a long traceback can't fill the page by itself. Past
// the cap the last line says how much was left; y copies the full line.
const activityWrapMax = 8

type activityState struct {
	// Form.
	formActive bool
	inputs     [actFieldCount]textinput.Model
	focus      int
	formErr    string

	// Report.
	active bool
	fn     Function
	query  ActivityQuery
	gen    int                // bumps per run so a superseded run's messages are dropped
	ctx    context.Context    // the run's context; Esc cancels it (and every page after)
	cancel context.CancelFunc // nil once the run is stopped

	metricsLoading bool
	stats          InvocationStats
	statsErr       error

	scan     *LogScan
	scanning bool

	tbl table.Model
}

type activityMetricsMsg struct {
	gen   int
	stats InvocationStats
	err   error
}

type activityPageMsg struct {
	gen    int
	events []LogEvent
	next   *string
	err    error
}

func newActivityInputs() [actFieldCount]textinput.Model {
	var in [actFieldCount]textinput.Model
	placeholders := [actFieldCount]string{
		"YYYY-MM-DD, today or yesterday",
		`Go regex, e.g. Copied (\S+) · empty = every event`,
		`optional CloudWatch filter, e.g. "Copied"`,
	}
	for i := range in {
		t := textinput.New()
		t.Placeholder = placeholders[i]
		t.CharLimit = 512
		t.Width = 56
		t.Prompt = ""
		in[i] = t
	}
	in[actFieldDate].SetValue("today")
	return in
}

// openActivityForm shows the query form for the selected function. The regex
// and filter are remembered from the previous run.
func (mm *m) openActivityForm() {
	fn, ok := mm.selectedFunction()
	if !ok {
		return
	}
	mm.act.fn = fn
	mm.act.formActive = true
	mm.act.formErr = ""
	mm.focusActivityField(actFieldDate)
}

func (mm *m) focusActivityField(i int) {
	mm.act.focus = (i + actFieldCount) % actFieldCount
	for j := range mm.act.inputs {
		if j == mm.act.focus {
			mm.act.inputs[j].Focus()
		} else {
			mm.act.inputs[j].Blur()
		}
	}
}

func (mm *m) handleActivityFormKey(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		*cmds = append(*cmds, tea.Quit)
	case "esc":
		mm.act.formActive = false
	case "tab", "down":
		mm.focusActivityField(mm.act.focus + 1)
	case "shift+tab", "up":
		mm.focusActivityField(mm.act.focus - 1)
	case "enter":
		q, err := mm.activityQueryFromForm(time.Now())
		if err != nil {
			mm.act.formErr = err.Error()
			return
		}
		mm.act.formActive = false
		mm.startActivity(q, cmds)
	default:
		var cmd tea.Cmd
		mm.act.inputs[mm.act.focus], cmd = mm.act.inputs[mm.act.focus].Update(msg)
		mm.act.formErr = ""
		*cmds = append(*cmds, cmd)
	}
}

// activityQueryFromForm validates the form into a query.
func (mm *m) activityQueryFromForm(now time.Time) (ActivityQuery, error) {
	day, err := ParseDay(mm.act.inputs[actFieldDate].Value(), now, time.Local)
	if err != nil {
		return ActivityQuery{}, err
	}
	// An empty regex lists every event of the day (after the server filter,
	// when one is given).
	re := matchAll
	if p := strings.TrimSpace(mm.act.inputs[actFieldPattern].Value()); p != "" {
		if re, err = regexp.Compile(p); err != nil {
			return ActivityQuery{}, fmt.Errorf("invalid regex: %v", err)
		}
	}
	filter := strings.TrimSpace(mm.act.inputs[actFieldFilter].Value())
	fn := mm.act.fn
	return ActivityQuery{
		Region: fn.Region, Function: fn.Name, LogGroup: fn.LogGroup,
		Day: day, Pattern: re, Filter: filter, MaxMatches: DefaultMaxMatches,
	}, nil
}

// startActivity cancels any run in flight and starts a new one: the metrics
// call and (with a regex) the first log page, in parallel.
func (mm *m) startActivity(q ActivityQuery, cmds *[]tea.Cmd) {
	mm.stopActivity()
	a := &mm.act
	a.gen++
	a.active = true
	a.query = q
	a.stats, a.statsErr = InvocationStats{}, nil
	a.metricsLoading = true
	a.scan = nil
	a.scanning = false
	ctx, cancel := context.WithCancel(mm.ctx)
	a.ctx, a.cancel = ctx, cancel

	gen := a.gen
	*cmds = append(*cmds, mm.spinner.Tick, func() tea.Msg {
		slog.Info("Loading Lambda invocation metrics", "function", q.Function, "region", q.Region, "date", q.Day.Date)
		st, err := mm.client.InvocationStats(ctx, q.Region, q.Function, q.Day)
		return activityMetricsMsg{gen: gen, stats: st, err: err}
	})
	if q.Pattern != nil {
		a.scan = NewLogScan(q.Pattern, q.Filter, q.MaxMatches)
		a.scanning = true
		*cmds = append(*cmds, mm.activityPageCmd(ctx, gen, q, nil))
	}
	a.tbl = newLambdaTable(activityColumns(a.scan))
	mm.refreshActivityRows()
}

func (mm *m) activityPageCmd(ctx context.Context, gen int, q ActivityQuery, token *string) tea.Cmd {
	return func() tea.Msg {
		events, next, err := mm.client.LogPage(ctx, q, token)
		return activityPageMsg{gen: gen, events: events, next: next, err: err}
	}
}

// stopActivity cancels the run in flight (its late messages are dropped by the
// generation check).
func (mm *m) stopActivity() {
	if mm.act.cancel != nil {
		mm.act.cancel()
		mm.act.cancel = nil
	}
	if mm.act.scanning && mm.act.scan != nil {
		mm.act.scan.finish("scan cancelled")
	}
	mm.act.scanning = false
}

func (mm *m) closeActivity() {
	mm.stopActivity()
	mm.act.gen++
	mm.act.active = false
}

func (mm *m) handleActivityMetrics(msg activityMetricsMsg) {
	if msg.gen != mm.act.gen || !mm.act.active {
		return
	}
	mm.act.metricsLoading = false
	mm.act.stats, mm.act.statsErr = msg.stats, msg.err
	if msg.err != nil && errors.Is(msg.err, context.Canceled) {
		mm.act.statsErr = errors.New("cancelled")
		return
	}
	if msg.err != nil {
		slog.Warn("Lambda invocation metrics failed", "function", mm.act.query.Function, "error", msg.err.Error())
	}
}

// handleActivityPage folds a page into the scan and issues the next one.
func (mm *m) handleActivityPage(msg activityPageMsg, cmds *[]tea.Cmd) {
	a := &mm.act
	if msg.gen != a.gen || !a.active || a.scan == nil || !a.scanning {
		return
	}
	if msg.err != nil {
		a.scanning = false
		if a.ctx.Err() != nil {
			return // cancelled: the scan is already marked stopped
		}
		slog.Warn("Lambda log scan failed", "logGroup", a.query.LogGroup, "error", msg.err.Error())
		a.scan.Fail(msg.err)
		mm.refreshActivityRows()
		return
	}
	if a.scan.Ingest(msg.events) && a.scan.Advance(msg.next != nil) {
		*cmds = append(*cmds, mm.activityPageCmd(a.ctx, a.gen, a.query, msg.next))
	} else {
		a.scanning = false
		a.scan.SortMatches()
	}
	mm.refreshActivityRows()
}

// activityColumns is the match table's column set: time, request ID, level,
// then one column per capture group (or the matched text when the regex has
// none), then the message.
func activityColumns(scan *LogScan) []table.Column {
	cols := []table.Column{
		{Title: "TIME", Width: 12},
		{Title: "REQUEST ID", Width: 10},
		{Title: "LEVEL", Width: 5},
	}
	if scan != nil {
		for _, g := range scan.GroupNames {
			cols = append(cols, table.Column{Title: strings.ToUpper(groupHeader(g)), Width: 6})
		}
		if scan.MatchColumn() {
			cols = append(cols, table.Column{Title: "MATCH", Width: 8})
		}
	}
	return append(cols, table.Column{Title: "MESSAGE", Width: 20})
}

// activityRow renders one match as a table row. The request ID is shortened to
// its first block (enough to tell invocations apart; the footer shows it in
// full). The message is left whole (last cell); refreshActivityRows wraps it to
// the panel.
func activityRow(m Match, loc *time.Location, matchCol bool) table.Row {
	id := "—"
	if m.RequestID != "" {
		id = m.RequestID
		if i := strings.IndexByte(id, '-'); i > 0 {
			id = id[:i]
		}
		if m.RequestIDInferred {
			id += "~"
		}
	}
	row := table.Row{matchTime(m.Time, loc), id, dashEm(m.Level)}
	for _, g := range m.Groups {
		row = append(row, dashEm(truncate(oneLine(g), matchCellMax)))
	}
	if matchCol {
		row = append(row, dashEm(truncate(oneLine(m.Matched), matchCellMax)))
	}
	return append(row, m.Body)
}

// wrapActivityMessage lays a message out over the MESSAGE column: line breaks
// in the message start new lines (Python tracebacks separate theirs with a
// bare \r), and a line too wide wraps at word boundaries, hard-breaking a
// token with no space in it (an ARN, a JSON blob). At most activityWrapMax
// lines; when the cap bites, the last line says how many were left out.
func wrapActivityMessage(msg string, width int) []string {
	msg = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(msg)
	var out []string
	for _, para := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		out = append(out, ui.WrapWords(para, width)...)
	}
	if len(out) > activityWrapMax {
		hidden := len(out) - (activityWrapMax - 1)
		note := fmt.Sprintf("… +%d more line(s) — y copies the full line", hidden)
		out = append(out[:activityWrapMax-1], ansi.Truncate(note, width, "…"))
	}
	return out
}

// activityMessageWidth is the width left for the MESSAGE (last) column once the
// other columns are sized to their widest cell, so a long log line fills the
// panel on a wide terminal instead of stopping at a fixed cap. The table's
// width (the panel's inner width, as fitTable sets it) and the vertical
// scrollbar gutter are reserved unconditionally, so the column doesn't reflow
// when the match count crosses a page. Never below activityMessageMin.
func activityMessageWidth(termWidth int, cols []table.Column, rows []table.Row) int {
	if termWidth <= 0 || len(cols) == 0 {
		return matchCellMax
	}
	pad := ui.TableStyles().Cell.GetHorizontalPadding()
	avail := termWidth - 4 - 2 // fitTable's panel inset, the scrollbar gutter
	last := len(cols) - 1
	for i, c := range cols[:last] {
		w := max(c.Width, ansi.StringWidth(c.Title))
		for _, r := range rows {
			if i < len(r) {
				w = max(w, ansi.StringWidth(r[i]))
			}
		}
		avail -= w + pad
	}
	return max(avail-pad, activityMessageMin)
}

func dashEm(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (mm *m) refreshActivityRows() {
	a := &mm.act
	if a.scan == nil {
		a.tbl.SetRows(nil)
		return
	}
	cur := a.tbl.CursorGroup()
	loc := a.query.Day.Start.Location()
	matchRows := make([]table.Row, 0, len(a.scan.Matches))
	for _, m := range a.scan.Matches {
		matchRows = append(matchRows, activityRow(m, loc, a.scan.MatchColumn()))
	}
	// A long message wraps onto continuation rows whose other cells are blank;
	// the row groups keep each match one selectable, striped unit.
	msgW := activityMessageWidth(mm.width, activityColumns(a.scan), matchRows)
	rows := make([]table.Row, 0, len(matchRows))
	groups := make([]int, 0, len(matchRows))
	for i, r := range matchRows {
		last := len(r) - 1
		for j, line := range wrapActivityMessage(r[last], msgW) {
			row := make(table.Row, len(r))
			if j == 0 {
				copy(row, r[:last])
			}
			row[last] = line
			rows = append(rows, row)
			groups = append(groups, i)
		}
	}
	a.tbl.SetRows(rows)
	a.tbl.SetRowGroups(groups)
	a.tbl.SetCursorGroup(max(min(cur, len(matchRows)-1), 0))
}

func (mm *m) selectedMatch() (Match, bool) {
	a := &mm.act
	if a.scan == nil {
		return Match{}, false
	}
	i := a.tbl.CursorGroup()
	if i < 0 || i >= len(a.scan.Matches) {
		return Match{}, false
	}
	return a.scan.Matches[i], true
}

func (mm *m) handleActivityKey(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	a := &mm.act
	switch msg.String() {
	case "q", "ctrl+c":
		*cmds = append(*cmds, tea.Quit)
	case "esc", "backspace":
		if a.scanning {
			mm.stopActivity() // first Esc stops a running scan, keeping what was read
			return
		}
		mm.closeActivity()
	case "up", "k":
		a.tbl.MoveUp(1)
	case "down", "j":
		a.tbl.MoveDown(1)
	case "pgup":
		a.tbl.MoveUp(max(a.tbl.Height()-1, 1))
	case "pgdown", "pgdn", " ":
		a.tbl.MoveDown(max(a.tbl.Height()-1, 1))
	case "g", "home":
		a.tbl.GotoTop()
	case "G", "end":
		a.tbl.GotoBottom()
	case "<", ",":
		a.tbl.ScrollLeft()
	case ">", ".":
		a.tbl.ScrollRight()
	case "[", "]":
		n := -1
		if msg.String() == "]" {
			n = 1
		}
		q := a.query
		q.Day = q.Day.Shift(n)
		if q.Day.Start.After(time.Now()) {
			mm.setToast("That day hasn't started yet")
			*cmds = append(*cmds, toastCmd(3*time.Second))
			return
		}
		a.inputs[actFieldDate].SetValue(q.Day.Date)
		mm.startActivity(q, cmds)
	case "r":
		mm.startActivity(a.query, cmds)
	case "e":
		mm.stopActivity()
		a.inputs[actFieldDate].SetValue(a.query.Day.Date)
		a.formActive = true
		a.formErr = ""
		mm.focusActivityField(actFieldPattern)
	case "y":
		if m, ok := mm.selectedMatch(); ok {
			_ = clipboard.WriteAll(m.Message)
			mm.setToast("Copied log message")
			*cmds = append(*cmds, toastCmd(3*time.Second))
		}
	case ui.KeyDebug:
		mm.debug.Open(mm.width, mm.height)
	case ui.KeyAbout:
		mm.showAbout = true
	}
}

// --- rendering --------------------------------------------------------------

func (mm *m) renderActivityForm() string {
	a := &mm.act
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	label := lipgloss.NewStyle().Width(15).Foreground(lipgloss.Color(ui.ColorText()))
	focused := label.Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)

	var b strings.Builder
	b.WriteString(heading.Render("Activity — "+a.fn.Name) + "\n")
	b.WriteString(muted.Render(fmt.Sprintf("%s · logs %s · days are local time (%s)", a.fn.Region, a.fn.LogGroup, time.Now().Format("MST"))) + "\n\n")
	for i := range a.inputs {
		l := label
		marker := "  "
		if i == a.focus {
			l, marker = focused, "▸ "
		}
		b.WriteString(marker + l.Render(actFieldLabels[i]) + a.inputs[i].View() + "\n")
	}
	b.WriteString("\n")
	if a.formErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render("✗ "+a.formErr) + "\n\n")
	}
	b.WriteString(muted.Render("Invocations come from CloudWatch metrics. The regex (Go syntax; (?i) for\n" +
		"case-insensitive) is run over the day's log events; capture groups become\n" +
		"columns. The server filter (CloudWatch syntax) reads less data first.\n\n" +
		"Tab/↑↓ field · Enter run · Esc cancel"))
	w := 84
	if mm.width > 0 && mm.width-4 < w {
		w = mm.width - 4
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(1, 2).Width(w).Render(b.String())
}

// renderActivity draws the report: header lines, the match table and the
// selected match's footer. Heights are measured, not assumed, so the table
// fills exactly the space left.
func (mm *m) renderActivity() string {
	head := mm.activityHeader()
	a := &mm.act
	if a.scan == nil {
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("No regex given — press e to add one and scan the day's log events.")
	}
	if len(a.scan.Matches) == 0 {
		msg := "No log events matched yet…"
		switch {
		case matchesAll(a.query.Pattern) && a.scanning:
			msg = "No log events yet…"
		case matchesAll(a.query.Pattern):
			msg = "No log events for this day."
		case !a.scanning:
			msg = "No log events matched."
		}
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(msg)
	}
	foot := mm.activityFooter()
	mm.fitTable(&a.tbl, lipgloss.Height(head), lipgloss.Height(foot)+1)
	return head + "\n" + ui.TablePanelStyle(true).Render(a.tbl.View()) +
		"\n" + ui.TableScrollIndicator(&a.tbl) + "\n" + foot
}

func (mm *m) activityHeader() string {
	a := &mm.act
	q := a.query
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
	errS := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
	w := max(mm.width-2, 20)

	var lines []string
	title := " Activity — " + q.Function + " · " + q.Day.Label()
	if q.Day.InProgress(time.Now()) {
		title += " · in progress"
	}
	lines = append(lines, heading.Render(ansi.Truncate(title, w, "…")))

	// Invocations.
	switch {
	case a.metricsLoading:
		lines = append(lines, "  "+mm.spinner.View()+" loading invocation metrics…")
	case a.statsErr != nil:
		lines = append(lines, errS.Render(ansi.Truncate("  Invocations unknown — CloudWatch metrics could not be read: "+a.statsErr.Error(), w, "…")))
	case !a.stats.HasData:
		lines = append(lines, "  "+accent.Render("0")+" invocations "+
			muted.Render("— no datapoints (not invoked that day, or older than the 455-day retention)"))
	default:
		st := a.stats
		line := "  " + accent.Render(formatCount(st.Invocations)) + " invocations  ·  " +
			formatCount(st.Errors) + " errors  ·  " + formatCount(st.Throttles) + " throttles" +
			muted.Render("   (CloudWatch AWS/Lambda, Sum)")
		lines = append(lines, line)
		spark := sparkline.Render(st.HourlyInvocations())
		hourLine := "  per hour " + muted.Render(st.Hours[0].Start.Format("15h")) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(spark) + " " +
			muted.Render(st.Hours[len(st.Hours)-1].Start.Format("15h"))
		if p, ok := st.Peak(); ok {
			hourLine += muted.Render(fmt.Sprintf("  ·  peak %s at %s", formatCount(p.Invocations), p.Start.Format("15:04")))
		}
		lines = append(lines, hourLine)
		if st.Realigned {
			lines = append(lines, warn.Render("  CloudWatch returned UTC-hour buckets for this date — the day's edges are approximate"))
		}
	}

	// Log scan.
	if a.scan != nil {
		re := "/" + q.Pattern.String() + "/"
		if matchesAll(q.Pattern) {
			re = "all events"
		}
		if q.Filter != "" {
			re += " (filter " + q.Filter + ")"
		}
		prefix := "  "
		if a.scanning {
			prefix = "  " + mm.spinner.View() + " "
		}
		lines = append(lines, ansi.Truncate(prefix+"Log scan "+accent.Render(re)+"  "+a.scan.ScanSummary(), w, "…"))
		if note := a.scan.ScanNote(); note != "" {
			style := warn
			if a.scan.Err != nil {
				style = errS
			}
			lines = append(lines, style.Render(ansi.Truncate("  "+note, w, "…")))
		}
		if q.LogGroup != "/aws/lambda/"+q.Function {
			lines = append(lines, muted.Render(ansi.Truncate("  custom log group "+q.LogGroup+" — it may hold other functions' logs too", w, "…")))
		}
	}
	return strings.Join(lines, "\n")
}

// activityFooter shows the selected match in full: the request ID and stream,
// then the raw message hard-wrapped (up to three lines).
func (mm *m) activityFooter() string {
	m, ok := mm.selectedMatch()
	if !ok {
		return ""
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	w := max(mm.width-6, 20)
	id := dashEm(m.RequestID)
	if m.RequestIDInferred {
		id += " (from the stream's last START)"
	}
	meta := muted.Render(ansi.Truncate("  request "+id+" · "+m.Stream, w+2, "…"))
	wrapped := strings.Split(ansi.Wrap(oneLine(m.Message), w, ""), "\n")
	if len(wrapped) > 3 {
		wrapped = wrapped[:3]
		wrapped[2] = ansi.Truncate(wrapped[2], w-1, "") + "…"
	}
	for i := range wrapped {
		wrapped[i] = "  " + wrapped[i]
	}
	return meta + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Render(strings.Join(wrapped, "\n"))
}

func (mm *m) activityStatusLeft() string {
	a := &mm.act
	s := fmt.Sprintf("Activity: %s · %s", a.query.Function, a.query.Day.Date)
	if a.scan != nil {
		noun := "matches"
		if matchesAll(a.scan.Pattern) {
			noun = "events"
		}
		s += fmt.Sprintf("  ·  %d %s", len(a.scan.Matches), noun)
		if a.scanning {
			s += " · scanning…"
		}
	}
	return s
}

func (mm *m) activityHints() []ui.KeyHint {
	a := &mm.act
	hints := []ui.KeyHint{ui.H("↑/↓", "matches"), ui.H("[/]", "prev/next day"), ui.H("e", "edit query")}
	if a.scanning {
		hints = append(hints, ui.H("Esc", "stop scan"))
	} else {
		hints = append(hints, ui.H("Esc", "back"))
	}
	hints = append(hints, ui.H("y", "copy message"), ui.H("r", "rerun"))
	if hl, hr := a.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
	}
	return append(hints, ui.H("~", "debug"), ui.H("i", "about"), ui.H("q", "quit"))
}
