package lambdatui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// How a function is invoked and released: its published versions and aliases
// (with weighted routing), function URLs, provisioned concurrency, and the
// asynchronous-invocation settings (retries, event age, destinations). Fetched
// with the detail view, each read best-effort: a denied or failed call leaves
// that panel saying so, never "none".

// VersionInfo is one published version.
type VersionInfo struct {
	Version      string
	LastModified string
	Description  string
}

// AliasInfo is one alias and the version(s) it routes to.
type AliasInfo struct {
	Name        string
	Version     string
	Weights     map[string]float64 // additional versions → share (0..1)
	Description string
}

// URLInfo is one function URL (on $LATEST or an alias).
type URLInfo struct {
	URL        string
	AuthType   string // NONE or AWS_IAM
	Qualifier  string // alias the URL points at ("" = unqualified)
	CORSOrigin []string
	InvokeMode string
}

// ProvisionedInfo is one qualifier's provisioned concurrency.
type ProvisionedInfo struct {
	Qualifier                       string
	Requested, Allocated, Available int32
	Status, Reason                  string
}

// AsyncInfo is the function's asynchronous-invocation configuration. Default
// is set when none is configured, in which case Lambda retries twice and
// keeps events for six hours.
type AsyncInfo struct {
	Default        bool
	MaxRetries     *int32
	MaxEventAgeSec *int32
	OnSuccess      string
	OnFailure      string
}

// invokeDetailAPI is the slice of the Lambda API these reads use.
type invokeDetailAPI interface {
	lambda.ListAliasesAPIClient
	lambda.ListVersionsByFunctionAPIClient
	lambda.ListFunctionUrlConfigsAPIClient
	lambda.ListProvisionedConcurrencyConfigsAPIClient
	GetFunctionEventInvokeConfig(ctx context.Context, in *lambda.GetFunctionEventInvokeConfigInput, optFns ...func(*lambda.Options)) (*lambda.GetFunctionEventInvokeConfigOutput, error)
}

// readError turns a failed read into the panel's note: denied reads name the
// permission, anything else is logged and shown — never mistaken for "none".
func readError(err error, what, perm, region, name string) string {
	if apiErrorCodeIs(err, "AccessDeniedException", "AccessDenied") {
		return fmt.Sprintf("Access denied: not permitted to read the %s (%s).", what, perm)
	}
	slog.Warn("Lambda detail read failed", "read", what, "region", region, "function", name, "error", err.Error())
	return fmt.Sprintf("Could not read the %s: %v", what, err)
}

// loadInvokeDetail fills d's versions, aliases, URLs, provisioned concurrency
// and async config — five independent reads, run concurrently, each writing
// its own fields.
func loadInvokeDetail(ctx context.Context, cl invokeDetailAPI, region, name string, d *FunctionDetail) {
	fn := aws.String(name)
	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	run(func() {
		p := lambda.NewListVersionsByFunctionPaginator(cl, &lambda.ListVersionsByFunctionInput{FunctionName: fn})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				d.VersionsErr = readError(err, "versions", "lambda:ListVersionsByFunction", region, name)
				return
			}
			for _, v := range page.Versions {
				if ver := aws.ToString(v.Version); ver != "$LATEST" {
					d.Versions = append(d.Versions, VersionInfo{Version: ver, LastModified: aws.ToString(v.LastModified), Description: aws.ToString(v.Description)})
				}
			}
		}
		sort.Slice(d.Versions, func(i, j int) bool { return versionNum(d.Versions[i].Version) > versionNum(d.Versions[j].Version) })
	})
	run(func() {
		p := lambda.NewListAliasesPaginator(cl, &lambda.ListAliasesInput{FunctionName: fn})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				d.AliasesErr = readError(err, "aliases", "lambda:ListAliases", region, name)
				return
			}
			for _, a := range page.Aliases {
				ai := AliasInfo{Name: aws.ToString(a.Name), Version: aws.ToString(a.FunctionVersion), Description: aws.ToString(a.Description)}
				if a.RoutingConfig != nil && len(a.RoutingConfig.AdditionalVersionWeights) > 0 {
					ai.Weights = a.RoutingConfig.AdditionalVersionWeights
				}
				d.Aliases = append(d.Aliases, ai)
			}
		}
		sort.Slice(d.Aliases, func(i, j int) bool { return d.Aliases[i].Name < d.Aliases[j].Name })
	})
	run(func() {
		p := lambda.NewListFunctionUrlConfigsPaginator(cl, &lambda.ListFunctionUrlConfigsInput{FunctionName: fn})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				d.URLsErr = readError(err, "function URLs", "lambda:ListFunctionUrlConfigs", region, name)
				return
			}
			for _, u := range page.FunctionUrlConfigs {
				d.URLs = append(d.URLs, mapURL(u))
			}
		}
	})
	run(func() {
		p := lambda.NewListProvisionedConcurrencyConfigsPaginator(cl, &lambda.ListProvisionedConcurrencyConfigsInput{FunctionName: fn})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				d.ProvisionedErr = readError(err, "provisioned concurrency", "lambda:ListProvisionedConcurrencyConfigs", region, name)
				return
			}
			for _, pc := range page.ProvisionedConcurrencyConfigs {
				d.Provisioned = append(d.Provisioned, ProvisionedInfo{
					Qualifier: qualifierOf(aws.ToString(pc.FunctionArn)),
					Requested: aws.ToInt32(pc.RequestedProvisionedConcurrentExecutions),
					Allocated: aws.ToInt32(pc.AllocatedProvisionedConcurrentExecutions),
					Available: aws.ToInt32(pc.AvailableProvisionedConcurrentExecutions),
					Status:    string(pc.Status), Reason: aws.ToString(pc.StatusReason),
				})
			}
		}
	})
	run(func() {
		out, err := cl.GetFunctionEventInvokeConfig(ctx, &lambda.GetFunctionEventInvokeConfigInput{FunctionName: fn})
		switch {
		case err == nil:
			a := &AsyncInfo{MaxRetries: out.MaximumRetryAttempts, MaxEventAgeSec: out.MaximumEventAgeInSeconds}
			if dc := out.DestinationConfig; dc != nil {
				if dc.OnSuccess != nil {
					a.OnSuccess = aws.ToString(dc.OnSuccess.Destination)
				}
				if dc.OnFailure != nil {
					a.OnFailure = aws.ToString(dc.OnFailure.Destination)
				}
			}
			d.Async = a
		case apiErrorCodeIs(err, "ResourceNotFoundException"):
			// No configuration: Lambda's defaults apply (not "unknown").
			d.Async = &AsyncInfo{Default: true}
		default:
			d.AsyncErr = readError(err, "async invocation config", "lambda:GetFunctionEventInvokeConfig", region, name)
		}
	})
	wg.Wait()
}

func mapURL(u lambdatypes.FunctionUrlConfig) URLInfo {
	out := URLInfo{
		URL:        aws.ToString(u.FunctionUrl),
		AuthType:   string(u.AuthType),
		Qualifier:  qualifierOf(aws.ToString(u.FunctionArn)),
		InvokeMode: string(u.InvokeMode),
	}
	if u.Cors != nil {
		out.CORSOrigin = u.Cors.AllowOrigins
	}
	return out
}

// qualifierOf returns the alias/version suffix of a qualified function ARN
// ("" for an unqualified one).
func qualifierOf(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 8 {
		return parts[7]
	}
	return ""
}

func versionNum(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

// maxVersionsShown bounds the versions panel; the newest are what matter and
// a function can hold hundreds.
const maxVersionsShown = 15

func versionsBody(d FunctionDetail) string {
	var b strings.Builder
	switch {
	case d.AliasesErr != "":
		b.WriteString("  " + d.AliasesErr + "\n")
	case len(d.Aliases) == 0:
		b.WriteString("  (no aliases — callers invoke $LATEST or a version directly)\n")
	default:
		b.WriteString("  aliases:\n")
		for _, a := range d.Aliases {
			line := fmt.Sprintf("    %-12s → v%s", a.Name, a.Version)
			if len(a.Weights) > 0 {
				main := 1.0
				var extra []string
				for _, v := range sortedMapKeysF(a.Weights) {
					main -= a.Weights[v]
					extra = append(extra, fmt.Sprintf("v%s (%.0f%%)", v, a.Weights[v]*100))
				}
				line = fmt.Sprintf("    %-12s → v%s (%.0f%%) + %s", a.Name, a.Version, main*100, strings.Join(extra, " + "))
			}
			if pc := provisionedFor(d, a.Name); pc != "" {
				line += " · " + pc
			}
			if a.Description != "" {
				line += " · " + a.Description
			}
			b.WriteString(line + "\n")
		}
	}
	for _, p := range d.Provisioned {
		if !hasAlias(d, p.Qualifier) {
			b.WriteString(fmt.Sprintf("  v%s · %s\n", p.Qualifier, provisionedLabel(p)))
		}
	}
	if d.ProvisionedErr != "" {
		b.WriteString("  " + d.ProvisionedErr + "\n")
	}
	switch {
	case d.VersionsErr != "":
		b.WriteString("  " + d.VersionsErr)
	case len(d.Versions) == 0:
		b.WriteString("  (no published versions — only $LATEST)")
	default:
		b.WriteString(fmt.Sprintf("  versions (%d):\n", len(d.Versions)))
		for i, v := range d.Versions {
			if i == maxVersionsShown {
				b.WriteString(fmt.Sprintf("    … %d older", len(d.Versions)-maxVersionsShown))
				break
			}
			line := fmt.Sprintf("    v%-5s %s", v.Version, shortTime(parseLambdaTime(v.LastModified)))
			if v.Description != "" {
				line += "  " + v.Description
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func hasAlias(d FunctionDetail, name string) bool {
	for _, a := range d.Aliases {
		if a.Name == name {
			return true
		}
	}
	return false
}

func provisionedFor(d FunctionDetail, qualifier string) string {
	for _, p := range d.Provisioned {
		if p.Qualifier == qualifier {
			return provisionedLabel(p)
		}
	}
	return ""
}

func provisionedLabel(p ProvisionedInfo) string {
	s := fmt.Sprintf("provisioned %d/%d (%s)", p.Allocated, p.Requested, strings.ToLower(p.Status))
	if p.Reason != "" {
		s += " — " + p.Reason
	}
	return s
}

func sortedMapKeysF(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return versionNum(keys[i]) < versionNum(keys[j]) })
	return keys
}

func urlBody(d FunctionDetail) string {
	if d.URLsErr != "" {
		return "  " + d.URLsErr
	}
	if len(d.URLs) == 0 {
		return "  (no function URL — not reachable over plain HTTPS)"
	}
	var b strings.Builder
	for i, u := range d.URLs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(dkv("URL", u.URL) + "\n")
		auth := u.AuthType
		if u.AuthType == "NONE" {
			auth = "NONE — ⚠ anyone on the internet can invoke it"
		}
		b.WriteString(dkv("Auth", auth) + "\n")
		b.WriteString(dkv("Points at", emptyDash(u.Qualifier)) + "\n")
		b.WriteString(dkv("Invoke mode", emptyDash(u.InvokeMode)) + "\n")
		b.WriteString(dkv("CORS origins", joinOrDash(u.CORSOrigin)))
	}
	return b.String()
}

func asyncBody(d FunctionDetail) string {
	var b strings.Builder
	switch {
	case d.AsyncErr != "":
		b.WriteString("  " + d.AsyncErr + "\n")
	case d.Async == nil:
	case d.Async.Default:
		b.WriteString(dkv("Retries", "2 (default)") + "\n")
		b.WriteString(dkv("Max event age", "6h (default)") + "\n")
		b.WriteString(dkv("On success", "") + "\n")
		b.WriteString(dkv("On failure", "") + "\n")
	default:
		a := d.Async
		retries, age := "2 (default)", "6h (default)"
		if a.MaxRetries != nil {
			retries = fmt.Sprintf("%d", *a.MaxRetries)
		}
		if a.MaxEventAgeSec != nil {
			age = formatTimeout(*a.MaxEventAgeSec)
		}
		b.WriteString(dkv("Retries", retries) + "\n")
		b.WriteString(dkv("Max event age", age) + "\n")
		b.WriteString(dkv("On success", a.OnSuccess) + "\n")
		b.WriteString(dkv("On failure", a.OnFailure) + "\n")
	}
	b.WriteString(dkv("Dead-letter queue", dlqLabel(d.DLQTarget)))
	return b.String()
}
