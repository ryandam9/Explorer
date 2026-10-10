package vpctui

import (
	"strings"
	"testing"
)

// The help overlay lists only the current screen's shortcuts: the
// resource-browser keys do nothing on the VPC list, and vice versa.
func TestHelpTextIsPerScreen(t *testing.T) {
	m := &Model{state: stateVPCList}
	list := m.helpText()
	for _, want := range []string{"Open resource browser", "Refresh VPC list", "Quit"} {
		if !strings.Contains(list, want) {
			t.Errorf("VPC list help missing %q", want)
		}
	}
	for _, unwanted := range []string{"Export a Markdown report", "findings linter", "Resource browser"} {
		if strings.Contains(list, unwanted) {
			t.Errorf("VPC list help should not show %q", unwanted)
		}
	}

	m.state = stateResourceBrowser
	browser := m.helpText()
	for _, want := range []string{"Export a Markdown report", "findings linter", "Go back to VPC list", "Quit"} {
		if !strings.Contains(browser, want) {
			t.Errorf("resource browser help missing %q", want)
		}
	}
	if strings.Contains(browser, "Refresh VPC list") {
		t.Error("resource browser help should not show VPC-list keys")
	}
	if m.helpTitle() == (&Model{state: stateVPCList}).helpTitle() {
		t.Error("help title should name the screen")
	}
}
