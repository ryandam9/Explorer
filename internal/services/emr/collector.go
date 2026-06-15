// Package emr collects Amazon EMR clusters and, beyond bare inventory, stamps
// each cluster with the operational facts engineers ask for first — its release
// label, the applications installed on it (Spark, HBase, Hive, Oozie…), whether
// it auto-terminates, and why it stopped — so the summary TUI and the dedicated
// EMR dashboard can show health at a glance (AXE-033).
//
// At detailed scope it additionally lists each non-terminated cluster's steps as
// their own resources, carrying the step state and (on failure) the reason and
// log location.
package emr

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// describeConcurrency bounds parallel DescribeCluster / ListSteps calls so
// accounts with many clusters don't serialize on per-cluster enrichment or trip
// EMR's request throttling.
const describeConcurrency = 8

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "emr"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := emr.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := emr.NewListClustersPaginator(client, &emr.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list EMR clusters: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Clusters))
		for _, cluster := range page.Clusters {
			batch = append(batch, c.mapCluster(input.Region, cluster, input.DetailLevel))
		}

		// Beyond a bare listing, DescribeCluster fills in the release label,
		// installed applications and stop reason. It is best-effort: a denied or
		// failed describe leaves the cluster listed with only its summary fields.
		if input.DetailLevel != services.DetailLevelMinimal {
			c.enrichClusters(ctx, client, batch, input.DetailLevel)
		}

		resources = input.EmitOrAppend(resources, batch)

		// At detailed scope, list each live cluster's steps as their own
		// resources so step outcomes show up in find / summary / JSON output.
		if input.DetailLevel == services.DetailLevelDetailed || input.DetailLevel == services.DetailLevelRaw {
			steps := c.collectSteps(ctx, client, input.Region, batch)
			resources = input.EmitOrAppend(resources, steps)
		}
	}

	return resources, nil
}

// mapCluster maps a ClusterSummary (from ListClusters) to a resource. The richer
// fields are layered on later by enrichClusters via DescribeCluster.
func (c *Collector) mapCluster(region string, cluster types.ClusterSummary, detail services.DetailLevel) model.Resource {
	id := aws.ToString(cluster.Id)
	name := aws.ToString(cluster.Name)
	state := ""
	if cluster.Status != nil {
		state = string(cluster.Status.State)
	}

	res := model.Resource{
		Service: "emr",
		Type:    "cluster",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     aws.ToString(cluster.ClusterArn),
		State:   state,
		Summary: map[string]string{
			"normalizedInstanceHours": fmt.Sprintf("%d", aws.ToInt32(cluster.NormalizedInstanceHours)),
		},
	}

	if cluster.Status != nil && cluster.Status.Timeline != nil && cluster.Status.Timeline.CreationDateTime != nil {
		res.CreatedAt = cluster.Status.Timeline.CreationDateTime
	}

	return res
}

// enrichClusters fills each cluster's release/apps/stop-reason Summary fields
// from DescribeCluster, fetched with bounded concurrency. Each goroutine writes
// its own slice index, so no locking is needed; errors are swallowed because the
// enrichment is additive, not a reason to drop the cluster.
func (c *Collector) enrichClusters(ctx context.Context, client *emr.Client, batch []model.Resource, detail services.DetailLevel) {
	var g errgroup.Group
	g.SetLimit(describeConcurrency)
	for i := range batch {
		if batch[i].ID == "" {
			continue
		}
		g.Go(func() error {
			out, err := client.DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: aws.String(batch[i].ID)})
			if err != nil || out.Cluster == nil {
				return nil
			}
			applyClusterDetail(&batch[i], out.Cluster, detail)
			return nil
		})
	}
	_ = g.Wait()
}

// applyClusterDetail stamps the operational facts from a DescribeCluster result
// onto the cluster resource. Pure over its inputs, so it is fixture-tested.
func applyClusterDetail(res *model.Resource, cl *types.Cluster, detail services.DetailLevel) {
	if res.Summary == nil {
		res.Summary = map[string]string{}
	}
	if rl := aws.ToString(cl.ReleaseLabel); rl != "" {
		res.Summary["releaseLabel"] = rl
	}
	if apps := applicationNames(cl.Applications); apps != "" {
		res.Summary["applications"] = apps
	}
	res.Summary["autoTerminate"] = fmt.Sprintf("%t", aws.ToBool(cl.AutoTerminate))
	if dns := aws.ToString(cl.MasterPublicDnsName); dns != "" {
		res.Summary["masterDNS"] = dns
	}
	if cl.Status != nil && cl.Status.StateChangeReason != nil {
		if msg := aws.ToString(cl.Status.StateChangeReason.Message); msg != "" {
			res.Summary["stateChangeReason"] = msg
		}
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		details := map[string]any{
			"releaseLabel":       aws.ToString(cl.ReleaseLabel),
			"applications":       applicationList(cl.Applications),
			"autoTerminate":      aws.ToBool(cl.AutoTerminate),
			"terminationProtect": aws.ToBool(cl.TerminationProtected),
			"logUri":             aws.ToString(cl.LogUri),
			"serviceRole":        aws.ToString(cl.ServiceRole),
			"securityConfig":     aws.ToString(cl.SecurityConfiguration),
			"scaleDownBehavior":  string(cl.ScaleDownBehavior),
		}
		if cl.Ec2InstanceAttributes != nil {
			ec2 := cl.Ec2InstanceAttributes
			details["subnetId"] = aws.ToString(ec2.Ec2SubnetId)
			details["availabilityZone"] = aws.ToString(ec2.Ec2AvailabilityZone)
			details["keyName"] = aws.ToString(ec2.Ec2KeyName)
			details["instanceProfile"] = aws.ToString(ec2.IamInstanceProfile)
		}
		res.Details = details
	}
}

// collectSteps lists steps for each non-terminated cluster in the batch and maps
// them to step resources. Best-effort: a denied ListSteps on one cluster is
// skipped, the rest proceed.
func (c *Collector) collectSteps(ctx context.Context, client *emr.Client, region string, clusters []model.Resource) []model.Resource {
	var g errgroup.Group
	out := make([][]model.Resource, len(clusters))
	g.SetLimit(describeConcurrency)
	for i := range clusters {
		if clusters[i].ID == "" || isTerminated(clusters[i].State) {
			continue
		}
		g.Go(func() error {
			var steps []model.Resource
			pag := emr.NewListStepsPaginator(client, &emr.ListStepsInput{ClusterId: aws.String(clusters[i].ID)})
			for pag.HasMorePages() {
				page, err := pag.NextPage(ctx)
				if err != nil {
					break
				}
				for _, s := range page.Steps {
					steps = append(steps, mapStep(region, clusters[i].ID, s))
				}
			}
			out[i] = steps
			return nil
		})
	}
	_ = g.Wait()

	var flat []model.Resource
	for _, s := range out {
		flat = append(flat, s...)
	}
	return flat
}

// mapStep maps a cluster step to a resource, carrying its state and (on failure)
// the reason and log location. Pure, so it is fixture-tested.
func mapStep(region, clusterID string, s types.StepSummary) model.Resource {
	res := model.Resource{
		Service: "emr",
		Type:    "step",
		Region:  region,
		ID:      aws.ToString(s.Id),
		Name:    aws.ToString(s.Name),
		Summary: map[string]string{
			"cluster":         clusterID,
			"actionOnFailure": string(s.ActionOnFailure),
		},
	}
	if s.Status != nil {
		res.State = string(s.Status.State)
		if s.Status.Timeline != nil {
			res.CreatedAt = s.Status.Timeline.CreationDateTime
		}
		if s.Status.FailureDetails != nil {
			if reason := aws.ToString(s.Status.FailureDetails.Reason); reason != "" {
				res.Summary["failureReason"] = reason
			}
			if logFile := aws.ToString(s.Status.FailureDetails.LogFile); logFile != "" {
				res.Summary["failureLog"] = logFile
			}
		}
	}
	return res
}

// applicationNames renders the installed applications as "Spark, HBase, Hive" for
// the at-a-glance Summary, dropping versions for brevity.
func applicationNames(apps []types.Application) string {
	if len(apps) == 0 {
		return ""
	}
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		if n := aws.ToString(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

// applicationList renders the installed applications with versions for the
// detail blob, e.g. ["Spark 3.5.0", "HBase 2.4.17"].
func applicationList(apps []types.Application) []string {
	if len(apps) == 0 {
		return nil
	}
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		name := aws.ToString(a.Name)
		if name == "" {
			continue
		}
		if v := aws.ToString(a.Version); v != "" {
			out = append(out, name+" "+v)
		} else {
			out = append(out, name)
		}
	}
	return out
}

// isTerminated reports whether a cluster state is a terminal one; terminated
// clusters have no live steps worth listing.
func isTerminated(state string) bool {
	switch strings.ToUpper(state) {
	case "TERMINATED", "TERMINATED_WITH_ERRORS":
		return true
	default:
		return false
	}
}
