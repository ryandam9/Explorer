package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
)

// Build metadata, injected at build time via
// -ldflags "-X github.com/ryandam9/aws_explorer/cmd.version=…".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var cfgFile string
var awsProfile string
var awsAuthMethod string
var awsRoleARN string
var awsRegion string
var outputFormat string
var allRegions bool
var noHeader bool
var AppConfig *config.Config
var resolvedCfgFile string // absolute path after viper resolves it

// defaultConfigYAML holds the built-in default configuration, embedded from
// the repository's config.yaml by main and injected via SetDefaultConfig.
var defaultConfigYAML []byte

// SetDefaultConfig installs the embedded default configuration used when no
// config file exists on disk.
func SetDefaultConfig(b []byte) { defaultConfigYAML = b }

// configFilePath returns the resolved path to the active config file. When
// the app is running on built-in defaults this is the path a save would
// create, not a file that necessarily exists yet.
func configFilePath() string { return resolvedCfgFile }

// userConfigPath returns the per-user config file location
// (~/.config/aws_explorer/config.yaml on Linux), or "" when the user config
// directory cannot be determined.
func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "aws_explorer", "config.yaml")
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "aws_explorer",
	Short: "Discover and list AWS resources across accounts and regions",
	Long: `AWS Explorer discovers, monitors and lists AWS resources — EC2, S3, RDS,
Lambda and a dozen more services — across accounts and regions.

Run with no arguments to scan the enabled services and stream results to
stdout as they arrive, or use a subcommand for an interactive TUI.

Configuration is optional: when no config.yaml exists in the current
directory or in the user config directory, built-in defaults are used.
Run "aws_explorer config init" to write a starter file.`,
	Example: `  # Scan the configured services and regions
  aws_explorer

  # Scan a single region with a named profile
  aws_explorer --profile prod --region eu-west-1

  # Machine-readable output
  aws_explorer -o json | jq '.[].id'
  aws_explorer -o ndjson | head
  aws_explorer -o csv --no-header > resources.csv

  # Scan every available region
  aws_explorer --all-regions`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return output.ValidateFormat(outputFormat)
	},
	Run: func(cmd *cobra.Command, args []string) {
		applyGlobalAWSOverrides()

		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		// Collection warnings are summarized after the run by the output
		// layer; the raw log stream would only garble the streamed results.
		SilenceScanLogs()

		chunks := make(chan model.ResultChunk, 64)
		go eng.StreamRun(ctx, chunks)
		output.StreamOutput(chunks, outputFormat, output.Options{
			NoHeader:   noHeader,
			TotalTasks: len(eng.PlannedTaskKeys()),
		})
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

// applyGlobalAWSOverrides applies the persistent CLI auth and region flags
// onto AppConfig. CLI flags override config file values.
func applyGlobalAWSOverrides() {
	if allRegions {
		AppConfig.AWS.AllRegions = true
	}
	if awsProfile != "" {
		AppConfig.AWS.Profile = awsProfile
	}
	if awsAuthMethod != "" {
		AppConfig.AWS.AuthMethod = awsAuthMethod
	}
	if awsRoleARN != "" {
		AppConfig.AWS.STS.RoleARN = awsRoleARN
		if AppConfig.AWS.AuthMethod == "" || AppConfig.AWS.AuthMethod == "auto" {
			AppConfig.AWS.AuthMethod = "sts"
		}
	}

	// --region pins the scan to a single region. It wins over every other
	// region setting: the config's aws.regions, aws.allRegions, --all-regions,
	// and any filters.regions narrowing. Applied last so it overrides the
	// --all-regions handling above.
	if awsRegion != "" {
		AppConfig.AWS.Regions = []string{awsRegion}
		AppConfig.AWS.AllRegions = false
		AppConfig.Filters.Regions = nil
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)

	// Define global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml, then the user config dir, then built-in defaults)")
	rootCmd.PersistentFlags().StringVar(&awsProfile, "profile", "", "AWS named profile (overrides aws.profile in config)")
	rootCmd.PersistentFlags().StringVar(&awsAuthMethod, "auth-method", "", "auth method: auto, profile, env, static, sts (overrides aws.authMethod in config)")
	rootCmd.PersistentFlags().StringVar(&awsRoleARN, "role-arn", "", "IAM role ARN to assume via STS (sets auth method to sts)")
	rootCmd.PersistentFlags().StringVarP(&awsRegion, "region", "r", "", "scan only this region (overrides aws.regions, --all-regions and region filters)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format: "+output.FormatList())
	rootCmd.PersistentFlags().BoolVar(&noHeader, "no-header", false, "omit the header row in table and csv output")
	rootCmd.PersistentFlags().BoolVar(&allRegions, "all-regions", false, "scan all available AWS regions")

	// Shell completion for flag values.
	_ = rootCmd.RegisterFlagCompletionFunc("output",
		cobra.FixedCompletions(output.Formats, cobra.ShellCompDirectiveNoFileComp))
	_ = rootCmd.RegisterFlagCompletionFunc("auth-method",
		cobra.FixedCompletions([]string{"auto", "profile", "env", "static", "sts"}, cobra.ShellCompDirectiveNoFileComp))
	_ = rootCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return auth.ListProfiles(), cobra.ShellCompDirectiveNoFileComp
		})
}

// initConfig locates and reads the configuration. Search order: the --config
// flag, ./config.yaml, the user config directory, and finally the built-in
// defaults embedded in the binary — so the tool works from any directory
// with zero setup.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file: %s\n", err)
			os.Exit(1)
		}
		resolvedCfgFile = viper.ConfigFileUsed()
	} else {
		dir, err := os.Getwd()
		cobra.CheckErr(err)

		// Search ./config.yaml first, then the per-user config.
		viper.AddConfigPath(dir)
		if ucp := userConfigPath(); ucp != "" {
			viper.AddConfigPath(filepath.Dir(ucp))
		}
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")

		switch err := viper.ReadInConfig(); err.(type) {
		case nil:
			resolvedCfgFile = viper.ConfigFileUsed()
		case viper.ConfigFileNotFoundError:
			// No config anywhere: run on the built-in defaults. Saves from
			// the in-app settings panel land in the user config directory.
			if rerr := viper.ReadConfig(bytes.NewReader(defaultConfigYAML)); rerr != nil {
				fmt.Fprintf(os.Stderr, "Error reading built-in default config: %s\n", rerr)
				os.Exit(1)
			}
			resolvedCfgFile = userConfigPath()
		default:
			fmt.Fprintf(os.Stderr, "Error reading config file: %s\n", err)
			os.Exit(1)
		}
	}

	viper.AutomaticEnv() // read in environment variables that match

	// Unmarshal into strongly typed config struct
	AppConfig = &config.Config{}
	if err := viper.Unmarshal(AppConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config file: %s\n", err)
		os.Exit(1)
	}
}
