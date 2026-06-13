// Package trail answers "who changed this resource, and when?" via
// CloudTrail's LookupEvents API. LookupEvents covers the last 90 days of
// management events with no trail or S3 bucket setup required, which makes it
// the right zero-config source for incident attribution.
package trail

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

// Event is one CloudTrail management event affecting a resource, reduced to
// the facts that matter in an incident: when, what, who, from where.
type Event struct {
	Time      time.Time `json:"time"`
	EventName string    `json:"eventName"`
	Principal string    `json:"principal"`
	SourceIP  string    `json:"sourceIp"`
	ReadOnly  bool      `json:"readOnly"`
}

// Options tunes a Lookup. The zero value means: mutations only, last 90 days
// (the LookupEvents window), up to DefaultLimit events.
type Options struct {
	// Since bounds the search; zero means the full 90-day window.
	Since time.Time
	// Limit caps the number of returned events; <=0 means DefaultLimit.
	Limit int
	// IncludeReadOnly also returns read-only (Describe*/List*/Get*) events,
	// which otherwise drown out the mutations the caller is after.
	IncludeReadOnly bool
}

// DefaultLimit is the event cap when Options.Limit is unset.
const DefaultLimit = 50

// maxPages bounds pagination: LookupEvents is rate-limited to 2 TPS, so an
// unbounded scan of a chatty resource could take minutes. 8 pages × 50 events
// is plenty to find recent mutations.
const maxPages = 8

// pageInterval keeps successive page fetches under the 2 TPS service limit.
const pageInterval = 600 * time.Millisecond

// apiMaxResults is the LookupEvents per-page ceiling.
const apiMaxResults = 50

// Lookup fetches CloudTrail events that reference the resource, newest first.
// resourceID should be the bare resource name/ID as CloudTrail records it
// (use LookupValue to derive it from an ARN). Pages are fetched serially to
// respect the API's 2 TPS limit.
//
// The returned truncated flag is true when the scan stopped at the maxPages
// safety cap with more events still available — i.e. the result is an
// incomplete prefix of the matching history, not because the caller's Limit
// was reached. Callers should surface this so a missing older event isn't
// mistaken for "no such event".
func Lookup(ctx context.Context, cfg aws.Config, region, resourceID string, opts Options) (events []Event, truncated bool, err error) {
	if resourceID == "" {
		return nil, false, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	ctCfg := cfg.Copy()
	if region != "" && region != "global" {
		ctCfg.Region = region
	}
	client := cloudtrail.NewFromConfig(ctCfg)

	// Don't fetch a full page of 50 when the caller wants fewer; cap the page
	// size at the limit so a small Limit doesn't pull (and discard) the rest.
	pageSize := int32(apiMaxResults)
	if limit < apiMaxResults {
		pageSize = int32(limit)
	}

	input := &cloudtrail.LookupEventsInput{
		LookupAttributes: []types.LookupAttribute{{
			AttributeKey:   types.LookupAttributeKeyResourceName,
			AttributeValue: aws.String(resourceID),
		}},
		MaxResults: aws.Int32(pageSize),
	}
	if !opts.Since.IsZero() {
		input.StartTime = aws.Time(opts.Since)
	}

	for page := 0; page < maxPages; page++ {
		if page > 0 {
			select {
			case <-ctx.Done():
				return events, false, ctx.Err()
			case <-time.After(pageInterval):
			}
		}
		resp, err := client.LookupEvents(ctx, input)
		if err != nil {
			return events, false, err
		}
		for _, e := range resp.Events {
			ev := summarize(aws.ToString(e.Username), aws.ToString(e.ReadOnly), aws.ToString(e.CloudTrailEvent))
			ev.Time = aws.ToTime(e.EventTime)
			ev.EventName = aws.ToString(e.EventName)
			if ev.ReadOnly && !opts.IncludeReadOnly {
				continue
			}
			events = append(events, ev)
			if len(events) >= limit {
				return events, false, nil
			}
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			return events, false, nil // reached the end of the matching events
		}
		input.NextToken = resp.NextToken
	}
	// Stopped at the page cap with a NextToken still pending: more events exist.
	return events, true, nil
}

// rawCTEvent is the subset of the CloudTrail event record JSON needed to
// attribute an event to a principal and source.
type rawCTEvent struct {
	SourceIPAddress string `json:"sourceIPAddress"`
	ReadOnly        bool   `json:"readOnly"`
	UserIdentity    struct {
		Type        string `json:"type"`
		Arn         string `json:"arn"`
		PrincipalID string `json:"principalId"`
		AccountID   string `json:"accountId"`
		InvokedBy   string `json:"invokedBy"`
	} `json:"userIdentity"`
}

// summarize extracts principal (short form), source IP and read-only flag
// from a CloudTrail event record. username and readOnly come from the
// LookupEvents response fields and act as fallbacks when the record JSON is
// missing or unparsable. Pure: fixture-testable without AWS.
func summarize(username, readOnly, rawJSON string) Event {
	ev := Event{
		Principal: username,
		SourceIP:  "-",
		ReadOnly:  strings.EqualFold(readOnly, "true"),
	}

	var raw rawCTEvent
	if rawJSON == "" || json.Unmarshal([]byte(rawJSON), &raw) != nil {
		if ev.Principal == "" {
			ev.Principal = "-"
		}
		return ev
	}

	if raw.SourceIPAddress != "" {
		ev.SourceIP = raw.SourceIPAddress
	}
	if readOnly == "" {
		ev.ReadOnly = raw.ReadOnly
	}

	switch {
	case raw.UserIdentity.Type == "Root":
		ev.Principal = "root"
		if raw.UserIdentity.AccountID != "" {
			ev.Principal += " (" + raw.UserIdentity.AccountID + ")"
		}
	case raw.UserIdentity.Type == "AWSService" && raw.UserIdentity.InvokedBy != "":
		ev.Principal = raw.UserIdentity.InvokedBy
	case raw.UserIdentity.Arn != "":
		ev.Principal = ShortPrincipal(raw.UserIdentity.Arn)
	case raw.UserIdentity.InvokedBy != "":
		ev.Principal = raw.UserIdentity.InvokedBy
	case raw.UserIdentity.PrincipalID != "":
		ev.Principal = raw.UserIdentity.PrincipalID
	}
	if ev.Principal == "" {
		ev.Principal = "-"
	}
	return ev
}

// ShortPrincipal reduces a principal ARN to the short form people actually
// say out loud: "role/deploy-pipeline", "user/alice", "root". Assumed-role
// session ARNs collapse to the underlying role. Anything unrecognized (e.g. a
// service principal like cloudformation.amazonaws.com) passes through as-is.
func ShortPrincipal(arn string) string {
	if i := strings.Index(arn, ":assumed-role/"); i >= 0 {
		rest := arn[i+len(":assumed-role/"):]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			rest = rest[:slash]
		}
		return "role/" + rest
	}
	for _, kind := range []string{":role/", ":user/", ":group/"} {
		if i := strings.Index(arn, kind); i >= 0 {
			return arn[i+1:]
		}
	}
	if strings.HasSuffix(arn, ":root") {
		return "root"
	}
	return arn
}

// LookupValue derives the value to pass as CloudTrail's ResourceName lookup
// attribute. CloudTrail records bare resource names/IDs, so an ARN is reduced
// to its final resource segment: arn:aws:ec2:…:instance/i-0abc → i-0abc,
// arn:aws:lambda:…:function:my-fn → my-fn, arn:aws:s3:::bucket → bucket.
// Non-ARN input passes through unchanged.
func LookupValue(input string) string {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "arn:") {
		return input
	}
	parts := strings.SplitN(input, ":", 6)
	if len(parts) < 6 {
		return input
	}
	resource := parts[5]
	if i := strings.LastIndexByte(resource, '/'); i >= 0 {
		return resource[i+1:]
	}
	if i := strings.LastIndexByte(resource, ':'); i >= 0 {
		return resource[i+1:]
	}
	return resource
}
