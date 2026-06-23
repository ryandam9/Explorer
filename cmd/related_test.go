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

func TestRelatedOutputFormat(t *testing.T) {
	// --format unset → fall back to -o.
	if got, err := relatedOutputFormat("json", ""); err != nil || got != "json" {
		t.Errorf("unset --format should use -o: got %q, %v", got, err)
	}
	// --format graph dialects override -o.
	for _, f := range []string{"dot", "mermaid", "DOT", "Mermaid"} {
		got, err := relatedOutputFormat("table", f)
		if err != nil || (got != "dot" && got != "mermaid") {
			t.Errorf("relatedOutputFormat(table, %q) = %q, %v", f, got, err)
		}
	}
	// Invalid --format is rejected.
	if _, err := relatedOutputFormat("table", "png"); err == nil {
		t.Errorf("invalid --format should error")
	}
}

func TestParseDepth(t *testing.T) {
	cases := []struct {
		in      int
		want    int
		wantErr bool
	}{
		{0, 1, false},  // floored to one hop
		{-3, 1, false}, // floored
		{1, 1, false},
		{relatedMaxDepth, relatedMaxDepth, false},
		{relatedMaxDepth + 1, 0, true}, // too deep
	}
	for _, c := range cases {
		got, err := parseDepth(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseDepth(%d) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("parseDepth(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseShowPaths(t *testing.T) {
	cases := []struct {
		in      string
		wantAll bool
		wantErr bool
	}{
		{"", false, false},
		{"shortest", false, false},
		{"all", true, false},
		{"ALL", true, false},
		{"bogus", false, true},
	}
	for _, c := range cases {
		all, err := parseShowPaths(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseShowPaths(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if err == nil && all != c.wantAll {
			t.Errorf("parseShowPaths(%q) = %v, want %v", c.in, all, c.wantAll)
		}
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
