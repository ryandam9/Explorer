package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/ryandam9/aws_explorer/internal/docsgen"
)

var (
	docsDir    string
	docsFormat string
)

// docsFormats are the supported --format values.
var docsFormats = []string{"markdown", "html", "man", "all"}

// docsCmd generates the project documentation. Hidden: it is packaging /
// authoring tooling (make docs, release pipelines), not a day-to-day command.
//
// markdown/html produce a browsable doc site — a command reference generated
// from this very command tree, plus curated guides for the TUI screens and
// shortcuts. man produces troff man pages for packaging.
var docsCmd = &cobra.Command{
	Use:    "docs",
	Short:  "Generate project documentation (markdown, html, man)",
	Hidden: true,
	Example: `  # A browsable HTML site under ./docs/site
  aws_explorer docs --format html --dir docs/site

  # Markdown (renders as a landing page on GitHub)
  aws_explorer docs --format markdown --dir docs

  # Everything (markdown, html and man pages)
  aws_explorer docs --format all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format := strings.ToLower(strings.TrimSpace(docsFormat))
		if !validDocsFormat(format) {
			return fmt.Errorf("unknown --format %q (available: %s)", docsFormat, strings.Join(docsFormats, ", "))
		}

		pages := docsgen.Build(rootCmd)

		if format == "markdown" || format == "all" {
			n, err := docsgen.WriteMarkdown(docsDir, pages)
			if err != nil {
				return fmt.Errorf("writing markdown docs: %w", err)
			}
			fmt.Printf("Wrote %d Markdown file(s) to %s\n", n, docsDir)
		}
		if format == "html" || format == "all" {
			n, err := docsgen.WriteHTML(docsDir, pages)
			if err != nil {
				return fmt.Errorf("writing html docs: %w", err)
			}
			fmt.Printf("Wrote %d HTML file(s) to %s\n", n, docsDir)
		}
		if format == "man" || format == "all" {
			if err := writeManPages(docsDir); err != nil {
				return fmt.Errorf("writing man pages: %w", err)
			}
			fmt.Printf("Wrote man pages to %s\n", docsDir)
		}
		return nil
	},
}

// writeManPages generates troff man pages for the whole command tree.
func writeManPages(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	header := &doc.GenManHeader{
		Title:   "AWS_EXPLORER",
		Section: "1",
		Source:  "aws_explorer " + version,
		Manual:  "AWS Explorer Manual",
	}
	return doc.GenManTree(rootCmd, header, dir)
}

func validDocsFormat(f string) bool {
	for _, v := range docsFormats {
		if f == v {
			return true
		}
	}
	return false
}

func init() {
	docsCmd.Flags().StringVar(&docsDir, "dir", "docs", "directory to write the documentation into")
	docsCmd.Flags().StringVar(&docsFormat, "format", "markdown",
		"documentation format: "+strings.Join(docsFormats, ", "))
	_ = docsCmd.RegisterFlagCompletionFunc("format",
		cobra.FixedCompletions(docsFormats, cobra.ShellCompDirectiveNoFileComp))
	rootCmd.AddCommand(docsCmd)
}
