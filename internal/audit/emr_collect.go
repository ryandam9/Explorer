package audit

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsemr "github.com/aws/aws-sdk-go-v2/service/emr"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxEMRClusters    = 200
	emrEnrichParallel = 8
)

// collectEMRRegion gathers the EMR health/cost snapshot for one region. Same
// best-effort contract as the other collectors: a denied call degrades the
// affected checks (recorded as a collection error) and never aborts the audit.
func collectEMRRegion(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.EMRSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.EMRSnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	listCtx, cancel := withTimeout(ctx, perCallTimeout)
	defer cancel()
	client := awsemr.NewFromConfig(cfg)

	pag := awsemr.NewListClustersPaginator(client, &awsemr.ListClustersInput{})
	truncated := false
	for pag.HasMorePages() && !truncated {
		page, err := pag.NextPage(listCtx)
		if err != nil {
			rec.record("emr", err)
			break
		}
		for _, cs := range page.Clusters {
			ec := findings.EMRCluster{ID: aws.ToString(cs.Id), Name: aws.ToString(cs.Name)}
			if cs.Status != nil {
				ec.State = string(cs.Status.State)
			}
			ec.ARN = aws.ToString(cs.ClusterArn)
			snap.Clusters = append(snap.Clusters, ec)
			if len(snap.Clusters) >= maxEMRClusters {
				rec.recordTruncation("emr", "clusters", maxEMRClusters)
				truncated = true
				break
			}
		}
	}

	// Enrich each cluster: DescribeCluster for posture, and the latest step
	// state for live clusters. A per-cluster failure leaves that fact unknown
	// (StepsKnown=false) so the dependent check stays silent.
	var g errgroup.Group
	g.SetLimit(emrEnrichParallel)
	var mu sync.Mutex
	for i := range snap.Clusters {
		i := i
		g.Go(func() error {
			enrichEMRCluster(ctx, client, &snap.Clusters[i], rec, &mu, perCallTimeout)
			return nil
		})
	}
	_ = g.Wait()

	return snap, rec.errs
}

func enrichEMRCluster(ctx context.Context, client *awsemr.Client, ec *findings.EMRCluster, rec *errRecorder, mu *sync.Mutex, timeout time.Duration) {
	dctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	out, err := client.DescribeCluster(dctx, &awsemr.DescribeClusterInput{ClusterId: aws.String(ec.ID)})
	if err != nil {
		mu.Lock()
		rec.record("emr", err)
		mu.Unlock()
		return
	}
	if cl := out.Cluster; cl != nil {
		ec.AutoTerminate = aws.ToBool(cl.AutoTerminate)
		ec.HasLogURI = aws.ToString(cl.LogUri) != ""
		ec.HasSecurityConfig = aws.ToString(cl.SecurityConfiguration) != ""
		if cl.Status != nil && cl.Status.Timeline != nil && cl.Status.Timeline.CreationDateTime != nil {
			ec.Created = *cl.Status.Timeline.CreationDateTime
		}
	}

	// Latest step state for live clusters (EMR returns steps newest-first).
	if isLiveEMRState(ec.State) {
		sctx, scancel := withTimeout(ctx, timeout)
		defer scancel()
		steps, serr := client.ListSteps(sctx, &awsemr.ListStepsInput{ClusterId: aws.String(ec.ID)})
		if serr != nil {
			mu.Lock()
			rec.record("emr", serr)
			mu.Unlock()
			return
		}
		ec.StepsKnown = true
		if len(steps.Steps) > 0 && steps.Steps[0].Status != nil {
			ec.LatestStepState = string(steps.Steps[0].Status.State)
		}
	}
}

func isLiveEMRState(state string) bool {
	switch state {
	case "TERMINATED", "TERMINATED_WITH_ERRORS", "TERMINATING", "":
		return false
	default:
		return true
	}
}
