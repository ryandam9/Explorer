package auth

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProfiles returns the profile names defined in the shared AWS config and
// credentials files (~/.aws/config and ~/.aws/credentials, honoring the
// AWS_CONFIG_FILE and AWS_SHARED_CREDENTIALS_FILE overrides). Best-effort:
// missing or unreadable files contribute no names. "default" is listed first
// when present; the rest are sorted.
func ListProfiles() []string {
	seen := map[string]bool{}

	for _, f := range []struct {
		path         string
		stripProfile bool // config file sections are "[profile name]"
	}{
		{sharedFilePath("AWS_SHARED_CREDENTIALS_FILE", "credentials"), false},
		{sharedFilePath("AWS_CONFIG_FILE", "config"), true},
	} {
		for _, name := range iniSectionNames(f.path) {
			if f.stripProfile {
				if name == "default" {
					seen["default"] = true
					continue
				}
				rest, ok := strings.CutPrefix(name, "profile ")
				if !ok {
					continue // sso-session / services sections etc.
				}
				name = strings.TrimSpace(rest)
			}
			if name != "" {
				seen[name] = true
			}
		}
	}

	var names []string
	for n := range seen {
		if n != "default" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if seen["default"] {
		names = append([]string{"default"}, names...)
	}
	return names
}

// sharedFilePath resolves a shared AWS file location: the env override when
// set, otherwise ~/.aws/<name>.
func sharedFilePath(envVar, name string) string {
	if p := os.Getenv(envVar); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aws", name)
}

// iniSectionNames returns the [section] names of an INI-style file.
func iniSectionNames(path string) []string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			names = append(names, strings.TrimSpace(line[1:len(line)-1]))
		}
	}
	return names
}
