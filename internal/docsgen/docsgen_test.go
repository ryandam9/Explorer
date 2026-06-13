package docsgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// testTree builds a small command tree that exercises the generator: a root
// with global flags, a leaf command with local flags + an example, a parent
// with a nested subcommand, and a hidden command that must be skipped.
func testTree() *cobra.Command {
	root := &cobra.Command{Use: "tool", Short: "A test tool", Run: func(*cobra.Command, []string) {}}
	root.PersistentFlags().String("profile", "", "AWS named profile")

	bill := &cobra.Command{
		Use:     "bill",
		Short:   "Show the bill",
		Long:    "Show the bill, grouped by service.",
		Example: "  # current month\n  tool bill\n  tool bill --month 2026-05",
		Run:     func(*cobra.Command, []string) {},
	}
	bill.Flags().String("month", "", "billing period YYYY-MM")
	bill.Flags().Bool("tui", false, "live screen")

	iam := &cobra.Command{Use: "iam", Short: "IAM helpers", Run: func(*cobra.Command, []string) {}}
	can := &cobra.Command{Use: "can", Short: "Simulate policy", Run: func(*cobra.Command, []string) {}}
	iam.AddCommand(can)

	hidden := &cobra.Command{Use: "secret", Short: "hidden", Hidden: true, Run: func(*cobra.Command, []string) {}}

	root.AddCommand(bill, iam, hidden)
	return root
}

func pageBySlug(pages []Page, slug string) (Page, bool) {
	for _, p := range pages {
		if p.Slug == slug {
			return p, true
		}
	}
	return Page{}, false
}

func TestBuild_StructureAndOrdering(t *testing.T) {
	pages := Build(testTree())

	if len(pages) == 0 || pages[0].Section != SectionHome {
		t.Fatalf("first page must be the home/index, got %+v", pages[0])
	}

	// Every embedded guide must produce a page.
	for _, g := range guideList {
		if _, ok := pageBySlug(pages, g.slug); !ok {
			t.Errorf("missing guide page %q", g.slug)
		}
	}

	// Expected command pages exist; the hidden command does not.
	for _, want := range []string{"cli", "bill", "iam", "iam_can"} {
		if _, ok := pageBySlug(pages, want); !ok {
			t.Errorf("missing command page %q", want)
		}
	}
	if _, ok := pageBySlug(pages, "secret"); ok {
		t.Error("hidden command should not be documented")
	}

	// Slugs must be unique (collisions would overwrite files).
	seen := map[string]bool{}
	for _, p := range pages {
		if seen[p.Slug] {
			t.Errorf("duplicate slug %q", p.Slug)
		}
		seen[p.Slug] = true
		if p.Title == "" {
			t.Errorf("page %q has no title", p.Slug)
		}
	}
}

func TestCommandPage_FlagsExamplesSubcommands(t *testing.T) {
	pages := Build(testTree())

	bill, _ := pageBySlug(pages, "bill")
	for _, want := range []string{"## Usage", "## Examples", "tool bill --month 2026-05", "## Flags", "`--month`", "## Global flags", "`--profile`"} {
		if !strings.Contains(bill.Body, want) {
			t.Errorf("bill page missing %q\n---\n%s", want, bill.Body)
		}
	}
	// Example must be dedented (no leading two-space indent on commands).
	if strings.Contains(bill.Body, "\n  tool bill") {
		t.Errorf("example was not dedented:\n%s", bill.Body)
	}

	iam, _ := pageBySlug(pages, "iam")
	if !strings.Contains(iam.Body, "## Subcommands") || !strings.Contains(iam.Body, "(iam_can.md)") {
		t.Errorf("iam page missing subcommand link:\n%s", iam.Body)
	}
}

func TestCommandSlug(t *testing.T) {
	root := testTree()
	if got := commandSlug(root); got != "cli" {
		t.Errorf("root slug = %q, want cli", got)
	}
	for _, c := range root.Commands() {
		if c.Name() == "iam" {
			for _, sub := range c.Commands() {
				if sub.Name() == "can" {
					if got := commandSlug(sub); got != "iam_can" {
						t.Errorf("nested slug = %q, want iam_can", got)
					}
				}
			}
		}
	}
}

func TestDedent(t *testing.T) {
	in := "  # one\n  two\n\n  three"
	want := "# one\ntwo\n\nthree"
	if got := dedent(in); got != want {
		t.Errorf("dedent = %q, want %q", got, want)
	}
	if got := dedent(""); got != "" {
		t.Errorf("dedent empty = %q, want empty", got)
	}
}

func TestWriteMarkdown(t *testing.T) {
	dir := t.TempDir()
	pages := Build(testTree())
	n, err := WriteMarkdown(dir, pages)
	if err != nil {
		t.Fatal(err)
	}
	// One file per page, plus the README.md duplicate of the home page.
	if n != len(pages)+1 {
		t.Errorf("wrote %d files, want %d", n, len(pages)+1)
	}
	for _, name := range []string{"index.md", "README.md", "bill.md", "guide-shortcuts.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	body, _ := os.ReadFile(filepath.Join(dir, "bill.md"))
	if !strings.HasPrefix(string(body), "[← Documentation index](index.md)") {
		t.Errorf("command page missing breadcrumb:\n%s", body)
	}
	if !strings.Contains(string(body), "# tool bill") {
		t.Errorf("command page missing H1 title:\n%s", body)
	}
}

func TestWriteHTML(t *testing.T) {
	dir := t.TempDir()
	pages := Build(testTree())
	n, err := WriteHTML(dir, pages)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(pages) {
		t.Errorf("wrote %d html files, want %d", n, len(pages))
	}

	bill, err := os.ReadFile(filepath.Join(dir, "bill.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(bill)

	// Markdown ".md" cross-links are rewritten to ".html".
	if strings.Contains(html, ".md\"") || strings.Contains(html, ".md)") {
		t.Errorf("html still contains .md links:\n%s", html)
	}
	// The flags table rendered as a real HTML table.
	if !strings.Contains(html, "<table>") {
		t.Error("expected a rendered HTML table in the bill page")
	}
	// The current page is marked active and template escaping did not choke.
	if !strings.Contains(html, `class="active"`) {
		t.Error("current page should carry an active nav class")
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Error("html/template rejected generated markup (ZgotmplZ)")
	}
	// Sidebar lists the section headings.
	for _, sec := range []string{string(SectionGuides), string(SectionCommands)} {
		if !strings.Contains(html, ">"+sec+"<") {
			t.Errorf("nav missing section %q", sec)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	if got := firstSentence("Show the bill. More text here."); got != "Show the bill." {
		t.Errorf("got %q", got)
	}
	if got := firstSentence("# heading\n\nA single line"); got != "A single line." {
		t.Errorf("got %q", got)
	}
}
