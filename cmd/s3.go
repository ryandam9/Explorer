package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/user/aws_explorer/internal/s3tui"
)

var (
	s3Bucket  string
	s3Prefix  string
	s3Profile string
	s3Region  string
	s3Theme   string
)

var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "Start the S3 Explorer TUI",
	Long:  `Start a highly interactive, read-only S3 TUI for exploring buckets, objects, and metadata.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Get region/profile from global flags or local flags
		profile := awsProfile
		if s3Profile != "" {
			profile = s3Profile
		}

		m, err := s3tui.NewModel(ctx, profile, s3Region, s3Bucket, s3Prefix, s3Theme)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing S3 TUI: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running S3 TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	s3Cmd.Flags().StringVarP(&s3Bucket, "bucket", "b", "", "S3 bucket to explore")
	s3Cmd.Flags().StringVarP(&s3Prefix, "prefix", "p", "", "Initial S3 prefix")
	s3Cmd.Flags().StringVar(&s3Profile, "profile", "", "AWS profile (overrides global)")
	s3Cmd.Flags().StringVar(&s3Region, "region", "us-east-1", "AWS region")
	s3Cmd.Flags().StringVar(&s3Theme, "theme", "spotted pardalote", "Color theme (spotted pardalote, plains wanderer, bee-eater, rose-crowned fruit dove, eastern rosella, oriole, princess parrot, superb fairy-wren, cassowary, yellow robin, galah, blue-winged kookaburra)")
	rootCmd.AddCommand(s3Cmd)
}
