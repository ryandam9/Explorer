package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// newOutputFlagCmd returns a command carrying the same --output flag the root
// command defines, so applyOutputFormatDefault can be exercised in isolation.
func newOutputFlagCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "")
	return cmd
}

func TestApplyOutputFormatDefault(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		setFlag   string // non-empty => simulate explicit --output
		startWith string // initial outputFormat value
		want      string
	}{
		{
			name:      "output.format applied when flag not set",
			cfg:       &config.Config{Output: config.OutputConfig{Format: "json"}},
			startWith: "table",
			want:      "json",
		},
		{
			name:      "app.defaultOutput applied when output.format empty",
			cfg:       &config.Config{App: config.AppConfig{DefaultOutput: "ndjson"}},
			startWith: "table",
			want:      "ndjson",
		},
		{
			name:      "output.format wins over app.defaultOutput",
			cfg:       &config.Config{App: config.AppConfig{DefaultOutput: "ndjson"}, Output: config.OutputConfig{Format: "csv"}},
			startWith: "table",
			want:      "csv",
		},
		{
			name:      "explicit flag wins over config",
			cfg:       &config.Config{Output: config.OutputConfig{Format: "json"}},
			setFlag:   "csv",
			startWith: "table",
			want:      "csv",
		},
		{
			name:      "no config values leaves flag default",
			cfg:       &config.Config{},
			startWith: "table",
			want:      "table",
		},
		{
			name:      "nil config is a no-op",
			cfg:       nil,
			startWith: "table",
			want:      "table",
		},
	}

	origCfg := AppConfig
	origFmt := outputFormat
	t.Cleanup(func() {
		AppConfig = origCfg
		outputFormat = origFmt
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			AppConfig = tt.cfg
			outputFormat = tt.startWith
			cmd := newOutputFlagCmd()
			if tt.setFlag != "" {
				if err := cmd.Flags().Set("output", tt.setFlag); err != nil {
					t.Fatalf("set flag: %v", err)
				}
			}

			applyOutputFormatDefault(cmd)

			if outputFormat != tt.want {
				t.Errorf("outputFormat = %q, want %q", outputFormat, tt.want)
			}
		})
	}
}
