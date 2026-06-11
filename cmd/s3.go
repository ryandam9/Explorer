package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/s3tui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	s3Bucket      string
	s3Prefix      string
	s3Theme       string
	s3AllowDelete bool
	s3EndpointURL string
)

var s3Cmd = &cobra.Command{
	Use:   "s3",
	Short: "Start the S3 Explorer TUI",
	Long: `Start an interactive S3 TUI for exploring buckets, objects, versions and
metadata, with preview, download, presigned URLs and (optionally) guarded
delete operations.`,
	Example: `  # Browse all buckets
  aws_explorer s3

  # Jump straight into a bucket prefix
  aws_explorer s3 --bucket my-bucket --prefix logs/2026/

  # Point at LocalStack or MinIO
  aws_explorer s3 --endpoint-url http://localhost:4566`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s3Cfg := tuiAWSConfig()
		activeTheme := resolveTheme(cmd, s3Theme)

		ui.InitFromConfig(AppConfig.UI)
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceScanLogs()

		m, err := s3tui.NewModel(ctx, s3Cfg, awsRegion, s3Bucket, s3Prefix, activeTheme, s3AllowDelete, s3EndpointURL, configFilePath(), AppConfig)
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
	s3Cmd.Flags().StringVar(&s3Theme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	s3Cmd.Flags().BoolVar(&s3AllowDelete, "allow-delete", false, "Enable delete operations (guarded by confirmation)")
	s3Cmd.Flags().StringVar(&s3EndpointURL, "endpoint-url", "", "Custom endpoint URL (for LocalStack/MinIO)")
	registerThemeCompletion(s3Cmd, ui.ThemeNames())
	rootCmd.AddCommand(s3Cmd)
}
