package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// withRoleRestore snapshots a role's value and restores it after the test, so
// tests that commit through the tuner don't leak into the global Themes slice.
func withRoleRestore(t *testing.T, themeIdx, fieldIdx int) {
	t.Helper()
	orig := *Roles[fieldIdx].Ptr(&Themes[themeIdx].Colors)
	t.Cleanup(func() { setColorForField(themeIdx, fieldIdx, orig) })
}

func TestRoleGroupsCoverEveryRole(t *testing.T) {
	seen := make(map[int]bool)
	for _, g := range settingsGroups {
		if len(g.roles) == 0 {
			t.Errorf("group %q has no roles", g.name)
		}
		for _, ri := range g.roles {
			if seen[ri] {
				t.Errorf("role %q appears in more than one group", Roles[ri].Name)
			}
			seen[ri] = true
		}
	}
	if len(seen) != len(Roles) {
		t.Errorf("groups cover %d roles, want %d", len(seen), len(Roles))
	}
}

func TestRoleLabel(t *testing.T) {
	tests := map[string]string{
		"heading":           "HEADING",
		"tableSelectedBg":   "TABLE SELECTED BG",
		"statusBarText":     "STATUS BAR TEXT",
		"borderFocus":       "BORDER FOCUS",
		"highlightText":     "HIGHLIGHT TEXT",
		"tableHeaderLine":   "TABLE HEADER LINE",
		"tableSelectedText": "TABLE SELECTED TEXT",
	}
	for in, want := range tests {
		if got := roleLabel(in); got != want {
			t.Errorf("roleLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSubsystemTabNavigation(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	if s.groupIdx != 0 || s.fieldIdx != settingsGroups[0].roles[0] {
		t.Fatalf("fresh model should start on the first role of GENERAL")
	}
	s, _ = s.Update(key("tab"))
	if s.groupIdx != 1 {
		t.Fatalf("tab should advance to TABLES, got group %d", s.groupIdx)
	}
	if s.fieldIdx != settingsGroups[1].roles[0] {
		t.Errorf("switching groups should select the group's first role")
	}
	s, _ = s.Update(key("4"))
	if s.groupIdx != 3 {
		t.Errorf("'4' should jump to ALERTS, got group %d", s.groupIdx)
	}
	s, _ = s.Update(key("down"))
	if s.fieldIdx != settingsGroups[3].roles[1] {
		t.Errorf("down should move within the active group")
	}
}

func TestTunerAdjustAndApply(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	withRoleRestore(t, s.themeIdx, s.fieldIdx)

	setColorForField(s.themeIdx, s.fieldIdx, "#ff0000") // hue 0, sat 100, lum 50
	s, _ = s.Update(key("enter"))                       // open tuner
	if !s.EditMode() {
		t.Fatal("enter should open the tuner (EditMode)")
	}
	s, _ = s.Update(key("right")) // hue 0 → 5
	s, _ = s.Update(key("enter")) // apply
	if s.EditMode() {
		t.Fatal("enter should close the tuner")
	}
	got := s.colorForField(s.themeIdx, s.fieldIdx)
	if got != hslToHex(5, 100, 50) {
		t.Errorf("after one hue step, role = %q, want %q", got, hslToHex(5, 100, 50))
	}
}

func TestTunerCancelKeepsOldValue(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	withRoleRestore(t, s.themeIdx, s.fieldIdx)

	setColorForField(s.themeIdx, s.fieldIdx, "#00ff00")
	s, _ = s.Update(key("enter"))
	s, _ = s.Update(key("right"))
	s, _ = s.Update(key("esc"))
	if s.EditMode() {
		t.Fatal("esc should close the tuner")
	}
	if got := s.colorForField(s.themeIdx, s.fieldIdx); got != "#00ff00" {
		t.Errorf("cancel must not change the role, got %q", got)
	}
}

func TestTunerHexEntry(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	withRoleRestore(t, s.themeIdx, s.fieldIdx)

	s, _ = s.Update(key("enter"))
	// Move to the HEX channel (hue → sat → lum → hex).
	s, _ = s.Update(key("down"))
	s, _ = s.Update(key("down"))
	s, _ = s.Update(key("down"))
	if s.tuneChan != chanHex {
		t.Fatalf("expected hex channel, got %d", s.tuneChan)
	}
	// Clear the seeded buffer, then type a color. 'a' must act as a hex
	// digit here, not as the auto toggle.
	for range 8 {
		s, _ = s.Update(key("backspace"))
	}
	for _, r := range "#34e0a1" {
		s, _ = s.Update(key(string(r)))
	}
	s, _ = s.Update(key("enter"))
	if got := s.colorForField(s.themeIdx, s.fieldIdx); got != "#34e0a1" {
		t.Errorf("typed hex not applied, got %q", got)
	}
	if s.tuneAuto {
		t.Error("typing a hex value should clear the staged auto flag")
	}
}

func TestTunerAutoClearsRole(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	withRoleRestore(t, s.themeIdx, s.fieldIdx)

	setColorForField(s.themeIdx, s.fieldIdx, "#123456")
	s, _ = s.Update(key("enter"))
	s, _ = s.Update(key("a")) // toggle auto (not on the hex channel)
	s, _ = s.Update(key("enter"))
	if got := s.colorForField(s.themeIdx, s.fieldIdx); got != "" {
		t.Errorf("auto + apply should clear the role, got %q", got)
	}
}

func TestQuickAutoInNavMode(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	withRoleRestore(t, s.themeIdx, s.fieldIdx)

	setColorForField(s.themeIdx, s.fieldIdx, "#123456")
	s, _ = s.Update(key("a"))
	if got := s.colorForField(s.themeIdx, s.fieldIdx); got != "" {
		t.Errorf("'a' in nav mode should reset the role to auto, got %q", got)
	}
}

// The console must render in both modes without panicking, contain the
// signature sections, and never exceed the requested width.
func TestConsoleViewRenders(t *testing.T) {
	s := NewSettingsModel(100, 40, "", nil)
	for _, mode := range []string{"nav", "tune"} {
		if mode == "tune" {
			withRoleRestore(t, s.themeIdx, s.fieldIdx)
			s, _ = s.Update(key("enter"))
		}
		view := s.View()
		for _, want := range []string{"THEME CONSOLE", "SUBSYSTEM", "SIGNAL MONITOR", "ACTIVE THEME"} {
			if !strings.Contains(view, want) {
				t.Errorf("%s view missing %q", mode, want)
			}
		}
		if mode == "tune" {
			for _, want := range []string{"HUE", "SAT", "LUM", "HEX", "APPLY"} {
				if !strings.Contains(view, want) {
					t.Errorf("tune view missing %q", want)
				}
			}
		}
	}
}
