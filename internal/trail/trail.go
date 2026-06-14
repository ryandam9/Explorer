// Package trail answers "who changed this resource, and when?" via
// CloudTrail's LookupEvents API. LookupEvents covers the last 90 days of
// management events with no trail or S3 bucket setup required, which makes it
// the right zero-config source for incident attribution.
package trail

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

// Event is one CloudTrail management event affecting a resource, reduced to
// the facts that matter in an incident: when, what, who, from where, and
// whether the call failed.
type Event struct {
	Time      time.Time `json:"time"`
	EventName string    `json:"eventName"`
	Principal string    `json:"principal"`
	SourceIP  string    `json:"sourceIp"`
	ReadOnly  bool      `json:"readOnly"`
	// ErrorCode is the CloudTrail errorCode when the call failed (e.g.
	// "AccessDenied", "UnauthorizedOperation"); empty when it succeeded. A
	// burst of these is a strong recon / misconfiguration signal.
	ErrorCode string `json:"errorCode,omitempty"`
}

// Filter selects which events LookupEvents returns. LookupEvents accepts at
// most one lookup attribute, so set at most one field; the zero value is an
// unfiltered account-wide activity feed. ResourceName is the historical
// resource-scoped lookup ("who changed this"); Principal, EventName and
// EventSource are the activity-feed pivots ("everything alice did", "every
// TerminateInstances", "all of EC2").
type Filter struct {
	ResourceName string
	Principal    string // CloudTrail "Username" attribute
	EventName    string
	EventSource  string // e.g. "ec2.amazonaws.com"
}

// attribute maps the filter to its single CloudTrail LookupAttribute. The
// bool is false for the zero filter (no attribute → account-wide feed).
func (f Filter) attribute() (types.LookupAttribute, bool) {
	switch {
	case f.ResourceName != "":
		return lookupAttr(types.LookupAttributeKeyResourceName, f.ResourceName), true
	case f.Principal != "":
		return lookupAttr(types.LookupAttributeKeyUsername, f.Principal), true
	case f.EventName != "":
		return lookupAttr(types.LookupAttributeKeyEventName, f.EventName), true
	case f.EventSource != "":
		return lookupAttr(types.LookupAttributeKeyEventSource, f.EventSource), true
	}
	return types.LookupAttribute{}, false
}

func lookupAttr(key types.LookupAttributeKey, value string) types.LookupAttribute {
	return types.LookupAttribute{AttributeKey: key, AttributeValue: aws.String(value)}
}

// lookupAttributes chooses the single server-side LookupAttribute for a lookup
// (LookupEvents accepts at most one). A set Filter field wins. Otherwise, the
// account-wide feed filters to mutations server-side (ReadOnly=false) whenever
// read-only events aren't wanted — this is the key lever that keeps the feed
// from drowning in Describe*/List*/Get* noise, since CloudTrail returns events
// newest-first and there is no other way to exclude reads before they consume
// the page-scan budget. When read-only events are requested (--read-events)
// there is no attribute and the API returns everything.
func lookupAttributes(f Filter, opts Options) []types.LookupAttribute {
	if attr, ok := f.attribute(); ok {
		return []types.LookupAttribute{attr}
	}
	if !opts.IncludeReadOnly {
		return []types.LookupAttribute{lookupAttr(types.LookupAttributeKeyReadOnly, "false")}
	}
	return nil
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
	// ErrorsOnly keeps only events that carry an errorCode (failed or denied
	// calls) — the security-triage view of the feed.
	ErrorsOnly bool
	// HideEvents lists event-name patterns to drop from the results, sourced
	// from the config file's `trail.hideEvents`. Matching is case-insensitive;
	// a trailing "*" makes the pattern a prefix match ("Describe*" hides every
	// describe call). Empty hides nothing. See HideMatcher.
	HideEvents []string
	// MaxPages overrides the page-scan cap (0 = the default for the lookup
	// kind). Each page is up to 50 events; deeper scans cost ~600ms per page.
	MaxPages int
}

// HideMatcher compiles a list of event-name patterns into a predicate that
// reports whether an event name should be hidden from the feed. It backs the
// config file's `trail.hideEvents` so users can permanently suppress noisy
// events (e.g. AssumeRole, ConsoleLogin) without re-passing CLI flags.
//
// Matching is case-insensitive. A trailing "*" turns the pattern into a prefix
// match, so "Describe*" hides DescribeInstances, DescribeVolumes, and so on; a
// pattern without "*" must equal the event name exactly. A nil or empty pattern
// list hides nothing.
func HideMatcher(patterns []string) func(name string) bool {
	type rule struct {
		text   string
		prefix bool
	}
	rules := make([]rule, 0, len(patterns))
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			rules = append(rules, rule{text: strings.TrimSuffix(p, "*"), prefix: true})
		} else {
			rules = append(rules, rule{text: p})
		}
	}
	if len(rules) == 0 {
		return func(string) bool { return false }
	}
	return func(name string) bool {
		n := strings.ToLower(name)
		for _, r := range rules {
			if r.prefix {
				if strings.HasPrefix(n, r.text) {
					return true
				}
			} else if n == r.text {
				return true
			}
		}
		return false
	}
}

// DefaultLimit is the event cap when Options.Limit is unset.
const DefaultLimit = 50

// Page caps bound pagination: LookupEvents is rate-limited to 2 TPS, so an
// unbounded scan could take minutes. A pivoted lookup (resource, principal,
// event, source) matches few events, so a shallow cap finds them. The
// unfiltered account-wide feed must page much deeper: its newest events are
// dominated by read-only noise, so a shallow cap can return nothing useful
// while real mutations sit just past it.
const (
	pivotPageCap = 8  // ~400 events for an attribute-filtered lookup
	feedPageCap  = 20 // ~1000 events for the account-wide feed
)

// DeepFeedPageCap is a deeper page cap for the interactive feed. When read-only
// events are filtered out server-side (trail.hideEvents), each page yields far
// fewer countable events, so the account-wide scan must page further to fill
// the limit past the read-only noise. Set it via Options.MaxPages. ~2500
// events; at the 2 TPS limit this is a worst case of ~30s per region, but the
// scan stops as soon as the limit is reached.
const DeepFeedPageCap = 50

// pageCapFor returns the page cap for a lookup. Options.MaxPages overrides it
// (the TUI scans deeper); otherwise the account-wide feed gets the deeper cap.
func pageCapFor(f Filter, opts Options) int {
	if opts.MaxPages > 0 {
		return opts.MaxPages
	}
	if _, hasAttr := f.attribute(); !hasAttr {
		return feedPageCap
	}
	return pivotPageCap
}

// pageInterval keeps successive page fetches under the 2 TPS service limit.
const pageInterval = 600 * time.Millisecond

// apiMaxResults is the LookupEvents per-page ceiling.
const apiMaxResults = 50

// Lookup fetches CloudTrail events that reference the resource, newest first.
// resourceID should be the bare resource name/ID as CloudTrail records it
// (use LookupValue to derive it from an ARN). It is the resource-scoped
// special case of LookupFiltered, kept for the existing "who changed this"
// callers; an empty resourceID returns no events (rather than the whole feed).
func Lookup(ctx context.Context, cfg aws.Config, region, resourceID string, opts Options) (events []Event, truncated bool, err error) {
	if resourceID == "" {
		return nil, false, nil
	}
	return LookupFiltered(ctx, cfg, region, Filter{ResourceName: resourceID}, opts)
}

// LookupFiltered fetches CloudTrail events matching the filter, newest first.
// The zero Filter is an account-wide activity feed; a single set field pivots
// on that attribute (resource, principal, event name, or source). For the
// account-wide feed, mutations are filtered server-side (ReadOnly=false) unless
// opts.IncludeReadOnly is set — see lookupAttributes. Pages are fetched serially
// to respect the API's 2 TPS limit.
//
// The returned truncated flag is true when the scan stopped at the maxPages
// safety cap with more events still available — i.e. the result is an
// incomplete prefix of the matching history, not because the caller's Limit
// was reached. Callers should surface this so a missing older event isn't
// mistaken for "no such event".
func LookupFiltered(ctx context.Context, cfg aws.Config, region string, f Filter, opts Options) (events []Event, truncated bool, err error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	ctCfg := cfg.Copy()
	if region != "" && region != "global" {
		ctCfg.Region = region
	}
	client := cloudtrail.NewFromConfig(ctCfg)

	// Client-side filtering (mutations-only, errors-only) discards events after
	// the fetch, so a small page would return fewer than Limit even when more
	// match. Only cap the page size to Limit when nothing is filtered out.
	pageSize := int32(apiMaxResults)
	if limit < apiMaxResults && opts.IncludeReadOnly && !opts.ErrorsOnly {
		pageSize = int32(limit)
	}

	input := &cloudtrail.LookupEventsInput{MaxResults: aws.Int32(pageSize)}
	input.LookupAttributes = lookupAttributes(f, opts)
	if !opts.Since.IsZero() {
		input.StartTime = aws.Time(opts.Since)
	}

	hidden := HideMatcher(opts.HideEvents)
	// An explicit single-event lookup means the caller is asking for exactly
	// that event, so never hide it even if a pattern would match (e.g. looking
	// up DescribeInstances while "Describe*" is in trail.hideEvents).
	if f.EventName != "" {
		hidden = func(string) bool { return false }
	}

	maxPages := pageCapFor(f, opts)
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
			if opts.ErrorsOnly && ev.ErrorCode == "" {
				continue
			}
			if hidden(ev.EventName) {
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

// LookupFilteredRegions runs LookupFiltered across several regions concurrently
// and merges the results newest-first, capped at the effective limit. It is the
// account-wide / --all-regions path: each region's CloudTrail has its own 2 TPS
// budget, so the regions are queried in parallel. truncated is true when any
// region truncated or the merged set exceeded the limit (older events exist).
//
// Collection is best-effort: a per-region failure is recorded but does not
// abort the others. An error is returned only when every region failed.
func LookupFilteredRegions(ctx context.Context, cfg aws.Config, regions []string, f Filter, opts Options) (events []Event, truncated bool, err error) {
	switch len(regions) {
	case 0:
		return nil, false, nil
	case 1:
		return LookupFiltered(ctx, cfg, regions[0], f, opts)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	type result struct {
		events    []Event
		truncated bool
		err       error
	}
	results := make([]result, len(regions))
	var wg sync.WaitGroup
	sem := make(chan struct{}, regionConcurrency)
	for i, region := range regions {
		wg.Add(1)
		go func(i int, region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			evs, trunc, e := LookupFiltered(ctx, cfg, region, f, opts)
			results[i] = result{events: evs, truncated: trunc, err: e}
		}(i, region)
	}
	wg.Wait()

	failures := 0
	for _, r := range results {
		if r.err != nil {
			failures++
			if err == nil {
				err = r.err
			}
			continue
		}
		events = append(events, r.events...)
		truncated = truncated || r.truncated
	}
	// Every region failed: surface the (first) error rather than empty success.
	if failures == len(regions) {
		return nil, false, err
	}

	sort.Slice(events, func(i, j int) bool { return events[i].Time.After(events[j].Time) })
	if len(events) > limit {
		events = events[:limit]
		truncated = true
	}
	return events, truncated, nil
}

// regionConcurrency bounds how many regions are queried at once.
const regionConcurrency = 8

// rawCTEvent is the subset of the CloudTrail event record JSON needed to
// attribute an event to a principal and source.
type rawCTEvent struct {
	SourceIPAddress string `json:"sourceIPAddress"`
	ReadOnly        bool   `json:"readOnly"`
	ErrorCode       string `json:"errorCode"`
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
	ev.ErrorCode = raw.ErrorCode

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
