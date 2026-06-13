package docsgen

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// indexPage builds the documentation home: a short intro, the guide list, and
// the full command list — each linked. It is the README.md / index.html the
// writers emit first.
func indexPage(root *cobra.Command, guides, cmds []Page) Page {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(root.Short))
	b.WriteString("This documentation is generated from the tool itself — the " +
		"command reference below is built from the live command tree, so it always " +
		"matches the binary you are running. New in this release: regenerate it any " +
		"time with `" + root.Name() + " docs --format html` (or `markdown`).\n\n")

	b.WriteString("## Guides\n\n")
	for _, g := range guides {
		fmt.Fprintf(&b, "- [%s](%s.md) — %s\n", g.Title, g.Slug, firstSentence(g.Body))
	}
	b.WriteString("\n")

	b.WriteString("## Command reference\n\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "- [`%s`](%s.md) — %s\n", c.Title, c.Slug, firstSentence(c.Body))
	}
	b.WriteString("\n")

	return Page{
		Slug:    "index",
		Title:   root.Name() + " documentation",
		Section: SectionHome,
		Body:    strings.TrimRight(b.String(), "\n"),
	}
}

// firstSentence extracts a one-line summary from a Markdown body: the first
// non-empty, non-heading line, trimmed to its first sentence.
func firstSentence(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") ||
			strings.HasPrefix(line, "`") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, ">") {
			continue
		}
		if i := strings.Index(line, ". "); i > 0 {
			return line[:i+1]
		}
		return strings.TrimSuffix(line, ".") + "."
	}
	return ""
}
