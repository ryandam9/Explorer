package awserr

import (
	"errors"
	"strings"

	"github.com/aws/smithy-go"
)

// Expired-credential detection (AXE-022). An expired AWS SSO session is the
// most common auth failure for human users, and the SDK reports it as a wall
// of wrapped errors. These helpers classify the failure and produce the one
// line the user actually needs: the command that fixes it.

// expiredErrorCodes are AWS error codes that indicate expired (rather than
// missing or insufficient) credentials.
var expiredErrorCodes = map[string]bool{
	"ExpiredToken":          true,
	"ExpiredTokenException": true,
	"RequestExpired":        true,
	// Returned by the SSO OIDC service when a cached token can no longer be
	// refreshed.
	"InvalidGrantException": true,
}

// expiredErrorPhrases are fragments (lower-cased) the SDK and the STS/SSO
// services put in expired- or stale-credential error chains. Deliberately
// NOT in this list: "failed to refresh cached credentials" — the SDK uses
// that for any provider failure, including "no credentials at all" (e.g. no
// IMDS role), which a login hint would misdescribe.
var expiredErrorPhrases = []string{
	"token has expired",
	"token included in the request is expired",
	"sso session has expired",
	"sso token has expired",
	"failed to read cached sso token", // not logged in (or logged out) — same fix
	"expired and refresh failed",
}

// IsExpiredCreds reports whether err is an expired-credentials error — an
// expired SSO session, an expired STS token, or a cached credential the SDK
// could not refresh. Distinct from IsAuthError, which is about insufficient
// permissions of valid credentials.
func IsExpiredCreds(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && expiredErrorCodes[apiErr.ErrorCode()] {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, phrase := range expiredErrorPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

// isSSOError reports whether the error chain points at AWS SSO (IAM Identity
// Center) rather than plain expired credentials.
func isSSOError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "sso")
}

// Classify maps a collection error onto the (code, message) pair the error
// surfaces use: expired credentials get the login hint, permission errors
// get the friendly IAM message, anything else passes through. service names
// the AWS service whose call failed; profile may be empty.
func Classify(err error, service, profile string) (code, msg string) {
	switch {
	case IsExpiredCreds(err):
		hint, _ := LoginHint(err, profile)
		return "ExpiredCredentials", hint
	case IsAuthError(err):
		return "AccessDenied", FriendlyMessage(err, service)
	default:
		return "CollectionError", err.Error()
	}
}

// LoginHint returns the actionable one-liner for an expired-credentials
// error, naming the exact command to run. profile is the active AWS profile
// ("" or "default" when none was chosen explicitly). ok is false when err is
// not an expired-credentials error.
func LoginHint(err error, profile string) (string, bool) {
	if !IsExpiredCreds(err) {
		return "", false
	}

	profileFlag := ""
	forProfile := ""
	if profile != "" && profile != "default" {
		profileFlag = " --profile " + profile
		forProfile = " for profile '" + profile + "'"
	}

	if isSSOError(err) {
		return "AWS SSO session" + forProfile + " is expired or missing — run: aws sso login" + profileFlag, true
	}
	return "AWS credentials" + forProfile + " have expired — refresh them " +
		"(aws sso login" + profileFlag + ", re-assume the role, or re-export your AWS_* environment variables)", true
}
