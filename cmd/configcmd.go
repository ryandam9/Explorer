package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	configInitPath  string
	configInitForce bool
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the configuration file",
	Long: `Inspect or scaffold the aws_explorer configuration.

The configuration is searched in this order: the --config flag, ./config.yaml,
the user config directory (e.g. ~/.config/aws_explorer/config.yaml), and
finally the defaults built into the binary.`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a starter config.yaml with the built-in defaults",
	Example: `  # Scaffold ./config.yaml in the current directory
  aws_explorer config init

  # Scaffold the per-user config used from any directory
  aws_explorer config init --path ~/.config/aws_explorer/config.yaml`,
	Run: func(cmd *cobra.Command, args []string) {
		path := configInitPath
		if path == "" {
			path = "config.yaml"
		}
		if home, err := os.UserHomeDir(); err == nil && len(path) > 1 && path[:2] == "~/" {
			path = filepath.Join(home, path[2:])
		}
		if _, err := os.Stat(path); err == nil && !configInitForce {
			fmt.Fprintf(os.Stderr, "Error: %s already exists (use --force to overwrite)\n", path)
			os.Exit(1)
		}
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", dir, err)
				os.Exit(1)
			}
		}
		if err := os.WriteFile(path, defaultConfigYAML, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %s\n", path)
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path of the active configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		path := configFilePath()
		if path == "" {
			fmt.Println("(built-in defaults — no config file found)")
			return
		}
		if _, err := os.Stat(path); err != nil {
			fmt.Printf("%s (not created yet — running on built-in defaults)\n", path)
			return
		}
		fmt.Println(path)
	},
}

func init() {
	configInitCmd.Flags().StringVar(&configInitPath, "path", "", "where to write the config file (default ./config.yaml)")
	configInitCmd.Flags().BoolVar(&configInitForce, "force", false, "overwrite an existing file")
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configPathCmd)
	rootCmd.AddCommand(configCmd)
}
