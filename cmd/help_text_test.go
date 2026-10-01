package cmd

import (
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Fang title-cases the first word of every command summary and flag
// description when it renders the help page, and Go's cases.Title lowercases
// the rest of that word — so a description beginning with an acronym comes out
// as "Aws named profile", "Iam role ARN" or "Tui: use Nerd Font icons".
//
// On a tool whose every other noun is an initialism that reads as carelessness,
// which is the opposite of what styled help is for. The acronym is fine
// anywhere but first, so this test keeps the first word a normal word.
func TestHelpTextDoesNotStartWithAnAcronym(t *testing.T) {
	var walk func(*cobra.Command)
	check := func(what, text string) {
		first, _, _ := strings.Cut(strings.TrimSpace(text), " ")
		first = strings.TrimRight(first, ":,.")
		if isAcronym(first) {
			t.Errorf("%s starts with %q, which the help page renders as %q — "+
				"reword so the acronym is not the first word", what, first, titleish(first))
		}
	}
	walk = func(c *cobra.Command) {
		if c.Short != "" {
			check("command "+c.CommandPath()+" Short", c.Short)
		}
		flags := func(f *pflag.Flag) {
			if f.Usage != "" {
				check("flag --"+f.Name+" on "+c.CommandPath(), f.Usage)
			}
		}
		c.Flags().VisitAll(flags)
		c.PersistentFlags().VisitAll(flags)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// isAcronym reports whether a word is two or more letters, all upper case
// (ignoring any trailing punctuation) — "AWS", "IAM", "TUI", "S3".
func isAcronym(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			letters++
		}
	}
	return letters >= 2
}

// titleish shows what the rendered help would look like, for the failure
// message.
func titleish(w string) string {
	r := []rune(strings.ToLower(w))
	if len(r) == 0 {
		return w
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
