package sqstui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

func (m *model) View() string {
	if m.err != nil {
		return m.debug.Overlay(m.renderErrorView(), m.width, m.height)
	}

	var sb strings.Builder

	// Spotlight the active region scope when not in all-regions mode, so a
	// single-region session can't be mistaken for the whole account.
	if badge := ui.RegionBadge(m.regions, m.allRegions); badge != "" {
		sb.WriteString(badge + "\n")
	}

	sidebarW := 42
	contentW := m.width - sidebarW - 4
	if contentW < 20 {
		contentW = 20
	}

	sidebar := m.renderSidebar(sidebarW)
	var content string
	if m.view == viewMessages {
		content = m.renderMessagesPanel(contentW)
	} else {
		content = m.renderOverviewPanel(contentW)
	}

	sb.WriteString(lipgloss.JoinHorizontal(
		lipgloss.Top,
		sidebar,
		lipgloss.NewStyle().Width(2).Render(" "),
		content,
	) + "\n")

	regionLabel := "all (" + fmt.Sprintf("%d", len(m.regions)) + " regions)"
	if len(m.regions) == 1 {
		regionLabel = m.regions[0]
	}
	statusText := fmt.Sprintf("Region: %s  ·  Queues: %d", regionLabel, len(m.filtered))
	if m.view == viewMessages {
		statusText += fmt.Sprintf("  ·  Sampled: %d messages", len(m.messages))
	}
	if m.peekLoading {
		statusText += "  ·  Peeking…"
	}
	if m.jumpLoading {
		statusText += "  ·  Looking up consumers…"
	}
	sb.WriteString(ui.StatusBar(m.width, statusText, m.getHelpHints()))

	frame := m.applyToast(sb.String())
	switch {
	case m.showAbout:
		frame = ui.OverlayCenterBlank(ui.AboutView("About — SQS Queues", sqsAboutText, ui.AboutWidth(m.width)), m.width, m.height)
	case m.showHelp:
		frame = ui.OverlayCenterBlank(m.helpOverlay(), m.width, m.height)
	case m.recordActive:
		frame = ui.OverlayCenterBlank(m.renderMessageRecord(), m.width, m.height)
	case m.peekConfirm:
		frame = ui.OverlayCenterBlank(m.renderPeekConfirm(), m.width, m.height)
	}
	return m.debug.Overlay(frame, m.width, m.height)
}

// sqsAboutText explains what the SQS TUI is for, shown in the About overlay.
const sqsAboutText = "This is the SQS queue explorer. The sidebar lists every queue your " +
	"credentials can see across the selected regions; the panel shows the " +
	"selected queue's attributes, tags, and dead-letter relationships (d jumps " +
	"to the DLQ).\n\n" +
	"P peeks at a sample of the queue's visible messages. Peeking never " +
	"deletes anything and returns visibility immediately, but SQS still " +
	"increments each sampled message's receive count — on a queue with a " +
	"redrive policy that moves messages closer to the DLQ, which is why the " +
	"peek asks for confirmation first.\n\n" +
	"m shows CloudWatch metric sparklines (refreshes are floored to once a " +
	"minute — GetMetricData is a paid API), and L opens the CloudWatch Logs " +
	"explorer for the queue's Lambda consumer, when one exists.\n\n" +
	"The status bar shows the keys usable right now; ? opens the full key " +
	"reference."

func (m *model) renderSidebar(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	b.WriteString(headingStyle.Render(" Queues") + "\n")

	if m.searchActive {
		b.WriteString(" " + m.search.View() + "\n")
	} else {
		b.WriteString("  (Press / to filter)\n")
	}
	b.WriteString("\n")

	if m.queuesLoading {
		b.WriteString(fmt.Sprintf("  %s Loading queues…\n", m.spinner.View()))
	} else if len(m.filtered) == 0 {
		b.WriteString("  No queues found.\n")
	} else {
		visibleHeight := m.height - 8
		if visibleHeight < 5 {
			visibleHeight = 5
		}
		start, end := getVisibleRange(m.selectedIdx, len(m.filtered), visibleHeight)

		multiRegion := len(m.regions) > 1
		metaW := 8
		if multiRegion {
			metaW = 14
		}

		for i := start; i < end; i++ {
			q := m.filtered[i]
			name := q.Name
			maxNameW := width - metaW - 5
			if len(name) > maxNameW {
				name = "..." + name[len(name)-maxNameW+3:]
			}

			// Meta column: region when several are in scope, otherwise the
			// approximate depth for queues whose detail has been fetched.
			// Blank means "not fetched", never 0.
			meta := ""
			if multiRegion {
				meta = q.Region
			} else if d, ok := m.details[detailKey(q)]; ok && d.Attrs != nil {
				meta = attrCount(d.Attrs, "ApproximateNumberOfMessages")
			}

			item := fmt.Sprintf(" %-*s %*s", maxNameW, name, metaW, meta)
			if i == m.selectedIdx {
				b.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(ui.ColorHighlight())).
					Foreground(lipgloss.Color(ui.ColorHighlightText())).
					Render("> "+item) + "\n")
			} else {
				b.WriteString("  " + item + "\n")
			}
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Width(width).
		Height(m.height - 4).
		Render(b.String())
}

// renderOverviewPanel shows the selected queue's attributes, tags, redrive
// relationships and (when toggled) metric sparklines.
func (m *model) renderOverviewPanel(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))

	q, ok := m.selectedQueue()
	if !ok {
		b.WriteString(headingStyle.Render(" Queue overview") + "\n\n")
		if !m.queuesLoading {
			b.WriteString("  Select a queue in the sidebar.\n")
		}
		return m.panelBox(width, b.String())
	}

	title := " Queue: " + q.Name + " [" + q.Region + "]"
	if len(title) > width-4 {
		title = title[:max(0, width-7)] + "..."
	}
	b.WriteString(headingStyle.Render(title) + "\n\n")

	wrapW := max(20, width-4)
	writeWrapped := func(line string) {
		for _, l := range wrapLine(sanitizeLine(line), wrapW, "    ") {
			b.WriteString(" " + l + "\n")
		}
	}

	writeWrapped("URL: " + q.URL)

	d, haveDetail := m.details[detailKey(q)]
	switch {
	case m.detailLoading && !haveDetail:
		b.WriteString(fmt.Sprintf("\n  %s Loading queue attributes…\n", m.spinner.View()))
	case haveDetail && d.AttrsErr != nil:
		// A failed read is a failed read — never rendered as empty values.
		writeWrapped("")
		b.WriteString(" " + errStyle.Render("Couldn't read attributes: "+d.AttrsErr.Error()) + "\n")
		b.WriteString(" " + mutedStyle.Render("(r to retry)") + "\n")
	case haveDetail && d.Attrs != nil:
		attrs := d.Attrs
		writeWrapped("ARN: " + attrs["QueueArn"])
		b.WriteString("\n")

		qType := "Standard"
		if isFifo(attrs) {
			qType = "FIFO"
			if strings.EqualFold(attrs["ContentBasedDeduplication"], "true") {
				qType += " (content-based dedup)"
			}
		}
		writeWrapped(fmt.Sprintf("Messages: %s visible · %s in flight · %s delayed",
			attrCount(attrs, "ApproximateNumberOfMessages"),
			attrCount(attrs, "ApproximateNumberOfMessagesNotVisible"),
			attrCount(attrs, "ApproximateNumberOfMessagesDelayed")))
		b.WriteString(" " + mutedStyle.Render(fmt.Sprintf("(approximate; fetched %s — r refreshes)", d.FetchedAt.Format("15:04:05"))) + "\n")
		writeWrapped("Type: " + qType + " · Encryption: " + encryptionLabel(attrs))
		writeWrapped(fmt.Sprintf("Visibility timeout: %s · Retention: %s · Max size: %s",
			attrSeconds(attrs, "VisibilityTimeout"),
			attrSeconds(attrs, "MessageRetentionPeriod"),
			attrBytes(attrs, "MaximumMessageSize")))
		writeWrapped(fmt.Sprintf("Delivery delay: %s · Receive wait: %s",
			attrSeconds(attrs, "DelaySeconds"),
			attrSeconds(attrs, "ReceiveMessageWaitTimeSeconds")))
		writeWrapped("Created: " + attrEpoch(attrs, "CreatedTimestamp") + " · Modified: " + attrEpoch(attrs, "LastModifiedTimestamp"))

		b.WriteString("\n")
		if rd, ok := parseRedrive(attrs["RedrivePolicy"]); ok {
			writeWrapped(fmt.Sprintf("Redrive → %s after %d receives", queueNameFromARN(rd.TargetARN), rd.MaxReceiveCount))
			b.WriteString(" " + mutedStyle.Render("(d opens the DLQ)") + "\n")
		} else {
			writeWrapped("Redrive: none (no DLQ configured)")
		}
	}

	if haveDetail {
		if d.SourcesErr != nil {
			b.WriteString(" " + errStyle.Render("Couldn't list DLQ sources: "+d.SourcesErr.Error()) + "\n")
		} else if len(d.Sources) > 0 {
			const maxShown = 8
			names := make([]string, 0, min(len(d.Sources), maxShown))
			for i, src := range d.Sources {
				if i >= maxShown {
					break
				}
				names = append(names, queueNameFromURL(src))
			}
			line := "DLQ for: " + strings.Join(names, ", ")
			if extra := len(d.Sources) - maxShown; extra > 0 {
				line += fmt.Sprintf(" (+%d more)", extra)
			}
			writeWrapped(line)
		}

		b.WriteString("\n")
		switch {
		case d.TagsErr != nil:
			b.WriteString(" " + errStyle.Render("Couldn't read tags: "+d.TagsErr.Error()) + "\n")
		case len(d.Tags) > 0:
			pairs := make([]string, 0, len(d.Tags))
			for k, v := range d.Tags {
				pairs = append(pairs, k+"="+v)
			}
			// Map order is random; sort for a stable display.
			sort.Strings(pairs)
			writeWrapped("Tags: " + strings.Join(pairs, " · "))
		default:
			b.WriteString(" " + mutedStyle.Render("No tags") + "\n")
		}
	}

	if m.metricsVisible {
		b.WriteString("\n" + headingStyle.Render(" Metrics (3h)") + "\n")
		key := detailKey(q)
		entry, haveMetrics := m.metrics[key]
		switch {
		case m.metricsLoading && !haveMetrics:
			b.WriteString(fmt.Sprintf("  %s Loading metrics…\n", m.spinner.View()))
		case haveMetrics && entry.err != nil:
			b.WriteString(" " + errStyle.Render("Couldn't read metrics: "+entry.err.Error()) + "\n")
		case haveMetrics:
			for _, s := range entry.series {
				if len(s.Values) == 0 {
					// No datapoints is "no data", not a flat zero line.
					b.WriteString(fmt.Sprintf("  %-20s %s\n", s.Label, mutedStyle.Render("no data")))
					continue
				}
				last := s.Values[len(s.Values)-1]
				b.WriteString(fmt.Sprintf("  %-20s %s  %s\n", s.Label,
					accent.Render(awsutil.GenerateSparkline(s.Values)),
					formatMetricValue(last)))
			}
			b.WriteString(" " + mutedStyle.Render(fmt.Sprintf("(fetched %s; m refreshes, floored to 1m — paid API)", entry.fetchedAt.Format("15:04:05"))) + "\n")
		}
	}

	return m.panelBox(width, b.String())
}

// renderMessagesPanel shows the sampled messages through the shared table.
func (m *model) renderMessagesPanel(width int) string {
	var b strings.Builder

	headingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	title := " Messages: " + m.peekQueue.Name + " [" + m.peekQueue.Region + "]"
	if len(title) > width-4 {
		title = title[:max(0, width-7)] + "..."
	}
	b.WriteString(headingStyle.Render(title) + "\n")
	// A sample must never read as the whole queue (§ no silent caps).
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  Sample of %d visible messages — not the whole queue; order not guaranteed. Receive counts were incremented by this peek.", len(m.messages))) + "\n")
	b.WriteString("\n")

	if len(m.messages) == 0 {
		b.WriteString("  The peek returned no messages. The queue may be empty, or all messages\n")
		b.WriteString("  may currently be in flight (invisible) or delayed. P re-peeks.\n")
	} else {
		hdrLines := strings.Count(b.String(), "\n")
		tableH := (m.height - 4) - hdrLines - 1
		if tableH < 3 {
			tableH = 3
		}
		m.msgTable.SetWidth(width - 2)
		m.msgTable.SetHeight(tableH)
		b.WriteString(m.msgTable.View() + "\n")
		if s := ui.TableScrollIndicator(&m.msgTable); s != "" {
			b.WriteString(" " + s)
		}
	}

	return m.panelBox(width, b.String())
}

// renderPeekConfirm is the consent gate for the message peek, stating exactly
// what the peek does and does not do — including the receive-count side
// effect and its DLQ implication when the queue has a redrive policy.
func (m *model) renderPeekConfirm() string {
	q, _ := m.selectedQueue()

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorHeading())).Bold(true).Render("Peek messages — "+q.Name) + "\n\n")
	b.WriteString(fmt.Sprintf("Sample up to %d visible messages from this queue.\n\n", peekMaxMessages))
	b.WriteString("• Messages are NOT deleted, and visibility is returned immediately\n")
	b.WriteString("  (VisibilityTimeout=0), so consumers are not starved.\n")
	b.WriteString("• SQS still increments each sampled message's receive count.\n")

	if d, ok := m.details[detailKey(q)]; ok && d.Attrs != nil {
		if rd, ok := parseRedrive(d.Attrs["RedrivePolicy"]); ok {
			warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))
			b.WriteString(warn.Render(fmt.Sprintf("• This queue redrives to %s after %d receives — repeated peeks\n  move messages closer to the DLQ.", queueNameFromARN(rd.TargetARN), rd.MaxReceiveCount)) + "\n")
		}
		if isFifo(d.Attrs) {
			b.WriteString("• FIFO queue: a peek can momentarily hold back a message group\n  from other consumers.\n")
		}
	}

	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("Enter/y: peek · any other key: cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorWarning())).
		Padding(1, 2).
		Render(b.String())
}

// renderMessageRecord renders the message record overlay panel.
func (m *model) renderMessageRecord() string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true).
		Render("Message record")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.recordVP.View())
	return lipgloss.NewStyle().
		Width(m.recordOverlayWidth()+4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Foreground(lipgloss.Color(ui.ColorText())).
		Padding(1, 2).
		Render(body)
}

func (m *model) renderErrorView() string {
	var b strings.Builder
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).Render("  SQS explorer exception") + "\n\n")
	b.WriteString(fmt.Sprintf("  An error occurred: %v\n\n", m.err))
	b.WriteString("  Shortcuts:\n")
	b.WriteString("    Enter, Esc  - Attempt to return or retry\n")
	b.WriteString("    q, Ctrl+C   - Quit\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorError())).
		Width(m.width - 4).
		Height(m.height - 4).
		Render(b.String())
}

func (m *model) panelBox(width int, content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorder())).
		Width(width).
		Height(m.height - 4).
		Render(content)
}

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
		lines[1] = lipgloss.PlaceHorizontal(m.width, lipgloss.Right, toastRendered)
		rendered = strings.Join(lines, "\n")
	}
	return rendered
}

func (m *model) getHelpHints() []ui.KeyHint {
	if m.recordActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "scroll"),
			ui.H("y", "copy record"),
			ui.H("Esc", "close"),
		}
	}
	if m.peekConfirm {
		return []ui.KeyHint{
			ui.H("Enter/y", "peek"),
			ui.H("Esc", "cancel"),
		}
	}
	if m.view == viewMessages {
		return []ui.KeyHint{
			ui.H("↑/↓", "messages"),
			ui.H("v", "record"),
			ui.H("y", "copy body"),
			ui.H("s", "export"),
			ui.H("P", "re-peek"),
			ui.H("Esc", "back"),
			ui.H("?", "help"),
			ui.H("q", "quit"),
		}
	}
	return []ui.KeyHint{
		ui.H("↑/↓", "queues"),
		ui.H("/", "filter"),
		ui.H("P", "peek messages"),
		ui.H("d", "open DLQ"),
		ui.H("m", "metrics"),
		ui.H("L", "consumer logs"),
		ui.H("o", "console"),
		ui.H("y", "copy URL"),
		ui.H("r", "refresh"),
		ui.H("?", "help"),
		ui.H("~", "debug"),
		ui.H("i", "about"),
		ui.H("q", "quit"),
	}
}

// formatMetricValue renders a metric's most recent value compactly.
func formatMetricValue(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
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
