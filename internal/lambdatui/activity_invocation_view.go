package lambdatui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The invocation view (Enter on a match): every line of the run the match
// belongs to, START to REPORT, read back from its log stream — so after
// finding "Copied obj-0001" the rest of that run (what it did before, the
// error after) is one key away. E in the report jumps between matches whose
// invocation failed.

type invocationView struct {
	active    bool
	loading   bool
	gen       int
	from      Match // the matched line it was opened from
	inv       Invocation
	truncated bool
	err       error
	tbl       table.Model
}

type invocationMsg struct {
	gen       int
	inv       Invocation
	truncated bool
	err       error
}

// openInvocation starts reading the selected match's invocation back from its
// stream. A line with no request ID (a bare print() before any START in the
// window read) can't be tied to a run.
func (mm *m) openInvocation(cmds *[]tea.Cmd) {
	a := &mm.act
	m, ok := mm.selectedMatch()
	if !ok {
		return
	}
	if m.RequestID == "" {
		mm.setToast("This line has no request ID — it can't be tied to an invocation")
		*cmds = append(*cmds, toastCmd(4*time.Second))
		return
	}
	v := &a.inv
	v.gen++
	*v = invocationView{active: true, loading: true, gen: v.gen, from: m,
		tbl: newLambdaTable(invocationColumns())}
	gen, q := v.gen, a.query
	timeout := time.Duration(a.fn.TimeoutSec) * time.Second
	ctx := mm.ctx
	*cmds = append(*cmds, mm.spinner.Tick, func() tea.Msg {
		slog.Info("Reading Lambda invocation", "function", q.Function, "requestId", m.RequestID, "stream", m.Stream)
		inv, truncated, err := mm.client.InvocationLines(ctx, q.Region, q.LogGroup, m.Stream, m.RequestID, m.Time, timeout)
		return invocationMsg{gen: gen, inv: inv, truncated: truncated, err: err}
	})
}

func (mm *m) handleInvocationMsg(msg invocationMsg) {
	v := &mm.act.inv
	if !v.active || msg.gen != v.gen {
		return
	}
	v.loading = false
	v.inv, v.truncated, v.err = msg.inv, msg.truncated, msg.err
	if msg.err != nil && msg.err != context.Canceled {
		slog.Warn("Lambda invocation read failed", "requestId", v.from.RequestID, "error", msg.err.Error())
	}
	mm.refreshInvocationRows()
}

func invocationColumns() []table.Column {
	return []table.Column{
		{Title: "TIME", Width: 12},
		{Title: "LEVEL", Width: 5},
		{Title: "MESSAGE", Width: 20},
	}
}

func (mm *m) refreshInvocationRows() {
	v := &mm.act.inv
	day := mm.act.query.Day
	rows := make([]table.Row, 0, len(v.inv.Lines))
	for _, l := range v.inv.Lines {
		level := dashEm(l.Level)
		if failureOf(parseLambdaLine(l.Message)) != "" {
			level = "✗ " + level
		}
		rows = append(rows, table.Row{matchTime(l.Time, day), level, l.Body})
	}
	setWrappedRows(&v.tbl, mm.width, invocationColumns(), rows)
	// Open on the line the user came from.
	for i, l := range v.inv.Lines {
		if l.Time.Equal(v.from.Time) && l.Message == v.from.Message {
			v.tbl.SetCursorGroup(i)
			break
		}
	}
}

// nextFailedMatch moves the report's cursor to the next match (after the
// current one, wrapping) whose invocation failed.
func (mm *m) nextFailedMatch(cmds *[]tea.Cmd) {
	a := &mm.act
	if a.scan == nil || a.scan.FailedCount() == 0 {
		mm.setToast("No failed invocations in the lines read")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	n := len(a.scan.Matches)
	cur := a.tbl.CursorGroup()
	for k := 1; k <= n; k++ {
		i := (cur + k) % n
		if a.scan.Failure(a.scan.Matches[i].RequestID) != "" {
			a.tbl.SetCursorGroup(i)
			return
		}
	}
	mm.setToast("No matched line belongs to a failed invocation — try an empty regex or (?i)error")
	*cmds = append(*cmds, toastCmd(4*time.Second))
}

func (mm *m) handleInvocationKey(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	v := &mm.act.inv
	switch msg.String() {
	case "q", "ctrl+c":
		*cmds = append(*cmds, tea.Quit)
	case "esc", "backspace", "left":
		v.active = false
		v.gen++ // drop a read still in flight
	case "up", "k":
		v.tbl.MoveUp(1)
	case "down", "j":
		v.tbl.MoveDown(1)
	case "pgup":
		v.tbl.MoveUp(max(v.tbl.Height()-1, 1))
	case "pgdown", "pgdn", " ":
		v.tbl.MoveDown(max(v.tbl.Height()-1, 1))
	case "g", "home":
		v.tbl.GotoTop()
	case "G", "end":
		v.tbl.GotoBottom()
	case "<", ",":
		v.tbl.ScrollLeft()
	case ">", ".":
		v.tbl.ScrollRight()
	case "y":
		if i := v.tbl.CursorGroup(); i >= 0 && i < len(v.inv.Lines) {
			_ = clipboard.WriteAll(v.inv.Lines[i].Message)
			mm.setToast("Copied log line")
			*cmds = append(*cmds, toastCmd(3*time.Second))
		}
	case "Y":
		if len(v.inv.Lines) > 0 {
			lines := make([]string, len(v.inv.Lines))
			for i, l := range v.inv.Lines {
				lines[i] = l.Message
			}
			_ = clipboard.WriteAll(strings.Join(lines, "\n"))
			mm.setToast(fmt.Sprintf("Copied the invocation's %d lines", len(lines)))
			*cmds = append(*cmds, toastCmd(3*time.Second))
		}
	case ui.KeyDebug:
		mm.debug.Open(mm.width, mm.height)
	case ui.KeyAbout:
		mm.showAbout = true
	}
}

func (mm *m) renderInvocation() string {
	v := &mm.act.inv
	head := mm.invocationHeader()
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	switch {
	case v.loading:
		return head + "\n\n  " + mm.spinner.View() + " reading the invocation from its log stream…"
	case v.err != nil:
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not read the invocation: "+v.err.Error())
	case len(v.inv.Lines) == 0:
		return head + "\n\n  " + muted.Render("No lines of this invocation were found in its stream.")
	}
	mm.fitTable(&v.tbl, lipgloss.Height(head), 0)
	return head + "\n" + ui.TablePanel(&v.tbl, true, "Log lines")
}

func (mm *m) invocationHeader() string {
	v := &mm.act.inv
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
	errS := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
	w := max(mm.width-2, 20)
	day := mm.act.query.Day

	lines := []string{heading.Render(ansi.Truncate(" Invocation "+v.from.RequestID+" · "+mm.act.query.Function, w, "…"))}
	lines = append(lines, muted.Render(ansi.Truncate("  stream "+v.from.Stream, w, "…")))
	if v.loading || v.err != nil {
		return strings.Join(lines, "\n")
	}
	inv := v.inv
	if len(inv.Lines) > 0 {
		span := fmt.Sprintf("  %s → %s  ·  %d lines", matchTime(inv.Lines[0].Time, day), matchTime(inv.Lines[len(inv.Lines)-1].Time, day), len(inv.Lines))
		lines = append(lines, span)
	}
	if r := inv.Report; r != nil {
		lines = append(lines, "  "+reportSummary(*r, mm.act.fn.TimeoutSec))
	}
	if inv.Failure != "" {
		lines = append(lines, errS.Render("  ✗ this invocation "+inv.Failure))
	}
	var notes []string
	if !inv.HasStart {
		notes = append(notes, "its START line was not in the window read")
	}
	if !inv.HasEnd {
		notes = append(notes, "no REPORT line yet — still running, or it ended outside the window read")
	}
	if v.truncated {
		notes = append(notes, fmt.Sprintf("stopped after %d log pages", invocationMaxPages))
	}
	if len(notes) > 0 {
		lines = append(lines, warn.Render(ansi.Truncate("  "+strings.Join(notes, " · "), w, "…")))
	}
	return strings.Join(lines, "\n")
}

// reportSummary renders a REPORT line's figures for a header: duration
// against the timeout, memory against the size, and the cold start.
func reportSummary(r InvocationReport, timeoutSec int32) string {
	s := fmt.Sprintf("duration %s (billed %s)", formatMs(r.DurationMs), formatMs(r.BilledMs))
	if timeoutSec > 0 {
		s += fmt.Sprintf(" of %ds timeout (%.0f%%)", timeoutSec, 100*r.DurationMs/(float64(timeoutSec)*1000))
	}
	if r.MemorySizeMB > 0 {
		s += fmt.Sprintf("  ·  memory %.0f / %.0f MB (%.0f%%)", r.MaxMemoryMB, r.MemorySizeMB, 100*r.MaxMemoryMB/r.MemorySizeMB)
	}
	if r.ColdStart {
		s += fmt.Sprintf("  ·  cold start (init %s)", formatMs(r.InitMs))
	}
	if r.Status != "" && r.Status != "success" {
		s += "  ·  status " + r.Status
	}
	return s
}

// formatMs renders milliseconds compactly: 950 ms, 1.95 s, 2m05s.
func formatMs(ms float64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%.0f ms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.2f s", ms/1000)
	default:
		d := time.Duration(ms) * time.Millisecond
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

func (mm *m) invocationStatusLeft() string {
	v := &mm.act.inv
	id := v.from.RequestID
	if i := strings.IndexByte(id, '-'); i > 0 {
		id = id[:i]
	}
	s := "Invocation " + id
	if !v.loading && v.err == nil {
		s += fmt.Sprintf("  ·  %d lines", len(v.inv.Lines))
	}
	return s
}

func (mm *m) invocationHints() []ui.KeyHint {
	return []ui.KeyHint{ui.H("↑/↓", "lines"), ui.H("Esc", "back to matches"), ui.H("y", "copy line"),
		ui.H("Y", "copy all"), ui.H("~", "debug"), ui.H("q", "quit")}
}
