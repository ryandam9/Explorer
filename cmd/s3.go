package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/user/aws_explorer/internal/s3tui"
	"github.com/user/aws_explorer/internal/tui"
)

var (
	s3Bucket      string
	s3Prefix      string
	s3Profile     string
	s3Region      string
	s3Theme       string
	s3AllowDelete bool
	s3EndpointURL string
)

var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "Start the S3 Explorer TUI",
	Long:  `Start a highly interactive S3 TUI for exploring buckets, objects, and metadata.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Get region/profile from global flags or local flags
		profile := awsProfile
		if s3Profile != "" {
			profile = s3Profile
		}

		m, err := s3tui.NewModel(ctx, profile, s3Region, s3Bucket, s3Prefix, s3Theme, s3AllowDelete, s3EndpointURL)
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
	s3Cmd.Flags().StringVar(&s3Theme, "theme", "spotted-pardalote", "Color theme ("+strings.Join(tui.ThemeNames(), ", ")+")")
	s3Cmd.Flags().BoolVar(&s3AllowDelete, "allow-delete", false, "Enable delete operations (guarded by confirmation)")
	s3Cmd.Flags().StringVar(&s3EndpointURL, "endpoint-url", "", "Custom endpoint URL (for LocalStack/MinIO)")
	rootCmd.AddCommand(s3Cmd)
}
