package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var docsDir string

// docsCmd generates man pages for every command. Hidden: it exists for
// packaging (make man, release pipelines), not day-to-day use.
var docsCmd = &cobra.Command{
	Use:    "docs",
	Short:  "Generate man pages",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(docsDir, 0o755); err != nil {
			return err
		}
		header := &doc.GenManHeader{
			Title:   "AWS_EXPLORER",
			Section: "1",
			Source:  "aws_explorer " + version,
			Manual:  "AWS Explorer Manual",
		}
		if err := doc.GenManTree(rootCmd, header, docsDir); err != nil {
			return err
		}
		fmt.Printf("Wrote man pages to %s\n", docsDir)
		return nil
	},
}

func init() {
	docsCmd.Flags().StringVar(&docsDir, "dir", "man", "directory to write man pages into")
	rootCmd.AddCommand(docsCmd)
}
