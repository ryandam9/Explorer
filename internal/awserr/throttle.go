package awserr

import (
	"errors"
	"strings"

	"github.com/aws/smithy-go"
)

// Throttling detection (§7). A per-item sweep against a low-TPS API (the
// classic offender is CloudWatch Logs DescribeSubscriptionFilters, one call per
// log group) self-throttles on a large account: each call gives up after the
// SDK's retries with a "Rate exceeded" error. Those errors are transient and
// near-identical, but each carries a unique RequestID, so without canonicalizing
// them the recorder's dedup can't collapse the storm and the user sees dozens of
// lines for one underlying condition. Classify routes throttles to a stable
// (code, message) pair so a whole storm collapses to a single line.

// throttleErrorCodes are AWS error codes that indicate request throttling /
// rate-limiting rather than a permission or credential failure. The exact code
// varies by service, so the set is deliberately broad.
var throttleErrorCodes = map[string]bool{
	"Throttling":                             true,
	"ThrottlingException":                    true,
	"ThrottledException":                     true,
	"RequestThrottled":                       true,
	"RequestThrottledException":              true,
	"TooManyRequestsException":               true,
	"RequestLimitExceeded":                   true,
	"ProvisionedThroughputExceededException": true,
	"SlowDown":                               true, // S3
	"EC2ThrottledException":                  true,
}

// throttlePhrases are fragments (lower-cased) that mark a throttle in an error
// chain even when the SDK's retry wrapper ("exceeded maximum number of
// attempts, N") has buried the underlying error code.
var throttlePhrases = []string{
	"rate exceeded",
	"throttl", // matches Throttling / Throttled / ThrottlingException
	"too many requests",
}

// IsThrottle reports whether err is an AWS throttling / rate-limit error. These
// are transient: the call would likely succeed on a slower retry, so a throttle
// narrows (rather than denies) coverage, and several identical ones should
// collapse to a single line (§7). Distinct from IsAuthError (permissions) and
// IsExpiredCreds (credentials).
func IsThrottle(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && throttleErrorCodes[apiErr.ErrorCode()] {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, p := range throttlePhrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// throttleMessage is the stable, RequestID-free line for a throttling error.
// It is constant per service, so the recorder collapses a storm of identical
// throttles (one per item in a large sweep) into a single line (§7) instead of
// one line per RequestID.
func throttleMessage(service string) string {
	subject := "the request"
	if service != "" {
		subject = service
	}
	return "AWS throttled " + subject + " (rate exceeded); some results may be incomplete — " +
		"retry shortly, lower --max-concurrency, or narrow the scope (e.g. -r <region>)."
}
