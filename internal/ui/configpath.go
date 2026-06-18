package ui

import "os"

// ConfigArgPath returns path when it points to a config file that actually
// exists on disk, and "" otherwise.
//
// TUIs forward the active config to the child processes they spawn (the s3/cw
// log jumps) by appending --config <path>. The resolved config path, however,
// is also the location a settings save *would* create: when the app runs on
// built-in defaults (no config.yaml anywhere) that path names a file that does
// not exist yet. Forwarding it verbatim made the child's config loader abort
// with "Error reading config file: open <path>: no such file or directory"
// (an explicit --config is treated as fatal-if-missing), so the jump failed and
// left an error line on the terminal on every attempt.
//
// Guarding the forward keeps the propagation honest: a real config is passed
// through, and when there isn't one the child rediscovers the same built-in
// defaults on its own.
func ConfigArgPath(path string) string {
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}
