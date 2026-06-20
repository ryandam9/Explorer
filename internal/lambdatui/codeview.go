package lambdatui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// requestCode is the "v" action on a function detail: it gates the network
// download behind a confirmation, or explains why there is nothing to download
// (a container-image function, or a detail still loading).
func (mm *m) requestCode(cmds *[]tea.Cmd) {
	if mm.detailLoading {
		return
	}
	d := mm.detailFunc
	switch {
	case strings.EqualFold(d.PackageType, "Image"):
		mm.setToast("Container-image function — no downloadable source (see Image URI)")
		*cmds = append(*cmds, toastCmd(4*time.Second))
	case d.CodeLocation == "":
		mm.setToast("No downloadable code for this function")
		*cmds = append(*cmds, toastCmd(3*time.Second))
	default:
		mm.codeSizeNote = formatCodeSize(d.CodeSize)
		mm.codeConfirm = true
	}
}

// startCodeDownload accepts the confirmation and kicks off the package download.
func (mm *m) startCodeDownload(cmds *[]tea.Cmd) {
	d := mm.detailFunc
	mm.codeConfirm = false
	mm.codeActive = true
	mm.codeLoading = true
	mm.codeErr = nil
	mm.codeViewing = false
	mm.codeFiles = nil
	mm.codeFileName = ""
	mm.codeTitle = "Code — " + d.Name
	mm.codeKey = d.Region + "/" + d.Name
	*cmds = append(*cmds, mm.loadCodeCmd(mm.codeKey, d.CodeLocation), mm.spinner.Tick)
}

// loadCodeCmd downloads and unzips the deployment package off the UI goroutine.
func (mm *m) loadCodeCmd(key, url string) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Downloading Lambda deployment package", "function", key)
		ctx, cancel := context.WithTimeout(mm.ctx, codeDownloadTimeout)
		defer cancel()
		files, err := downloadCode(ctx, url)
		return codeMsg{key: key, files: files, err: err}
	}
}

// setCodeFiles loads the unzipped entries into the file-list table.
func (mm *m) setCodeFiles(files []codeFile) {
	mm.codeFiles = files
	mm.codeTbl = newLambdaTable(codeColumns())
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		rows = append(rows, table.Row{f.Name, codeLangLabel(f.Name), formatCodeSize(f.Size)})
	}
	mm.codeTbl.SetRows(rows)
	mm.codeTbl.SetCursor(0)
}

func codeColumns() []table.Column {
	return []table.Column{
		{Title: "File", Width: 50},
		{Title: "Type", Width: 8},
		{Title: "Size", Width: 10},
	}
}

// handleCodeKey routes keys while the code browser is open: scrolling the source
// viewer when a file is open, otherwise navigating the file list.
func (mm *m) handleCodeKey(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	if mm.codeViewing {
		switch msg.String() {
		case "q", "ctrl+c":
			*cmds = append(*cmds, tea.Quit)
		case "esc", "backspace", "left", "h":
			mm.codeViewing = false
		case "up", "k":
			mm.codeViewport.LineUp(1)
		case "down", "j":
			mm.codeViewport.LineDown(1)
		case "pgup":
			mm.codeViewport.LineUp(panelPageStep)
		case "pgdown", "pgdn", " ":
			mm.codeViewport.LineDown(panelPageStep)
		case "g", "home":
			mm.codeViewport.GotoTop()
		case "G", "end":
			mm.codeViewport.GotoBottom()
		case "y":
			mm.copyCodeFile(cmds)
		case ui.KeyAbout:
			mm.showAbout = true
		}
		return
	}

	switch msg.String() {
	case "q", "ctrl+c":
		*cmds = append(*cmds, tea.Quit)
	case "esc", "backspace", "left", "h":
		mm.codeActive = false
	case "up", "k":
		mm.codeTbl.MoveUp(1)
	case "down", "j":
		mm.codeTbl.MoveDown(1)
	case "g", "home":
		mm.codeTbl.GotoTop()
	case "G", "end":
		mm.codeTbl.GotoBottom()
	case "<", ",":
		mm.codeTbl.ScrollLeft()
	case ">", ".":
		mm.codeTbl.ScrollRight()
	case "enter", "right", "l":
		mm.openCodeFile()
	case "y":
		mm.copyCodeFile(cmds)
	case ui.KeyAbout:
		mm.showAbout = true
	}
}

// openCodeFile opens the selected file in the scrolling source viewer.
func (mm *m) openCodeFile() {
	i := mm.codeTbl.Cursor()
	if i < 0 || i >= len(mm.codeFiles) {
		return
	}
	mm.codeFileIdx = i
	mm.codeFileName = mm.codeFiles[i].Name
	mm.codeFileText = mm.codeDisplay(mm.codeFiles[i])
	w, h := mm.codeViewportSize()
	mm.codeViewport = viewport.New(w, h)
	mm.codeViewport.SetContent(lipgloss.NewStyle().Width(w).Render(mm.codeFileText))
	mm.codeViewing = true
}

// codeDisplay is the text shown for a file: syntax-highlighted source (via the
// shared ui.Highlight component), or a placeholder for binary/empty entries. The
// highlight is computed once here (not per frame) and only re-wrapped on render.
func (mm *m) codeDisplay(f codeFile) string {
	if f.Binary || len(f.Data) == 0 {
		return codeFileContent(f)
	}
	out := ui.Highlight(sanitizeCode(string(f.Data)), f.Name)
	if f.Truncated {
		out += fmt.Sprintf("\n\n… truncated — showing the first %s of %s",
			formatCodeSize(maxCodeFileBytes), formatCodeSize(f.Size))
	}
	return out
}

// copyCodeFile copies the selected/open file's text to the clipboard.
func (mm *m) copyCodeFile(cmds *[]tea.Cmd) {
	i := mm.codeTbl.Cursor()
	if mm.codeViewing {
		i = mm.codeFileIdx
	}
	if i < 0 || i >= len(mm.codeFiles) {
		return
	}
	f := mm.codeFiles[i]
	if f.Binary {
		mm.setToast("Binary file — not copied")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	_ = clipboard.WriteAll(string(f.Data))
	mm.setToast("Copied " + f.Name)
	*cmds = append(*cmds, toastCmd(3*time.Second))
}

// codeViewportSize is the source viewer's inner width/height.
func (mm *m) codeViewportSize() (w, h int) {
	w = mm.width - 6
	if w < 10 {
		w = 10
	}
	chrome := 5 // heading, filename, two border lines, status bar
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		chrome++
	}
	h = mm.height - chrome
	if h < 3 {
		h = 3
	}
	return w, h
}

// renderCodeLoading draws an animated, indeterminate progress indicator while
// the deployment package is fetched and unzipped: a spinner, a "scanner" bar
// whose lit window sweeps back and forth, and the size being pulled. The sweep
// phase comes from the wall clock, so it animates smoothly on every spinner tick
// regardless of the tick rate.
func (mm *m) renderCodeLoading() string {
	const (
		barW = 30
		win  = 7
	)
	span := barW - win
	// Triangle wave over [0, span] → ping-pong the lit window.
	ph := int(time.Now().UnixMilli()/80) % (2 * span)
	pos := ph
	if pos > span {
		pos = 2*span - pos
	}

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	bar := muted.Render(strings.Repeat("░", pos)) +
		accent.Render(strings.Repeat("█", win)) +
		muted.Render(strings.Repeat("░", barW-pos-win))

	size := mm.codeSizeNote
	if size == "" {
		size = "deployment package"
	}
	headline := fmt.Sprintf("  %s  Fetching %s", mm.spinner.View(), accent.Render(size))
	sub := muted.Render("  downloading & unzipping the function's source…")
	return headline + "\n\n  " + bar + "\n\n" + sub
}

// renderCode draws the code browser: a spinner/error while downloading, then the
// file list, or the source viewer when a file is open.
func (mm *m) renderCode() string {
	title := detailHeading(" " + mm.codeTitle)
	switch {
	case mm.codeLoading:
		return title + "\n\n" + mm.renderCodeLoading()
	case mm.codeErr != nil:
		return title + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load code: "+mm.codeErr.Error())
	case len(mm.codeFiles) == 0:
		return title + "\n\n  No files in the package."
	}
	if mm.codeViewing {
		return mm.renderCodeFile(title)
	}

	sub := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render(fmt.Sprintf("  %d files · Enter to open · y copies · Esc back", len(mm.codeFiles)))
	head := title + "\n" + sub
	mm.fitTable(&mm.codeTbl, lipgloss.Height(head), 1)
	return head + "\n" + ui.TablePanelStyle(true).Render(mm.codeTbl.View()) +
		"\n" + ui.TableScrollIndicator(&mm.codeTbl)
}

// renderCodeFile draws the scrolling source viewer for the open file. The
// viewport is rebuilt each frame (preserving the scroll offset) so it re-wraps
// to the current width on resize, mirroring the detail panels.
func (mm *m) renderCodeFile(title string) string {
	w, h := mm.codeViewportSize()
	off := mm.codeViewport.YOffset
	vp := viewport.New(w, h)
	vp.SetContent(lipgloss.NewStyle().Width(w).Render(mm.codeFileText))
	vp.SetYOffset(off)
	mm.codeViewport = vp

	bar := ui.VScrollbar(vp.Height, vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset)
	content := lipgloss.JoinHorizontal(lipgloss.Top, vp.View(), " ", bar)
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("  " + mm.codeFileName)
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(0, 1).
		Render(content)
	return title + "\n" + sub + "\n" + panel
}

// renderCodeConfirm is the modal shown before the network download.
func (mm *m) renderCodeConfirm() string {
	w := 64
	if mm.width > 0 && mm.width-8 < w {
		w = mm.width - 8
	}
	if w < 24 {
		w = 24
	}
	body := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render("Download deployment package?") + "\n\n" +
		fmt.Sprintf("Fetch the %s deployment package for\n%s over the network and browse its source.\n\n",
			mm.codeSizeNote, mm.detailFunc.Name) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("This is a read-only download from S3 (the function's own code).") + "\n\n" +
		"[y] download    [Esc] cancel"
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(1, 2).
		Width(w).
		Render(body)
}
