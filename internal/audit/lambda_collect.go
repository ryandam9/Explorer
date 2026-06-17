package audit

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const maxLambdaFunctions = 500

// collectLambdaRegion gathers the Lambda health/EOL snapshot for one region.
// Same best-effort contract as the other collectors: a denied call degrades the
// affected checks (recorded as a collection error) and never aborts the audit.
// Everything the checks need comes from ListFunctions, so it is a single
// paginated call with no per-function fan-out.
func collectLambdaRegion(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.LambdaSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.LambdaSnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	ctx, cancel := withTimeout(ctx, perCallTimeout)
	defer cancel()
	client := awslambda.NewFromConfig(cfg)

	paginator := awslambda.NewListFunctionsPaginator(client, &awslambda.ListFunctionsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			rec.record("lambda", err)
			break
		}
		for _, fn := range page.Functions {
			state := string(fn.State)
			snap.Functions = append(snap.Functions, findings.LambdaFunction{
				Name:             aws.ToString(fn.FunctionName),
				ARN:              aws.ToString(fn.FunctionArn),
				Runtime:          string(fn.Runtime),
				PackageType:      string(fn.PackageType),
				HasDLQ:           fn.DeadLetterConfig != nil && aws.ToString(fn.DeadLetterConfig.TargetArn) != "",
				StateKnown:       state != "",
				State:            state,
				LastUpdateStatus: string(fn.LastUpdateStatus),
			})
			if len(snap.Functions) >= maxLambdaFunctions {
				rec.recordTruncation("lambda", "functions", maxLambdaFunctions)
				break
			}
		}
		if len(snap.Functions) >= maxLambdaFunctions {
			break
		}
	}

	return snap, rec.errs
}
