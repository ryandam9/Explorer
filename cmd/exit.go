package cmd

// exitError lets a command request a specific process exit code without
// calling os.Exit itself, so command logic stays returnable (and testable) and
// the single exit point lives in main. An empty msg means the error only
// signals an outcome (e.g. audit findings) and should be silenced so Cobra
// prints no "Error:" line.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// ExitCode returns the process exit code main should use for this error.
func (e *exitError) ExitCode() int { return e.code }
