package cmd

import "testing"

func TestRelatedTUIFlagError(t *testing.T) {
	cases := []struct {
		name                  string
		tui, depth, direction bool
		wantErr               bool
	}{
		{"non-tui ignores both", false, true, true, false},
		{"tui, no flags set", true, false, false, false},
		{"tui with --depth", true, true, false, true},
		{"tui with --direction", true, false, true, true},
		{"tui with both", true, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := relatedTUIFlagError(c.tui, c.depth, c.direction)
			if (err != nil) != c.wantErr {
				t.Errorf("relatedTUIFlagError(%v,%v,%v) err=%v, wantErr=%v",
					c.tui, c.depth, c.direction, err, c.wantErr)
			}
		})
	}
}

func TestParseDirection(t *testing.T) {
	cases := []struct {
		in                   string
		wantUses, wantUsedBy bool
		wantErr              bool
	}{
		{"", true, true, false},
		{"both", true, true, false},
		{"uses", true, false, false},
		{"forward", true, false, false},
		{"usedby", false, true, false},
		{"used-by", false, true, false},
		{"reverse", false, true, false},
		{"USES", true, false, false}, // case-insensitive
		{"bogus", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			uses, usedBy, err := parseDirection(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseDirection(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
			if err != nil {
				return
			}
			if uses != c.wantUses || usedBy != c.wantUsedBy {
				t.Errorf("parseDirection(%q) = (%v,%v), want (%v,%v)", c.in, uses, usedBy, c.wantUses, c.wantUsedBy)
			}
		})
	}
}
