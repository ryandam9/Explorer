package uitest

import (
	"strings"
	"testing"
)

func TestCheckFrame(t *testing.T) {
	if p := CheckFrame("ab\ncd", 2, 2); len(p) != 0 {
		t.Errorf("fitting frame reported %v", p)
	}
	if p := CheckFrame("a\nb\nc", 5, 2); len(p) != 1 || !strings.Contains(p[0], "3 lines") {
		t.Errorf("tall frame = %v", p)
	}
	if p := CheckFrame("abcdef", 5, 2); len(p) != 1 || !strings.Contains(p[0], "6 cells") {
		t.Errorf("wide frame = %v", p)
	}
	// ANSI escapes do not count towards the width.
	if p := CheckFrame("\x1b[31mabc\x1b[0m", 3, 1); len(p) != 0 {
		t.Errorf("styled frame reported %v", p)
	}
}

func TestCheckBorders(t *testing.T) {
	ok := "╭──╮╭─╮\n│  ││ │\n╰──╯╰─╯"
	if p := CheckBorders(ok); len(p) != 0 {
		t.Errorf("closed panels reported %v", p)
	}
	clipped := "╭──╮╭──\n│  ││  \n╰──╯╰──"
	if p := CheckBorders(clipped); len(p) != 1 {
		t.Errorf("clipped panel = %v, want one problem", p)
	}
}

func TestRenderRecoversPanic(t *testing.T) {
	_, err := render(func(w, h int) string { panic("boom") }, 10, 10)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the panic", err)
	}
}
