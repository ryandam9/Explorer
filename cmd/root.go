package cmd

import (
	"context"
	"fmt"
	"os"

	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
)

var cfgFile string
var awsProfile string
var awsAuthMethod string
var awsRoleARN string
var outputFormat string
var allRegions bool
var AppConfig *config.Config
var resolvedCfgFile string // absolute path after viper resolves it

// configFilePath returns the resolved path to the active config file.
func configFilePath() string { return resolvedCfgFile }

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "aws_explorer",
	Short: "A tool to monitor and list AWS resources",
	Long: `AWS Explorer is a CLI tool that uses a configuration file
to monitor and list various AWS resources such as EC2, S3, RDS, etc.`,
	Run: func(cmd *cobra.Command, args []string) {
		if allRegions {
			AppConfig.AWS.AllRegions = true
		}
		// CLI flags override config file values.
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

		slog.Info("Starting AWS Explorer", "regions", AppConfig.AWS.Regions)

		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			slog.Error("Failed to initialize engine", "error", err)
			os.Exit(1)
		}

		chunks := make(chan model.ResultChunk, 64)
		go eng.StreamRun(ctx, chunks)
		output.StreamOutput(chunks, outputFormat)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Define global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().StringVar(&awsProfile, "profile", "", "AWS named profile (overrides aws.profile in config)")
	rootCmd.PersistentFlags().StringVar(&awsAuthMethod, "auth-method", "", "Auth method: auto, profile, env, static, sts (overrides aws.authMethod in config)")
	rootCmd.PersistentFlags().StringVar(&awsRoleARN, "role-arn", "", "IAM role ARN to assume via STS (sets auth method to sts)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, csv)")
	rootCmd.PersistentFlags().BoolVar(&allRegions, "all-regions", false, "Scan all available AWS regions")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find current directory
		dir, err := os.Getwd()
		cobra.CheckErr(err)

		// Search config in current directory with name "config" (without extension).
		viper.AddConfigPath(dir)
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config file: %s\n", err)
		os.Exit(1)
	}
	resolvedCfgFile = viper.ConfigFileUsed()

	// Unmarshal into strongly typed config struct
	AppConfig = &config.Config{}
	if err := viper.Unmarshal(AppConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config file: %s\n", err)
		os.Exit(1)
	}
}
