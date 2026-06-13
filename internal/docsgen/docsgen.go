// Package docsgen turns the live cobra command tree plus a set of curated
// guide pages into a browsable documentation site, written as Markdown and/or
// HTML. The command reference is generated from the binary itself, so it
// always matches the commands, flags and examples the user actually has;
// the guides cover the interactive TUI screens and keyboard shortcuts that a
// command tree cannot describe.
package docsgen

import (
	"embed"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Section groups pages in the navigation sidebar / index.
type Section string

const (
	SectionHome     Section = "Home"
	SectionGuides   Section = "Guides"
	SectionCommands Section = "Command reference"
)

// Page is one documentation page in both output formats. Body is Markdown
// without a leading H1 — the writers render Title as the heading so Markdown
// and HTML stay consistent. Internal links in Body use relative ".md" targets
// (e.g. "[bill](bill.md)"); the HTML writer rewrites them to ".html".
type Page struct {
	Slug    string  // file basename without extension (e.g. "bill", "guide-summary")
	Title   string  // rendered as the page H1 and nav label
	Section Section // navigation grouping
	Body    string  // Markdown body, no leading H1
}

//go:embed guides/*.md
var guideFS embed.FS

// guide registry — ordering and titles for the embedded guide Markdown. The
// slugs are deliberately prefixed "guide-" so they never collide with a
// command slug, and the cross-links inside the guides use these slugs.
var guideList = []struct {
	file, slug, title string
}{
	{"getting-started.md", "guide-getting-started", "Getting started"},
	{"authentication.md", "guide-authentication", "Authentication"},
	{"configuration.md", "guide-configuration", "Configuration"},
	{"tui-summary.md", "guide-summary", "The summary TUI"},
	{"tui-vpc.md", "guide-vpc", "VPC explorer TUI"},
	{"tui-s3.md", "guide-s3", "S3 browser TUI"},
	{"tui-cloudwatch.md", "guide-cloudwatch", "CloudWatch Logs TUI"},
	{"tui-reports.md", "guide-reports", "Audit & Bill TUIs"},
	{"shortcuts.md", "guide-shortcuts", "Keyboard shortcut cheat sheet"},
}

// Build assembles the full, ordered page set for the given root command:
// the home/index page, the guides, then one page per visible command.
// appName is the binary name used in titles and the install snippet.
func Build(root *cobra.Command) []Page {
	guides := guidePages()
	cmds := commandPages(root)
	pages := make([]Page, 0, 1+len(guides)+len(cmds))
	pages = append(pages, indexPage(root, guides, cmds))
	pages = append(pages, guides...)
	pages = append(pages, cmds...)
	return pages
}

// guidePages loads the embedded guide Markdown in registry order.
func guidePages() []Page {
	pages := make([]Page, 0, len(guideList))
	for _, g := range guideList {
		b, err := guideFS.ReadFile("guides/" + g.file)
		if err != nil {
			// Embedded files are compiled in; a miss is a programming error,
			// not a runtime condition. Skip rather than panic so a renamed
			// file never crashes `docs`.
			continue
		}
		pages = append(pages, Page{
			Slug:    g.slug,
			Title:   g.title,
			Section: SectionGuides,
			Body:    strings.TrimRight(string(b), "\n"),
		})
	}
	return pages
}

// commandPages walks the command tree depth-first (commands sorted by name at
// each level) and renders one page per visible command, including the root.
func commandPages(root *cobra.Command) []Page {
	var pages []Page
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		pages = append(pages, commandPage(c))
		children := append([]*cobra.Command(nil), c.Commands()...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if skipCommand(child) {
				continue
			}
			walk(child)
		}
	}
	walk(root)
	return pages
}

// skipCommand drops commands that should not get a documentation page: hidden
// commands (internal tooling like `docs`), cobra's auto-added `help` and
// `completion`, and additional help topics.
func skipCommand(c *cobra.Command) bool {
	if c.Hidden || c.IsAdditionalHelpTopicCommand() {
		return true
	}
	switch c.Name() {
	case "help", "completion":
		return true
	}
	return false
}

// commandSlug turns a command path into a file slug: the root command maps to
// "cli", everything else to its path below the root joined by underscores
// ("iam can" → "iam_can", "config init" → "config_init").
func commandSlug(c *cobra.Command) string {
	if !c.HasParent() {
		return "cli"
	}
	parts := strings.Fields(c.CommandPath())
	return strings.Join(parts[1:], "_")
}
