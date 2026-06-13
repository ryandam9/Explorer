package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// errPreflight is returned by the pre-flight check once it has already printed
// its own friendly message. It carries no text and is silenced (SilenceErrors)
// so Cobra does not print a second, raw "Error: …" line on top of the notice.
var errPreflight = errors.New("authentication check failed")

// preflightAuth verifies the user can call AWS before a command does any real
// work, so an expired SSO session or a stale STS token fails fast with the
// exact command that fixes it — instead of an empty TUI, a half-built engine,
// or a wall of identical per-region errors (issue #106).
//
// It runs from the root command's PersistentPreRunE, so every subcommand that
// reaches its Run inherits the check. Commands that do no AWS work (config,
// docs, completion) and the offline snapshot/diff viewers are skipped — see
// skipPreflight. On failure it renders a friendly notice to stderr itself and
// returns errPreflight, which is silenced so Cobra adds nothing further.
func preflightAuth(cmd *cobra.Command) error {
	if skipPreflight(cmd) {
		return nil
	}

	// Honour the persistent --profile/--auth-method/--role-arn/--region flags
	// before we resolve credentials; each Run re-applies them harmlessly.
	applyGlobalAWSOverrides()

	// No timeout here on purpose: the engine and every TUI resolve credentials
	// the same way (sts:GetCallerIdentity) with no deadline, and an STS
	// AssumeRole with MFA prompts on stdin — a short timeout would abort a user
	// mid-prompt. The SDK's own provider timeouts still bound a hung endpoint.
	ctx := context.Background()
	if _, err := auth.Verify(ctx, &AppConfig.AWS, bootstrapRegion(&AppConfig.AWS)); err != nil {
		renderAuthFailure(os.Stderr, err, AppConfig.AWS.Profile, bootstrapRegion(&AppConfig.AWS))
		// Suppress Cobra's own error/usage output; the notice above is the
		// whole message we want the user to see.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return errPreflight
	}
	return nil
}

// skipPreflight reports whether the pre-flight auth check must NOT run for cmd
// because it does no AWS work. The set of credential-free commands is small and
// stable (config, docs and Cobra's own helpers), so a deny-list keeps a new AWS
// command covered by default rather than silently unchecked.
func skipPreflight(cmd *cobra.Command) bool {
	// Cobra's shell-completion driver runs as a hidden "__complete" command;
	// completion must never make a network call or prompt for MFA.
	if strings.HasPrefix(cmd.Name(), "__") {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "config", "docs", "completion", "help":
			return true
		}
	}
	// `tui --snapshot` / `--diff` browse saved JSON offline with no credentials,
	// STS calls or region discovery; checking auth would block that on purpose.
	if cmd.Name() == "tui" && (snapshotPath != "" || len(diffPaths) > 0) {
		return true
	}
	return false
}

// bootstrapRegion picks the region used to resolve credentials for the
// pre-flight check, mirroring the engine: the first configured region, or
// us-east-1 when none is set or "all" is requested. STS is global, so the
// region only fixes which endpoint the verification call talks to.
func bootstrapRegion(cfg *config.AWSConfig) string {
	if cfg.AllRegions {
		return "us-east-1"
	}
	for _, r := range cfg.Regions {
		if strings.EqualFold(r, "all") {
			return "us-east-1"
		}
	}
	if len(cfg.Regions) > 0 && cfg.Regions[0] != "" {
		return cfg.Regions[0]
	}
	return "us-east-1"
}

// renderAuthFailure writes the friendly authentication-failure notice to w,
// classifying the error into the one line the user needs: the login command
// for expired credentials, a privileges hint for an access denial, or the raw
// error otherwise. Styling degrades to plain text when w is not a terminal.
func renderAuthFailure(w io.Writer, err error, profile, region string) {
	r := lipgloss.NewRenderer(w)
	title := r.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	fix := r.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	dim := r.NewStyle().Faint(true)

	var detail, action string
	switch {
	case awserr.IsExpiredCreds(err):
		detail = "Your AWS credentials are missing or have expired."
		action, _ = awserr.LoginHint(err, profile)
	case awserr.IsAuthError(err):
		detail = awserr.FriendlyMessage(err, "")
	default:
		detail = err.Error()
	}

	ctx := "region " + region
	if profile != "" && profile != "default" {
		ctx = "profile '" + profile + "', " + ctx
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, title.Render("✗ Cannot authenticate with AWS")+dim.Render(" — "+detail))
	if action != "" {
		fmt.Fprintln(w, "  "+fix.Render(action))
	}
	fmt.Fprintln(w, "  "+dim.Render("("+ctx+")"))
	fmt.Fprintln(w)
}
