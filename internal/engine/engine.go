package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/sync/errgroup"

	"github.com/user/aws_explorer/internal/auth"
	"github.com/user/aws_explorer/internal/awserr"
	"github.com/user/aws_explorer/internal/awsutil"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
	"github.com/user/aws_explorer/internal/services/cloudwatch"
	"github.com/user/aws_explorer/internal/services/cloudwatchlogs"
	"github.com/user/aws_explorer/internal/services/dynamodb"
	"github.com/user/aws_explorer/internal/services/ec2"
	"github.com/user/aws_explorer/internal/services/ecs"
	"github.com/user/aws_explorer/internal/services/eks"
	"github.com/user/aws_explorer/internal/services/elbv2"
	"github.com/user/aws_explorer/internal/services/emr"
	"github.com/user/aws_explorer/internal/services/iam"
	"github.com/user/aws_explorer/internal/services/lambda"
	"github.com/user/aws_explorer/internal/services/rds"
	"github.com/user/aws_explorer/internal/services/route53"
	"github.com/user/aws_explorer/internal/services/s3"
	"github.com/user/aws_explorer/internal/services/secretsmanager"
	"github.com/user/aws_explorer/internal/services/sns"
	"github.com/user/aws_explorer/internal/services/sqs"
)

// Engine is responsible for orchestrating the scans.
type Engine struct {
	Config          *config.Config
	AWSConfig       aws.Config
	registry        *services.Registry
	ResolvedRegions []string
	AccountID       string
}

// NewEngine creates a new scanning engine.
func NewEngine(ctx context.Context, cfg *config.Config) (*Engine, error) {
	slog.Info("Initializing AWS configuration",
		"authMethod", cfg.AWS.AuthMethod,
		"profile", cfg.AWS.Profile,
	)

	// Resolve regions from config or default
	regions := cfg.AWS.Regions
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	// Check if "all" is in the regions list
	allRegions := cfg.AWS.AllRegions
	bootstrapRegion := regions[0]
	for _, r := range regions {
		if strings.ToLower(r) == "all" {
			allRegions = true
			bootstrapRegion = "us-east-1"
			break
		}
	}

	awscfg, err := auth.BuildAWSConfig(ctx, &cfg.AWS, bootstrapRegion)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}

	// Resolve all regions if requested
	resolvedRegions := regions
	if allRegions {
		resolvedRegions = resolveAllRegions(ctx, awscfg)
		slog.Info("Resolved all AWS regions", "count", len(resolvedRegions))
	}

	// Resolve the caller's account ID once so collectors can construct ARNs for
	// resources AWS doesn't return ARNs for (EC2, S3, SQS, …). Best-effort: if
	// the lookup fails (e.g. missing sts:GetCallerIdentity), constructed ARNs
	// are simply omitted while AWS-provided ARNs still work.
	accountID := resolveAccountID(ctx, awscfg)

	registry := services.NewRegistry()
	registry.Register(ec2.NewCollector())
	registry.Register(s3.NewCollector())
	registry.Register(rds.NewCollector())
	registry.Register(iam.NewCollector())
	registry.Register(dynamodb.NewCollector())
	registry.Register(lambda.NewCollector())
	registry.Register(emr.NewCollector())
	registry.Register(ecs.NewCollector())
	registry.Register(eks.NewCollector())
	registry.Register(elbv2.NewCollector())
	registry.Register(secretsmanager.NewCollector())
	registry.Register(sqs.NewCollector())
	registry.Register(sns.NewCollector())
	registry.Register(cloudwatch.NewCollector())
	registry.Register(cloudwatchlogs.NewCollector())
	registry.Register(route53.NewCollector())

	return &Engine{
		Config:          cfg,
		AWSConfig:       awscfg,
		registry:        registry,
		ResolvedRegions: resolvedRegions,
		AccountID:       accountID,
	}, nil
}

// resolveAccountID returns the AWS account ID for the active credentials, or ""
// if it cannot be determined. Errors are logged but not fatal — the account ID
// is only used to enrich constructed ARNs.
func resolveAccountID(ctx context.Context, awscfg aws.Config) string {
	out, err := sts.NewFromConfig(awscfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		slog.Warn("Unable to resolve account ID; constructed ARNs will be omitted", "error", err.Error())
		return ""
	}
	return aws.ToString(out.Account)
}

// resolveAllRegions returns every region to scan for "--all-regions". It calls
// ec2:DescribeRegions, but that API is itself permission-gated: when the caller
// lacks ec2:DescribeRegions (or the call otherwise fails) we fall back to the
// canonical static region list with a warning rather than aborting the whole
// run, so the scan still proceeds best-effort.
func resolveAllRegions(ctx context.Context, awscfg aws.Config) []string {
	client := awsec2.NewFromConfig(awscfg)
	result, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		if awserr.IsAuthError(err) {
			slog.Warn("Not authorized to call ec2:DescribeRegions; "+
				"falling back to the built-in region list",
				"regions", len(awsutil.FallbackRegions))
		} else {
			slog.Warn("Unable to list AWS regions; falling back to the built-in region list",
				"error", err.Error(), "regions", len(awsutil.FallbackRegions))
		}
		return awsutil.FallbackRegions
	}
	regions := make([]string, 0, len(result.Regions))
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		return awsutil.FallbackRegions
	}
	return regions
}

// Run executes all configured scanners concurrently and returns the combined results.
func (e *Engine) Run(ctx context.Context) (model.ExploreResult, error) {
	chunks := make(chan model.ResultChunk, 64)
	go e.StreamRun(ctx, chunks)

	var result model.ExploreResult
	for chunk := range chunks {
		result.Resources = append(result.Resources, chunk.Resources...)
		result.Errors = append(result.Errors, chunk.Errors...)
	}
	return result, nil
}

// EffectiveRegions returns the regions to scan: the resolved region list
// narrowed by any filters.regions configured. It always returns at least one
// region.
func (e *Engine) EffectiveRegions() []string {
	regions := e.ResolvedRegions
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	if len(e.Config.Filters.Regions) > 0 {
		filterSet := make(map[string]struct{}, len(e.Config.Filters.Regions))
		for _, r := range e.Config.Filters.Regions {
			filterSet[r] = struct{}{}
		}
		var filtered []string
		for _, r := range regions {
			if _, ok := filterSet[r]; ok {
				filtered = append(filtered, r)
			}
		}
		regions = filtered
	}
	return regions
}

// StreamRun emits results to the channel as they arrive, then closes it.
func (e *Engine) StreamRun(ctx context.Context, chunks chan<- model.ResultChunk) {
	defer close(chunks)

	g, gCtx := errgroup.WithContext(ctx)
	maxConcurrency := e.Config.App.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	g.SetLimit(maxConcurrency)

	regions := e.EffectiveRegions()

	for _, srv := range e.registry.GetAll() {
		srvCfg, ok := e.Config.Services[srv.Name()]
		if !ok || !srvCfg.Enabled {
			continue
		}

		serviceRegions := regions
		if srv.IsGlobal() {
			serviceRegions = []string{"global"}
		}

		for _, region := range serviceRegions {
			s := srv
			r := region

			g.Go(func() error {
				slog.Debug("Starting collector", "service", s.Name(), "region", r)

				// app.timeoutSeconds bounds each collector run.
				collectCtx := gCtx
				if e.Config.App.TimeoutSeconds > 0 {
					var cancel context.CancelFunc
					collectCtx, cancel = context.WithTimeout(gCtx, time.Duration(e.Config.App.TimeoutSeconds)*time.Second)
					defer cancel()
				}

				regionalConfig := e.AWSConfig
				if r != "global" {
					regionalConfig.Region = r
				}

				input := services.CollectInput{
					Config:    e.Config,
					AWSConfig: regionalConfig,
					Region:    r,
					AccountID: e.AccountID,
					Filters: model.Filter{
						Regions: e.Config.Filters.Regions,
						States:  e.Config.Filters.States,
						Tags:    e.Config.Filters.Tags,
					},
					DetailLevel: services.DetailLevelSummary,
				}

				res, err := s.Collect(collectCtx, input)
				filteredRes := filterResources(res, input.Filters)
				if err != nil {
					// Collectors are best-effort: res may hold resources
					// gathered before the failure. Keep them and flag the
					// error as partial so consumers can say so.
					partial := len(filteredRes) > 0
					code := "CollectionError"
					msg := err.Error()
					if awserr.IsAuthError(err) {
						code = "AccessDenied"
						msg = awserr.FriendlyMessage(err, s.Name())
						slog.Warn("Access denied",
							"service", s.Name(), "region", r, "keptResources", len(filteredRes))
					} else {
						slog.Warn("Collection error",
							"service", s.Name(), "region", r, "keptResources", len(filteredRes),
							"error", err.Error())
					}
					chunks <- model.ResultChunk{
						Resources: filteredRes,
						Errors: []model.ExploreError{{
							Service: s.Name(),
							Region:  r,
							Code:    code,
							Message: msg,
							Partial: partial,
						}},
					}
					return nil
				}

				if len(filteredRes) > 0 {
					chunks <- model.ResultChunk{Resources: filteredRes}
				}
				return nil
			})
		}
	}

	g.Wait()
}

func filterResources(resources []model.Resource, filters model.Filter) []model.Resource {
	if len(filters.States) == 0 && len(filters.Tags) == 0 {
		return resources
	}
	filtered := make([]model.Resource, 0, len(resources))
	for _, r := range resources {
		// Filter by state
		if len(filters.States) > 0 {
			matchState := false
			for _, s := range filters.States {
				if strings.EqualFold(r.State, s) {
					matchState = true
					break
				}
			}
			if !matchState {
				continue
			}
		}

		// Filter by tag
		if len(filters.Tags) > 0 {
			matchTag := true
			for tk, tv := range filters.Tags {
				if val, ok := r.Tags[tk]; !ok || val != tv {
					matchTag = false
					break
				}
			}
			if !matchTag {
				continue
			}
		}

		filtered = append(filtered, r)
	}
	return filtered
}
