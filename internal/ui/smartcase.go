package ui

import (
	"regexp"
	"unicode"
)

// SmartCaseRegexp compiles a user-typed filter pattern with "smart case", the
// less(1)/ripgrep convention: a pattern with no uppercase letters matches
// case-insensitively (so "error" finds ERROR, Error and error), while an
// uppercase letter anywhere makes the match case-sensitive as typed (so
// "ERROR" finds only ERROR). An uppercase letter right after a backslash is a
// regex escape (\S, \W, \D, …), not a literal, and does not flip the mode.
// Shared by the CloudWatch log viewer's grep and the S3 preview's grep so the
// two filters always behave the same.
func SmartCaseRegexp(pattern string) (*regexp.Regexp, error) {
	esc := false
	for _, r := range pattern {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case unicode.IsUpper(r):
			return regexp.Compile(pattern)
		}
	}
	return regexp.Compile("(?i)" + pattern)
}
