package consolelink

import (
	"os"
	"os/exec"
	"runtime"
)

// CanOpenBrowser reports whether this looks like a local, GUI-capable
// session — the only case where opening a browser is helpful rather than a
// no-op on some remote host.
func CanOpenBrowser() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if os.Getenv("BROWSER") != "" {
		return true
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	}
}

// Open launches the URL in the user's browser, preferring $BROWSER over the
// OS opener. Best-effort: the command is started, not waited on.
func Open(url string) error {
	if b := os.Getenv("BROWSER"); b != "" {
		return exec.Command(b, url).Start()
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
