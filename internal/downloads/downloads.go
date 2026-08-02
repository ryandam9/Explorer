// Package downloads resolves the single directory every user-facing export
// is written to — CSV exports, saved CloudWatch logs, VPC report exports,
// downloaded S3 objects, EMR Markdown reports. One knob, one place: the
// app.downloadDir config value overrides it, and the default is
// ~/.aws_explorer/downloads (a dedicated folder under the app home).
//
// Internal application state (account snapshots, VPC baselines) deliberately
// does NOT live here: those are consumed by the tool itself and keep their
// own directories under ~/.aws_explorer, so pointing downloads somewhere
// else never breaks diffing.
package downloads

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu       sync.RWMutex
	override string
)

// Init records the configured download directory (app.downloadDir). Call it
// once after the config is loaded; an empty value keeps the default.
func Init(dir string) {
	mu.Lock()
	override = strings.TrimSpace(dir)
	mu.Unlock()
}

// Dir returns the directory downloads and exports are written to, creating it
// if needed. The configured app.downloadDir (with a leading "~" expanded)
// wins; otherwise ~/.aws_explorer/downloads.
func Dir() (string, error) {
	mu.RLock()
	dir := override
	mu.RUnlock()

	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".aws_explorer", "downloads")
	} else if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create download directory %s: %w", dir, err)
	}
	return dir, nil
}
