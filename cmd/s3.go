package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/s3tui"
	"github.com/user/aws_explorer/internal/ui"
)

var (
	s3Bucket      string
	s3Prefix      string
	s3Profile     string
	s3AuthMethod  string
	s3RoleARN     string
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

		// Build auth config, preferring local s3 flags over global ones.
		s3Cfg := &config.AWSConfig{
			Profile:    awsProfile,
			AuthMethod: awsAuthMethod,
		}
		if s3Profile != "" {
			s3Cfg.Profile = s3Profile
		}
		if s3AuthMethod != "" {
			s3Cfg.AuthMethod = s3AuthMethod
		}
		roleARN := awsRoleARN
		if s3RoleARN != "" {
			roleARN = s3RoleARN
		}
		if roleARN != "" {
			s3Cfg.STS.RoleARN = roleARN
			if s3Cfg.AuthMethod == "" || s3Cfg.AuthMethod == "auto" {
				s3Cfg.AuthMethod = "sts"
			}
		}

		// Theme: CLI flag overrides config; config overrides built-in default.
		activeTheme := s3Theme
		if AppConfig != nil && AppConfig.UI.Theme != "" && s3Theme == "spotted-pardalote" {
			activeTheme = AppConfig.UI.Theme
		}
		ui.InitFromConfig(AppConfig.UI)

		m, err := s3tui.NewModel(ctx, s3Cfg, s3Region, s3Bucket, s3Prefix, activeTheme, s3AllowDelete, s3EndpointURL, configFilePath(), AppConfig)
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
	s3Cmd.Flags().StringVar(&s3Profile, "profile", "", "AWS named profile (overrides global --profile)")
	s3Cmd.Flags().StringVar(&s3AuthMethod, "auth-method", "", "Auth method: auto, profile, env, static, sts (overrides global --auth-method)")
	s3Cmd.Flags().StringVar(&s3RoleARN, "role-arn", "", "IAM role ARN to assume via STS (overrides global --role-arn)")
	s3Cmd.Flags().StringVar(&s3Region, "region", "", "AWS region (defaults to the region in ~/.aws/config or AWS_DEFAULT_REGION)")
	s3Cmd.Flags().StringVar(&s3Theme, "theme", "spotted-pardalote", "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	s3Cmd.Flags().BoolVar(&s3AllowDelete, "allow-delete", false, "Enable delete operations (guarded by confirmation)")
	s3Cmd.Flags().StringVar(&s3EndpointURL, "endpoint-url", "", "Custom endpoint URL (for LocalStack/MinIO)")
	rootCmd.AddCommand(s3Cmd)
}
