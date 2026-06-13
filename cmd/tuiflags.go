package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// defaultThemeName is the --theme flag default for the standalone TUIs.
const defaultThemeName = "spotted-pardalote"

// tuiAWSConfig builds the AWS auth config for the standalone TUIs from the
// persistent CLI flags (--profile, --auth-method, --role-arn).
func tuiAWSConfig() *config.AWSConfig {
	c := &config.AWSConfig{Profile: awsProfile, AuthMethod: awsAuthMethod}
	if awsRoleARN != "" {
		c.STS.RoleARN = awsRoleARN
		if c.AuthMethod == "" || c.AuthMethod == "auto" {
			c.AuthMethod = "sts"
		}
	}
	return c
}

// resolveTheme picks the active theme: an explicitly passed --theme flag
// wins, then ui.theme from the config, then the flag's default.
func resolveTheme(cmd *cobra.Command, flagVal string) string {
	if cmd.Flags().Changed("theme") {
		return flagVal
	}
	if AppConfig != nil && AppConfig.UI.Theme != "" {
		return AppConfig.UI.Theme
	}
	return flagVal
}

// registerThemeCompletion wires shell completion for a --theme flag.
func registerThemeCompletion(cmd *cobra.Command, themeNames []string) {
	_ = cmd.RegisterFlagCompletionFunc("theme",
		cobra.FixedCompletions(themeNames, cobra.ShellCompDirectiveNoFileComp))
}

// registerAlwaysTUIFlag adds a --tui flag to a command that is always
// interactive (vpc, s3, cw). These have no table/CLI mode, but accepting --tui
// keeps invocation uniform with the commands that toggle a TUI off a CLI
// default (summary, audit, bill): "<cmd> --tui" then works everywhere, while
// the bare "<cmd>" form keeps working. The flag value is not read — the
// command is interactive either way.
func registerAlwaysTUIFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("tui", true,
		"open the interactive explorer (always on for this command; accepted for consistency)")
}
