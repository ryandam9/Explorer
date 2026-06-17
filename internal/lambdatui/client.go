// Package lambdatui is the interactive AWS Lambda dashboard (AXE-046/AXE-047):
// a tabbed Bubble Tea TUI over functions, layers and event-source mappings,
// with an on-demand per-function configuration drill-down (memory, timeout,
// role, layers, VPC, env-var keys, dead-letter queue, reserved concurrency,
// code location and tags — secret-looking values redacted) and a deterministic
// runtime/health findings panel.
package lambdatui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// Function, Layer and EventSource are the dashboard's flattened view of each
// Lambda resource — only the fields the tables, detail panels, findings and
// console links need, annotated with their region. Everything here comes from
// the paginated list calls (no per-resource fan-out); the function detail panel
// fetches the richer GetFunction view on demand.
type Function struct {
	Name             string
	Region           string
	ARN              string
	Runtime          string // "" for container-image (Image) functions
	PackageType      string // Zip / Image
	MemoryMB         int32
	TimeoutSec       int32
	Handler          string
	CodeSize         int64
	Architectures    []string
	Role             string
	Description      string
	LastModified     time.Time
	State            string // Active / Inactive / Pending / Failed ("" when unreported)
	LastUpdateStatus string // Successful / Failed / InProgress
	EphemeralMB      int32
	TracingMode      string
	Layers           []string // layer-version ARNs
	VpcID            string
	SubnetIDs        []string
	SecurityGroupIDs []string
	DLQTargetArn     string
	EnvVarKeys       []string // keys only — values are never collected/rendered
	LogGroup         string   // explicit LoggingConfig group, else /aws/lambda/<name>
}

type Layer struct {
	Name             string
	Region           string
	ARN              string
	LatestVersion    int64
	LatestVersionARN string
	Runtimes         []string
	Architectures    []string
	Description      string
	CreatedDate      string
	License          string
}

type EventSource struct {
	UUID                 string
	Region               string
	ARN                  string
	FunctionName         string
	SourceLabel          string // short "service:resource" derived from the source
	State                string
	BatchSize            int32
	LastModified         time.Time
	LastProcessingResult string
}

// Inventory is the full set of Lambda resources gathered across regions.
type Inventory struct {
	Functions    []Function
	Layers       []Layer
	EventSources []EventSource
}

// Client holds one Lambda client per region plus the account ID (used for the
// console-link ARN fallback when a list response omits an ARN).
type Client struct {
	clients   map[string]*lambda.Client
	regions   []string
	accountID string
}

// NewClient builds per-region Lambda clients. When allRegions is true the
// region list is discovered via ec2:DescribeRegions, falling back to the
// built-in list when that call is denied.
func NewClient(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool) (*Client, error) {
	bootstrap := "us-east-1"
	if len(regions) > 0 {
		bootstrap = regions[0]
	}
	base, err := auth.BuildAWSConfig(ctx, awsCfg, bootstrap)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	if allRegions {
		regions = resolveRegions(ctx, base)
	}
	if len(regions) == 0 {
		regions = []string{bootstrap}
	}
	sort.Strings(regions)

	clients := make(map[string]*lambda.Client, len(regions))
	for _, r := range regions {
		rCfg := base.Copy()
		rCfg.Region = r
		clients[r] = lambda.NewFromConfig(rCfg)
	}
	return &Client{clients: clients, regions: regions, accountID: resolveAccountID(ctx, base)}, nil
}

// resolveAccountID looks up the caller's account ID; an empty string (when
// sts:GetCallerIdentity is denied) is harmless — the list calls already return
// full ARNs.
func resolveAccountID(ctx context.Context, cfg aws.Config) string {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		slog.Warn("Unable to resolve account ID for Lambda", "error", err.Error())
		return ""
	}
	return aws.ToString(out.Account)
}

// Regions returns the regions this client queries, sorted.
func (c *Client) Regions() []string { return c.regions }

// AccountID returns the resolved caller account ID (may be empty).
func (c *Client) AccountID() string { return c.accountID }

func (c *Client) clientFor(region string) *lambda.Client {
	if cl, ok := c.clients[region]; ok {
		return cl
	}
	for _, cl := range c.clients {
		return cl
	}
	return nil
}

func resolveRegions(ctx context.Context, cfg aws.Config) []string {
	client := awsec2.NewFromConfig(cfg)
	result, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		slog.Warn("Unable to list AWS regions; falling back to the built-in region list",
			"error", err.Error(), "regions", len(awsutil.FallbackRegions))
		return awsutil.FallbackRegions
	}
	var regions []string
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}
	if len(regions) == 0 {
		return awsutil.FallbackRegions
	}
	return regions
}

// LoadInventory lists every resource across regions. Per-region failures are
// soft; an error is returned only when every region fails completely.
func (c *Client) LoadInventory(ctx context.Context) (Inventory, error) {
	var (
		mu       sync.Mutex
		inv      Inventory
		firstErr error
		failures int
		wg       sync.WaitGroup
	)

	for _, region := range c.regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			regional, err := c.loadRegion(ctx, region)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("Lambda inventory failed", "region", region, "error", err.Error())
				return
			}
			inv.Functions = append(inv.Functions, regional.Functions...)
			inv.Layers = append(inv.Layers, regional.Layers...)
			inv.EventSources = append(inv.EventSources, regional.EventSources...)
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return Inventory{}, firstErr
	}

	inv.sort()
	return inv, nil
}

// loadRegion gathers a region's three resource listings concurrently (they are
// independent). Each is best-effort: a failure in one is logged but the rest
// proceed. Each goroutine writes a distinct Inventory field, so no locking is
// needed.
func (c *Client) loadRegion(ctx context.Context, region string) (Inventory, error) {
	cl := c.clientFor(region)
	var inv Inventory

	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}

	run(func() { inv.Functions = c.loadFunctions(ctx, cl, region) })
	run(func() { inv.Layers = c.loadLayers(ctx, cl, region) })
	run(func() { inv.EventSources = c.loadEventSources(ctx, cl, region) })
	wg.Wait()

	return inv, nil
}

// loadFunctions lists every function in the region (best-effort, paginated).
func (c *Client) loadFunctions(ctx context.Context, cl *lambda.Client, region string) []Function {
	var out []Function
	p := lambda.NewListFunctionsPaginator(cl, &lambda.ListFunctionsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			slog.Warn("ListFunctions failed", "region", region, "error", err.Error())
			break
		}
		for _, fn := range page.Functions {
			out = append(out, mapFunction(region, fn))
		}
	}
	return out
}

// loadLayers lists every layer in the region (best-effort, paginated).
func (c *Client) loadLayers(ctx context.Context, cl *lambda.Client, region string) []Layer {
	var out []Layer
	p := lambda.NewListLayersPaginator(cl, &lambda.ListLayersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			slog.Warn("ListLayers failed", "region", region, "error", err.Error())
			break
		}
		for _, l := range page.Layers {
			out = append(out, mapLayer(region, l))
		}
	}
	return out
}

// loadEventSources lists every event-source mapping in the region (best-effort,
// paginated).
func (c *Client) loadEventSources(ctx context.Context, cl *lambda.Client, region string) []EventSource {
	var out []EventSource
	p := lambda.NewListEventSourceMappingsPaginator(cl, &lambda.ListEventSourceMappingsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			slog.Warn("ListEventSourceMappings failed", "region", region, "error", err.Error())
			break
		}
		for _, m := range page.EventSourceMappings {
			out = append(out, mapEventSource(region, m))
		}
	}
	return out
}

// FunctionDetail fetches a function's full configuration on demand (one
// GetFunction call), flattened for the detail overlay with environment-variable
// values redacted. Reserved concurrency, code location and tags come only from
// GetFunction, so the panel fetches it rather than reusing the list view.
func (c *Client) FunctionDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(name)})
	if err != nil {
		return ResourceDetail{}, err
	}
	if out.Configuration == nil {
		return ResourceDetail{}, fmt.Errorf("function %q not found", name)
	}
	return buildFunctionDetail(region, name, out), nil
}

func mapFunction(region string, fn lambdatypes.FunctionConfiguration) Function {
	f := Function{
		Name:             aws.ToString(fn.FunctionName),
		Region:           region,
		ARN:              aws.ToString(fn.FunctionArn),
		Runtime:          string(fn.Runtime),
		PackageType:      string(fn.PackageType),
		MemoryMB:         aws.ToInt32(fn.MemorySize),
		TimeoutSec:       aws.ToInt32(fn.Timeout),
		Handler:          aws.ToString(fn.Handler),
		CodeSize:         fn.CodeSize,
		Role:             aws.ToString(fn.Role),
		Description:      aws.ToString(fn.Description),
		LastModified:     parseLambdaTime(aws.ToString(fn.LastModified)),
		State:            string(fn.State),
		LastUpdateStatus: string(fn.LastUpdateStatus),
	}
	for _, a := range fn.Architectures {
		f.Architectures = append(f.Architectures, string(a))
	}
	if fn.EphemeralStorage != nil {
		f.EphemeralMB = aws.ToInt32(fn.EphemeralStorage.Size)
	}
	if fn.TracingConfig != nil {
		f.TracingMode = string(fn.TracingConfig.Mode)
	}
	for _, l := range fn.Layers {
		f.Layers = append(f.Layers, aws.ToString(l.Arn))
	}
	if fn.VpcConfig != nil {
		f.VpcID = aws.ToString(fn.VpcConfig.VpcId)
		f.SubnetIDs = fn.VpcConfig.SubnetIds
		f.SecurityGroupIDs = fn.VpcConfig.SecurityGroupIds
	}
	if fn.DeadLetterConfig != nil {
		f.DLQTargetArn = aws.ToString(fn.DeadLetterConfig.TargetArn)
	}
	if fn.Environment != nil {
		f.EnvVarKeys = sortedMapKeys(fn.Environment.Variables)
	}
	f.LogGroup = "/aws/lambda/" + f.Name
	if fn.LoggingConfig != nil {
		if g := aws.ToString(fn.LoggingConfig.LogGroup); g != "" {
			f.LogGroup = g
		}
	}
	return f
}

func mapLayer(region string, l lambdatypes.LayersListItem) Layer {
	out := Layer{
		Name:   aws.ToString(l.LayerName),
		Region: region,
		ARN:    aws.ToString(l.LayerArn),
	}
	if v := l.LatestMatchingVersion; v != nil {
		out.LatestVersion = v.Version
		out.LatestVersionARN = aws.ToString(v.LayerVersionArn)
		out.Description = aws.ToString(v.Description)
		out.CreatedDate = aws.ToString(v.CreatedDate)
		out.License = aws.ToString(v.LicenseInfo)
		for _, r := range v.CompatibleRuntimes {
			out.Runtimes = append(out.Runtimes, string(r))
		}
		for _, a := range v.CompatibleArchitectures {
			out.Architectures = append(out.Architectures, string(a))
		}
	}
	return out
}

func mapEventSource(region string, m lambdatypes.EventSourceMappingConfiguration) EventSource {
	es := EventSource{
		UUID:                 aws.ToString(m.UUID),
		Region:               region,
		ARN:                  aws.ToString(m.EventSourceMappingArn),
		FunctionName:         functionNameFromARN(aws.ToString(m.FunctionArn)),
		SourceLabel:          eventSourceLabel(m),
		State:                aws.ToString(m.State),
		BatchSize:            aws.ToInt32(m.BatchSize),
		LastModified:         aws.ToTime(m.LastModified),
		LastProcessingResult: aws.ToString(m.LastProcessingResult),
	}
	return es
}

func (inv *Inventory) sort() {
	sort.Slice(inv.Functions, func(i, j int) bool {
		return less(inv.Functions[i].Name, inv.Functions[i].Region, inv.Functions[j].Name, inv.Functions[j].Region)
	})
	sort.Slice(inv.Layers, func(i, j int) bool {
		return less(inv.Layers[i].Name, inv.Layers[i].Region, inv.Layers[j].Name, inv.Layers[j].Region)
	})
	sort.Slice(inv.EventSources, func(i, j int) bool {
		return less(inv.EventSources[i].FunctionName, inv.EventSources[i].UUID, inv.EventSources[j].FunctionName, inv.EventSources[j].UUID)
	})
}

func less(ni, ri, nj, rj string) bool {
	if ni != nj {
		return ni < nj
	}
	return ri < rj
}
