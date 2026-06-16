// Package emrtui is the interactive Amazon EMR dashboard (AXE-034/AXE-035): a
// Bubble Tea TUI over the account's EMR clusters — each row showing release
// label, installed applications, size and state at a glance — with a per-cluster
// step-history drill-down (state, duration, action-on-failure and the failure
// reason inline) and an on-demand cluster-detail overlay.
package emrtui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// describeConcurrency bounds parallel DescribeCluster calls during inventory
// load so accounts with many clusters don't trip EMR's request throttling.
const describeConcurrency = 8

// activeClusterStates are the live (non-terminal) EMR cluster states. The
// dashboard lists only these by default so the often-large tail of recently
// terminated clusters doesn't dominate the view — or waste a DescribeCluster on
// each. The terminated states (TERMINATED, TERMINATED_WITH_ERRORS) are fetched
// only when the caller opts in (the dashboard's "t" toggle / the CLI's
// --all-states), keeping ListClusters cheap and the list focused.
var activeClusterStates = []emrtypes.ClusterState{
	emrtypes.ClusterStateStarting,
	emrtypes.ClusterStateBootstrapping,
	emrtypes.ClusterStateRunning,
	emrtypes.ClusterStateWaiting,
	emrtypes.ClusterStateTerminating,
}

// Cluster is the dashboard's flattened view of an EMR cluster — only the fields
// the table, detail panel and console link need.
type Cluster struct {
	ID            string
	Name          string
	Region        string
	ARN           string
	State         string
	ReleaseLabel  string
	Applications  string
	InstanceHours int32
	AutoTerminate bool
	MasterDNS     string
	StateReason   string
	Created       time.Time

	// DetailKnown is true once DescribeCluster enrichment succeeded for this
	// cluster. When false the detail-only fields below (and AutoTerminate,
	// MasterDNS, Applications, ReleaseLabel) were never populated — a denied or
	// throttled DescribeCluster — so a blank value means "unknown", not "none".
	DetailKnown bool

	// Detail-only fields, populated by enrichment (DescribeCluster).
	LogURI          string
	ServiceRole     string
	SecurityConfig  string
	SubnetID        string
	AvailabilityAZ  string
	KeyName         string
	InstanceProfile string
}

// Step is one cluster step, flattened for the step-history view.
type Step struct {
	ID              string
	Name            string
	State           string
	ActionOnFailure string
	Created         time.Time
	Started         time.Time
	Ended           time.Time
	FailureReason   string
	FailureLog      string
	Jar             string
	MainClass       string
	Args            []string
}

// Instance is one EC2 instance in a cluster, flattened for the instances twin.
type Instance struct {
	ID         string
	EC2ID      string
	Type       string
	Market     string // ON_DEMAND / SPOT
	State      string
	PrivateDNS string
	PublicDNS  string
	Group      string // instance group or fleet id
}

// AppInfo is one installed application and its version.
type AppInfo struct {
	Name    string
	Version string
}

// Inventory is the full set of EMR clusters gathered across regions.
type Inventory struct {
	Clusters []Cluster

	// EnrichFailures counts clusters that were listed but whose DescribeCluster
	// enrichment failed (denied/throttled), so the dashboard can warn that some
	// detail columns are unknown rather than silently blank.
	EnrichFailures int
}

// Client holds one EMR client per region.
type Client struct {
	clients map[string]*emr.Client
	regions []string
}

// NewClient builds per-region EMR clients. When allRegions is true the region
// list is discovered via ec2:DescribeRegions, falling back to the built-in list
// when that call is denied.
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

	clients := make(map[string]*emr.Client, len(regions))
	for _, r := range regions {
		rCfg := base.Copy()
		rCfg.Region = r
		clients[r] = emr.NewFromConfig(rCfg)
	}
	return &Client{clients: clients, regions: regions}, nil
}

// Regions returns the regions this client queries, sorted.
func (c *Client) Regions() []string { return c.regions }

func (c *Client) clientFor(region string) *emr.Client {
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

// LoadInventory fans the cluster listing out across every region in parallel.
// When includeTerminated is false only live clusters are listed (the dashboard
// default); when true the terminated tail is included too. Per-region failures
// are soft (opt-in regions commonly deny EMR); an error is returned only when
// every region fails completely.
func (c *Client) LoadInventory(ctx context.Context, includeTerminated bool) (Inventory, error) {
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
			clusters, enrichFails, err := c.loadRegion(ctx, region, includeTerminated)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("EMR inventory failed", "region", region, "error", err.Error())
				return
			}
			inv.Clusters = append(inv.Clusters, clusters...)
			inv.EnrichFailures += enrichFails
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return Inventory{}, firstErr
	}

	inv.sort()
	return inv, nil
}

// loadRegion lists clusters for one region and enriches each with DescribeCluster
// (release label, applications, stop reason). Used by the blocking LoadInventory
// (the CLI twins); the dashboard instead lists then enriches progressively via
// ListSkeleton + EnrichRegion.
func (c *Client) loadRegion(ctx context.Context, region string, includeTerminated bool) ([]Cluster, int, error) {
	clusters, err := c.listRegion(ctx, region, includeTerminated)
	if err != nil {
		return clusters, 0, err
	}
	fails := c.enrichClusters(ctx, region, clusters)
	return clusters, fails, nil
}

// listRegion lists a region's clusters (summary fields only — name, ID, state,
// created, normalized hours; no DescribeCluster detail). Unless includeTerminated
// is set, ListClusters is filtered to the live states so the terminated tail is
// neither listed nor enriched.
func (c *Client) listRegion(ctx context.Context, region string, includeTerminated bool) ([]Cluster, error) {
	cl := c.clientFor(region)
	var clusters []Cluster

	input := &emr.ListClustersInput{}
	if !includeTerminated {
		input.ClusterStates = activeClusterStates
	}
	pag := emr.NewListClustersPaginator(cl, input)
	for pag.HasMorePages() {
		page, err := pag.NextPage(ctx)
		if err != nil {
			return clusters, err
		}
		for _, summary := range page.Clusters {
			clusters = append(clusters, clusterFromSummary(region, summary))
		}
	}
	return clusters, nil
}

// enrichClusters fills in each cluster's DescribeCluster detail in place under
// bounded concurrency, returning the count that failed enrichment (denied or
// throttled). A failed cluster keeps its skeleton fields and DetailKnown=false,
// so a blank reads as "unknown", not "none".
func (c *Client) enrichClusters(ctx context.Context, region string, clusters []Cluster) int {
	cl := c.clientFor(region)
	var (
		wg          sync.WaitGroup
		enrichFails atomic.Int32
		sem         = make(chan struct{}, describeConcurrency)
	)
	for i := range clusters {
		if clusters[i].ID == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out, err := cl.DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: aws.String(clusters[i].ID)})
			if err != nil || out.Cluster == nil {
				if err != nil {
					slog.Debug("EMR DescribeCluster failed", "cluster", clusters[i].ID, "region", region, "error", err.Error())
				}
				enrichFails.Add(1)
				return
			}
			applyClusterDetail(&clusters[i], out.Cluster)
		}(i)
	}
	wg.Wait()
	return int(enrichFails.Load())
}

// ListSkeleton lists clusters across every region without enrichment — the fast
// first phase of the dashboard's progressive load. Per-region listing failures
// are soft; an error is returned only when every region fails.
func (c *Client) ListSkeleton(ctx context.Context, includeTerminated bool) (Inventory, error) {
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
			clusters, err := c.listRegion(ctx, region, includeTerminated)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("EMR cluster listing failed", "region", region, "error", err.Error())
				return
			}
			inv.Clusters = append(inv.Clusters, clusters...)
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return Inventory{}, firstErr
	}
	inv.sort()
	return inv, nil
}

// EnrichRegion enriches one region's clusters (the second, streaming phase of
// the dashboard load), returning the enriched copies and the failure count.
func (c *Client) EnrichRegion(ctx context.Context, region string, clusters []Cluster) ([]Cluster, int) {
	fails := c.enrichClusters(ctx, region, clusters)
	return clusters, fails
}

// Steps fetches a cluster's step history (newest first, capped to limit).
func (c *Client) Steps(ctx context.Context, region, clusterID string, limit int) ([]Step, error) {
	cl := c.clientFor(region)
	var steps []Step
	pag := emr.NewListStepsPaginator(cl, &emr.ListStepsInput{ClusterId: aws.String(clusterID)})
	for pag.HasMorePages() {
		page, err := pag.NextPage(ctx)
		if err != nil {
			return steps, err
		}
		for _, s := range page.Steps {
			steps = append(steps, stepFromSummary(s))
			if limit > 0 && len(steps) >= limit {
				return steps, nil
			}
		}
	}
	return steps, nil
}

// Instances fetches a cluster's EC2 instances (capped to limit).
func (c *Client) Instances(ctx context.Context, region, clusterID string, limit int) ([]Instance, error) {
	cl := c.clientFor(region)
	var out []Instance
	pag := emr.NewListInstancesPaginator(cl, &emr.ListInstancesInput{ClusterId: aws.String(clusterID)})
	for pag.HasMorePages() {
		page, err := pag.NextPage(ctx)
		if err != nil {
			return out, err
		}
		for _, in := range page.Instances {
			out = append(out, instanceFrom(in))
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// MasterDNS returns a cluster's primary-node DNS (for the on-cluster browsers).
func (c *Client) MasterDNS(ctx context.Context, region, clusterID string) (string, error) {
	out, err := c.clientFor(region).DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: aws.String(clusterID)})
	if err != nil {
		return "", err
	}
	if out.Cluster == nil {
		return "", fmt.Errorf("cluster %q not found", clusterID)
	}
	return aws.ToString(out.Cluster.MasterPublicDnsName), nil
}

// Apps fetches a cluster's installed applications and versions (one
// DescribeCluster call).
func (c *Client) Apps(ctx context.Context, region, clusterID string) ([]AppInfo, error) {
	out, err := c.clientFor(region).DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: aws.String(clusterID)})
	if err != nil {
		return nil, err
	}
	if out.Cluster == nil {
		return nil, fmt.Errorf("cluster %q not found", clusterID)
	}
	apps := make([]AppInfo, 0, len(out.Cluster.Applications))
	for _, a := range out.Cluster.Applications {
		name := aws.ToString(a.Name)
		if name == "" {
			continue
		}
		apps = append(apps, AppInfo{Name: name, Version: aws.ToString(a.Version)})
	}
	return apps, nil
}

func instanceFrom(in emrtypes.Instance) Instance {
	out := Instance{
		ID:         aws.ToString(in.Id),
		EC2ID:      aws.ToString(in.Ec2InstanceId),
		Type:       aws.ToString(in.InstanceType),
		Market:     string(in.Market),
		PrivateDNS: aws.ToString(in.PrivateDnsName),
		PublicDNS:  aws.ToString(in.PublicDnsName),
	}
	if g := aws.ToString(in.InstanceGroupId); g != "" {
		out.Group = g
	} else if f := aws.ToString(in.InstanceFleetId); f != "" {
		out.Group = f
	}
	if in.Status != nil {
		out.State = string(in.Status.State)
	}
	return out
}

// clusterFromSummary maps a ListClusters summary to the dashboard's Cluster.
func clusterFromSummary(region string, s emrtypes.ClusterSummary) Cluster {
	c := Cluster{
		ID:            aws.ToString(s.Id),
		Name:          aws.ToString(s.Name),
		Region:        region,
		ARN:           aws.ToString(s.ClusterArn),
		InstanceHours: aws.ToInt32(s.NormalizedInstanceHours),
	}
	if s.Status != nil {
		c.State = string(s.Status.State)
		if s.Status.Timeline != nil && s.Status.Timeline.CreationDateTime != nil {
			c.Created = *s.Status.Timeline.CreationDateTime
		}
	}
	return c
}

// applyClusterDetail layers a DescribeCluster result onto a cluster. Pure over
// its inputs, so it is fixture-tested.
func applyClusterDetail(c *Cluster, cl *emrtypes.Cluster) {
	c.DetailKnown = true
	c.ReleaseLabel = aws.ToString(cl.ReleaseLabel)
	c.Applications = applicationNames(cl.Applications)
	c.AutoTerminate = aws.ToBool(cl.AutoTerminate)
	c.MasterDNS = aws.ToString(cl.MasterPublicDnsName)
	c.LogURI = aws.ToString(cl.LogUri)
	c.ServiceRole = aws.ToString(cl.ServiceRole)
	c.SecurityConfig = aws.ToString(cl.SecurityConfiguration)
	if cl.Status != nil && cl.Status.StateChangeReason != nil {
		c.StateReason = aws.ToString(cl.Status.StateChangeReason.Message)
	}
	if cl.Ec2InstanceAttributes != nil {
		ec2 := cl.Ec2InstanceAttributes
		c.SubnetID = aws.ToString(ec2.Ec2SubnetId)
		c.AvailabilityAZ = aws.ToString(ec2.Ec2AvailabilityZone)
		c.KeyName = aws.ToString(ec2.Ec2KeyName)
		c.InstanceProfile = aws.ToString(ec2.IamInstanceProfile)
	}
}

// stepFromSummary maps a StepSummary to the dashboard's Step.
func stepFromSummary(s emrtypes.StepSummary) Step {
	step := Step{
		ID:              aws.ToString(s.Id),
		Name:            aws.ToString(s.Name),
		ActionOnFailure: string(s.ActionOnFailure),
	}
	if s.Config != nil {
		step.Jar = aws.ToString(s.Config.Jar)
		step.MainClass = aws.ToString(s.Config.MainClass)
		step.Args = s.Config.Args
	}
	if s.Status != nil {
		step.State = string(s.Status.State)
		if s.Status.Timeline != nil {
			if s.Status.Timeline.CreationDateTime != nil {
				step.Created = *s.Status.Timeline.CreationDateTime
			}
			if s.Status.Timeline.StartDateTime != nil {
				step.Started = *s.Status.Timeline.StartDateTime
			}
			if s.Status.Timeline.EndDateTime != nil {
				step.Ended = *s.Status.Timeline.EndDateTime
			}
		}
		if s.Status.FailureDetails != nil {
			step.FailureReason = aws.ToString(s.Status.FailureDetails.Reason)
			step.FailureLog = aws.ToString(s.Status.FailureDetails.LogFile)
		}
	}
	return step
}

func applicationNames(apps []emrtypes.Application) string {
	if len(apps) == 0 {
		return ""
	}
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		if n := aws.ToString(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return joinComma(names)
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func (inv *Inventory) sort() {
	sort.Slice(inv.Clusters, func(i, j int) bool {
		a, b := inv.Clusters[i], inv.Clusters[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Region < b.Region
	})
}
