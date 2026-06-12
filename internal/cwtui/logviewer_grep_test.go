package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func newGrepViewer() *logViewer {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}
	v.search = textinput.New()
	v.grepInput = textinput.New()
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "INFO user login ok"),
		testEvent("e2", 2000, "ERROR db timeout\n    retrying in 5s"),
		testEvent("e3", 3000, "INFO heartbeat"),
	})
	return v
}

func TestGrepFiltersLines(t *testing.T) {
	v := newGrepViewer()
	if len(v.lines) != 4 {
		t.Fatalf("expected 4 unfiltered lines, got %d: %v", len(v.lines), v.lines)
	}

	v.setGrep("ERROR|retrying")
	if len(v.lines) != 2 {
		t.Fatalf("expected 2 matching lines, got %d: %v", len(v.lines), v.lines)
	}
	joined := strings.Join(v.lines, "\n")
	if !strings.Contains(joined, "ERROR db timeout") || !strings.Contains(joined, "retrying in 5s") {
		t.Fatalf("wrong lines kept: %v", v.lines)
	}
	if strings.Contains(joined, "INFO") {
		t.Fatalf("non-matching lines must be dropped: %v", v.lines)
	}
	if v.grepTotal != 4 || len(v.grepSrc) != 2 {
		t.Fatalf("kept/total = %d/%d, want 2/4", len(v.grepSrc), v.grepTotal)
	}

	// Clearing restores everything.
	v.setGrep("")
	if len(v.lines) != 4 || v.grepRe != nil {
		t.Fatalf("expected all lines back after clear, got %d", len(v.lines))
	}
}

func TestGrepInvalidRegexKeepsLastFilter(t *testing.T) {
	v := newGrepViewer()
	v.setGrep("ERROR")
	if len(v.lines) != 1 {
		t.Fatalf("expected 1 line for ERROR, got %v", v.lines)
	}

	// A half-typed pattern must not blank or unfilter the view.
	v.setGrep("ERROR(")
	if v.grepErr == "" {
		t.Fatal("invalid regex should set grepErr")
	}
	if len(v.lines) != 1 || v.grepRe.String() != "ERROR" {
		t.Fatalf("last valid filter must stay applied, got %v", v.lines)
	}

	// Completing the pattern applies it and clears the error. Three logical
	// lines carry ERROR or INFO; the "retrying" continuation has neither.
	v.setGrep("ERROR|INFO")
	if v.grepErr != "" || len(v.lines) != 3 {
		t.Fatalf("expected 3 lines for ERROR|INFO, got %d (err %q)", len(v.lines), v.grepErr)
	}
}

func TestGrepKeepsWrappedContinuations(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 30}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "ERROR "+strings.Repeat("x", 60)),
		testEvent("e2", 2000, "INFO short"),
	})
	v.setGrep("ERROR")
	// The matching logical line wraps to several display lines; all of them
	// must survive the filter even though only the first contains "ERROR".
	if len(v.lines) < 2 {
		t.Fatalf("wrapped continuations of a matching line were dropped: %v", v.lines)
	}
	for _, l := range v.lines {
		if strings.Contains(l, "INFO") {
			t.Fatalf("non-matching line leaked through: %v", v.lines)
		}
	}
	if len(v.grepSrc) != 1 || !strings.Contains(v.grepSrc[0], "ERROR") {
		t.Fatalf("grepSrc must hold the unwrapped matching line, got %v", v.grepSrc)
	}
}

func TestGrepStreamedEventsStayFiltered(t *testing.T) {
	v := newGrepViewer()
	v.setGrep("ERROR")
	v.append([]types.FilteredLogEvent{
		testEvent("e4", 4000, "ERROR again"),
		testEvent("e5", 5000, "INFO noise"),
	})
	joined := strings.Join(v.lines, "\n")
	if !strings.Contains(joined, "ERROR again") || strings.Contains(joined, "INFO noise") {
		t.Fatalf("streamed events must be filtered too: %v", v.lines)
	}
}

func TestGrepKeyFlow(t *testing.T) {
	m := &model{height: 40, viewer: *newGrepViewer()}
	m.viewer.active = true

	var cmds []tea.Cmd
	m.handleViewerKeys(key("&"), &cmds)
	if !m.viewer.grepActive {
		t.Fatal("& must open the grep input")
	}
	for _, ch := range "ERROR" {
		m.handleViewerKeys(key(string(ch)), &cmds)
	}
	if len(m.viewer.lines) != 1 {
		t.Fatalf("live filtering expected 1 line, got %v", m.viewer.lines)
	}
	m.handleViewerKeys(tea.KeyMsg{Type: tea.KeyEnter}, &cmds)
	if m.viewer.grepActive || m.viewer.grepRe == nil {
		t.Fatal("enter must keep the filter and leave input mode")
	}

	// Esc inside the grep input clears the filter.
	m.handleViewerKeys(key("&"), &cmds)
	m.handleViewerKeys(tea.KeyMsg{Type: tea.KeyEscape}, &cmds)
	if m.viewer.grepRe != nil || len(m.viewer.lines) != 4 {
		t.Fatalf("esc must clear the filter, got %d lines", len(m.viewer.lines))
	}
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
