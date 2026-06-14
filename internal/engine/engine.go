package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
	"github.com/ryandam9/aws_explorer/internal/services/acm"
	"github.com/ryandam9/aws_explorer/internal/services/apigateway"
	"github.com/ryandam9/aws_explorer/internal/services/athena"
	"github.com/ryandam9/aws_explorer/internal/services/cloudformation"
	"github.com/ryandam9/aws_explorer/internal/services/cloudfront"
	"github.com/ryandam9/aws_explorer/internal/services/cloudwatch"
	"github.com/ryandam9/aws_explorer/internal/services/dynamodb"
	"github.com/ryandam9/aws_explorer/internal/services/ec2"
	"github.com/ryandam9/aws_explorer/internal/services/ecr"
	"github.com/ryandam9/aws_explorer/internal/services/ecs"
	"github.com/ryandam9/aws_explorer/internal/services/efs"
	"github.com/ryandam9/aws_explorer/internal/services/eks"
	"github.com/ryandam9/aws_explorer/internal/services/elasticache"
	"github.com/ryandam9/aws_explorer/internal/services/elbv2"
	"github.com/ryandam9/aws_explorer/internal/services/emr"
	"github.com/ryandam9/aws_explorer/internal/services/eventbridge"
	"github.com/ryandam9/aws_explorer/internal/services/glue"
	"github.com/ryandam9/aws_explorer/internal/services/iam"
	"github.com/ryandam9/aws_explorer/internal/services/kinesis"
	"github.com/ryandam9/aws_explorer/internal/services/kms"
	"github.com/ryandam9/aws_explorer/internal/services/lambda"
	"github.com/ryandam9/aws_explorer/internal/services/rds"
	"github.com/ryandam9/aws_explorer/internal/services/redshift"
	"github.com/ryandam9/aws_explorer/internal/services/route53"
	"github.com/ryandam9/aws_explorer/internal/services/s3"
	"github.com/ryandam9/aws_explorer/internal/services/secretsmanager"
	"github.com/ryandam9/aws_explorer/internal/services/sns"
	"github.com/ryandam9/aws_explorer/internal/services/sqs"
	"github.com/ryandam9/aws_explorer/internal/services/stepfunctions"
)

// Engine is responsible for orchestrating the scans.
type Engine struct {
	Config          *config.Config
	AWSConfig       aws.Config
	registry        *services.Registry
	ResolvedRegions []string
	AccountID       string

	// Per-account credentials resolved by the latest sweep, for AWSConfigFor.
	sweepMu   sync.RWMutex
	sweepCfgs map[string]aws.Config
}

// NewEngine creates a new scanning engine.
// defaultRegistry builds the registry of typed collectors used by every engine.
// It is split out from NewEngine (which also does live AWS auth) so the set of
// registered collectors can be inspected in tests without credentials — this is
// what guarantees each service surfaces in the Summary screen with complete,
// tag-independent coverage.
func defaultRegistry() *services.Registry {
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
	registry.Register(cloudfront.NewCollector())
	registry.Register(route53.NewCollector())
	registry.Register(apigateway.NewCollector())
	registry.Register(stepfunctions.NewCollector())
	registry.Register(eventbridge.NewCollector())
	registry.Register(elasticache.NewCollector())
	registry.Register(efs.NewCollector())
	registry.Register(kinesis.NewCollector())
	registry.Register(redshift.NewCollector())
	registry.Register(kms.NewCollector())
	registry.Register(ecr.NewCollector())
	registry.Register(acm.NewCollector())
	registry.Register(cloudformation.NewCollector())
	registry.Register(glue.NewCollector())
	registry.Register(athena.NewCollector())
	return registry
}

func NewEngine(ctx context.Context, cfg *config.Config) (*Engine, error) {
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

	// Surface the region scope on the init log: it is emitted before the CLI
	// silences scan logs, so non-TUI commands (find, whereused, …) print the
	// region(s) they are about to scan — invaluable when a lookup returns
	// nothing because it ran against the wrong region (issue #149).
	slog.Info("Initializing AWS configuration",
		"authMethod", cfg.AWS.AuthMethod,
		"profile", cfg.AWS.Profile,
		"region", regionScopeLabel(regions, allRegions),
	)

	awscfg, err := auth.BuildAWSConfig(ctx, &cfg.AWS, bootstrapRegion)
	if err != nil {
		// An expired SSO session is the most common failure here; surface the
		// exact command that fixes it instead of the SDK's error chain.
		if hint, ok := awserr.LoginHint(err, cfg.AWS.Profile); ok {
			return nil, errors.New(hint)
		}
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

	registry := defaultRegistry()

	return &Engine{
		Config:          cfg,
		AWSConfig:       awscfg,
		registry:        registry,
		ResolvedRegions: resolvedRegions,
		AccountID:       accountID,
	}, nil
}

// regionScopeLabel renders the region scope for the init log: "all" when every
// region is in play, otherwise the comma-separated configured region list.
func regionScopeLabel(regions []string, allRegions bool) string {
	if allRegions {
		return "all"
	}
	return strings.Join(regions, ",")
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

// PlannedTaskKeys returns one "service@region" key per collection task that
// StreamRun will launch with the current configuration, in launch order. TUIs
// use it to show real scan progress (done/total and what is still pending).
func (e *Engine) PlannedTaskKeys() []string {
	regions := e.EffectiveRegions()
	var keys []string
	accounts := e.Config.Accounts
	if len(accounts) == 0 {
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
				keys = append(keys, srv.Name()+"@"+region)
			}
		}
		return keys
	}
	for _, acc := range accounts {
		name := accountName(acc)
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
				keys = append(keys, name+"/"+srv.Name()+"@"+region)
			}
		}
	}
	return keys
}

// EffectiveRegions returns the regions to scan: the resolved region list
// narrowed by any filters.regions configured. It always returns at least one
// TypedServices returns the names of the registered typed collectors — the
// services whose inventory is complete regardless of tags, as opposed to those
// reached only through the tag-based discovery sweep. Enabled-state is ignored;
// this reflects what the tool can collect with a dedicated collector.
func (e *Engine) TypedServices() []string {
	all := e.registry.GetAll()
	names := make([]string, 0, len(all))
	for _, c := range all {
		names = append(names, c.Name())
	}
	return names
}

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

type accountSweep struct {
	Name      string
	AWSConfig aws.Config
	AccountID string
}

// accountName returns the display/progress name for a configured account
// sweep. It must be deterministic so PlannedTaskKeys and StreamRun progress
// markers agree.
func accountName(acc config.AccountConfig) string {
	if acc.Name != "" {
		return acc.Name
	}
	if acc.Profile != "" {
		return acc.Profile
	}
	return "default"
}

// buildSweeps resolves credentials for every configured account in parallel
// (credential resolution involves STS round-trips) and caches the per-account
// configs for AWSConfigFor lookups.
func (e *Engine) buildSweeps(ctx context.Context) []accountSweep {
	if len(e.Config.Accounts) == 0 {
		return []accountSweep{{
			Name:      e.Config.AWS.Profile,
			AWSConfig: e.AWSConfig,
			AccountID: e.AccountID,
		}}
	}

	results := make([]*accountSweep, len(e.Config.Accounts))
	var wg sync.WaitGroup
	for i, acc := range e.Config.Accounts {
		wg.Add(1)
		go func(i int, acc config.AccountConfig) {
			defer wg.Done()
			name := accountName(acc)

			accCfg := e.AWSConfig.Copy()
			if acc.Profile != "" || acc.RoleARN != "" {
				// One spec covers both: buildSTS bootstraps its AssumeRole
				// call from the profile/auto chain, so profile+roleArn chains
				// the role through the account's profile credentials.
				spec := &config.AWSConfig{
					Profile:    acc.Profile,
					AuthMethod: "profile",
					Retry:      e.Config.AWS.Retry,
				}
				if acc.RoleARN != "" {
					spec.AuthMethod = "sts"
					spec.STS = config.STSConfig{
						RoleARN:     acc.RoleARN,
						SessionName: "aws-explorer-sweep",
					}
				}
				var err error
				accCfg, err = auth.BuildAWSConfig(ctx, spec, "us-east-1")
				if err != nil {
					slog.Error("Failed to build AWS config for account", "name", name, "error", err)
					return
				}
			}
			results[i] = &accountSweep{
				Name:      name,
				AWSConfig: accCfg,
				AccountID: resolveAccountID(ctx, accCfg),
			}
		}(i, acc)
	}
	wg.Wait()

	var sweeps []accountSweep
	e.sweepMu.Lock()
	e.sweepCfgs = make(map[string]aws.Config, len(results))
	for _, sw := range results {
		if sw == nil {
			continue // credential failure already logged
		}
		sweeps = append(sweeps, *sw)
		e.sweepCfgs[sw.Name] = sw.AWSConfig
	}
	e.sweepMu.Unlock()
	return sweeps
}

// AWSConfigFor returns the credentials for the named account sweep, falling
// back to the engine's base config (single-account mode, unknown name).
func (e *Engine) AWSConfigFor(account string) aws.Config {
	e.sweepMu.RLock()
	defer e.sweepMu.RUnlock()
	if cfg, ok := e.sweepCfgs[account]; ok {
		return cfg
	}
	return e.AWSConfig
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
	sweeps := e.buildSweeps(ctx)

	for _, sweep := range sweeps {
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
				sw := sweep

				g.Go(func() error {
					slog.Debug("Starting collector", "service", s.Name(), "region", r, "account", sw.Name)

					// app.timeoutSeconds bounds each collector run.
					collectCtx := gCtx
					if e.Config.App.TimeoutSeconds > 0 {
						var cancel context.CancelFunc
						collectCtx, cancel = context.WithTimeout(gCtx, time.Duration(e.Config.App.TimeoutSeconds)*time.Second)
						defer cancel()
					}

					regionalConfig := sw.AWSConfig
					if r != "global" {
						regionalConfig.Region = r
					}

					// send delivers a chunk unless the whole run is being torn
					// down (consumer gone), in which case it drops the chunk
					// instead of blocking the worker forever.
					send := func(chunk model.ResultChunk) {
						select {
						case chunks <- chunk:
						case <-gCtx.Done():
						}
					}

					input := services.CollectInput{
						Config:    e.Config,
						AWSConfig: regionalConfig,
						Region:    r,
						AccountID: sw.AccountID,
						Filters: model.Filter{
							Regions: e.Config.Filters.Regions,
							States:  e.Config.Filters.States,
							Tags:    e.Config.Filters.Tags,
						},
						DetailLevel: services.DetailLevelSummary,
					}

					// Stream page-sized batches as the collector gathers them so
					// results surface after the first page round-trip, not after
					// the last. emitted counts streamed resources (post-filter)
					// so the error path below can still flag partial results;
					// collectors call Emit from this goroutine, so no locking.
					// Stamp every resource with its owning account: the sweep
					// name in multi-account mode (so the Account column shows
					// names, not a mix of names and IDs) and the resolved
					// account ID otherwise. Collectors set AccountID
					// inconsistently (only EC2 does), so stamping here
					// guarantees it for every service, and lets AWSConfigFor
					// lookups resolve from a resource's AccountID.
					multiAcct := len(e.Config.Accounts) > 0
					acct := sw.AccountID
					if multiAcct {
						acct = sw.Name
					}
					emitted := 0
					input.Emit = func(batch []model.Resource) {
						if acct != "" {
							for i := range batch {
								batch[i].AccountID = acct
							}
						}
						filtered := filterResources(batch, input.Filters)
						emitted += len(filtered)
						if len(filtered) > 0 {
							send(model.ResultChunk{Resources: filtered})
						}
					}

					res, err := s.Collect(collectCtx, input)
					if acct != "" {
						for i := range res {
							res[i].AccountID = acct
						}
					}
					filteredRes := filterResources(res, input.Filters)
					if err != nil {
						// Collectors are best-effort: res may hold resources
						// gathered before the failure (and more may have been
						// streamed already). Keep them and flag the error as
						// partial so consumers can say so.
						partial := len(filteredRes) > 0 || emitted > 0
						code := "CollectionError"
						msg := err.Error()
						switch {
						case awserr.IsExpiredCreds(err):
							code = "ExpiredCredentials"
							msg, _ = awserr.LoginHint(err, e.Config.AWS.Profile)
							slog.Warn("Credentials expired",
								"service", s.Name(), "region", r, "keptResources", len(filteredRes)+emitted)
						case awserr.IsAuthError(err):
							code = "AccessDenied"
							msg = awserr.FriendlyMessage(err, s.Name())
							slog.Warn("Access denied",
								"service", s.Name(), "region", r, "keptResources", len(filteredRes)+emitted)
						case errors.Is(err, context.DeadlineExceeded):
							code = "Timeout"
							msg = fmt.Sprintf("collection timed out after %ds (app.timeoutSeconds); results are incomplete",
								e.Config.App.TimeoutSeconds)
							slog.Warn("Collection timed out",
								"service", s.Name(), "region", r, "keptResources", len(filteredRes)+emitted)
						default:
							slog.Warn("Collection error",
								"service", s.Name(), "region", r, "keptResources", len(filteredRes)+emitted,
								"error", err.Error())
						}
						progressSvc := s.Name()
						if len(e.Config.Accounts) > 0 {
							progressSvc = sw.Name + "/" + s.Name()
						}
						send(model.ResultChunk{
							Resources: filteredRes,
							Errors: []model.ExploreError{{
								Service: s.Name(),
								Region:  r,
								Code:    code,
								Message: msg,
								Partial: partial,
							}},
							Progress: &model.TaskProgress{Service: progressSvc, Region: r},
						})
						return nil
					}

					// Always send the final chunk — even with zero resources —
					// so the Progress marker reaches consumers for every task.
					progressSvc := s.Name()
					if len(e.Config.Accounts) > 0 {
						progressSvc = sw.Name + "/" + s.Name()
					}
					send(model.ResultChunk{
						Resources: filteredRes,
						Progress:  &model.TaskProgress{Service: progressSvc, Region: r},
					})
					return nil
				})
			}
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
