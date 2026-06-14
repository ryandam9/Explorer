package audit

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// collectCloudTrailAccount gathers the account's CloudTrail configuration. It
// is account-global like the IAM sweep: trails are enumerated once from the
// bootstrap region (multi-region trails surface from any region as shadow
// entries), and findings are labeled "global". Same best-effort contract as
// the other collectors — a denied call degrades the affected checks instead of
// aborting the audit.
func collectCloudTrailAccount(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.CloudTrailSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.CloudTrailSnapshot{Now: time.Now().UTC()}
	rec := &errRecorder{region: "global"}

	collectTrails(ctx, cfg, &snap, rec, perCallTimeout)
	return snap, rec.errs
}

func collectTrails(ctx context.Context, cfg aws.Config, snap *findings.CloudTrailSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awscloudtrail.NewFromConfig(cfg)

	out, err := client.DescribeTrails(ctx, &awscloudtrail.DescribeTrailsInput{
		IncludeShadowTrails: aws.Bool(true),
	})
	if err != nil {
		rec.record("cloudtrail", err)
		return
	}
	snap.TrailsKnown = true

	// A multi-region trail appears once in this region's view (as a shadow of
	// its home-region trail); dedupe by ARN defensively in case the API repeats.
	seen := make(map[string]bool, len(out.TrailList))
	for _, t := range out.TrailList {
		arn := aws.ToString(t.TrailARN)
		if arn != "" {
			if seen[arn] {
				continue
			}
			seen[arn] = true
		}
		ct := findings.CTTrail{
			Name:                       aws.ToString(t.Name),
			HomeRegion:                 aws.ToString(t.HomeRegion),
			IsMultiRegion:              aws.ToBool(t.IsMultiRegionTrail),
			IsOrganizationTrail:        aws.ToBool(t.IsOrganizationTrail),
			IncludeGlobalServiceEvents: aws.ToBool(t.IncludeGlobalServiceEvents),
			LogFileValidationEnabled:   aws.ToBool(t.LogFileValidationEnabled),
			KMSKeyID:                   aws.ToString(t.KmsKeyId),
			CloudWatchLogsGroupARN:     aws.ToString(t.CloudWatchLogsLogGroupArn),
		}

		// GetTrailStatus / GetEventSelectors accept the ARN, which resolves a
		// shadow trail back to its home region; fall back to the name otherwise.
		ref := arn
		if ref == "" {
			ref = ct.Name
		}
		fetchTrailStatus(ctx, client, ref, &ct, rec)
		fetchEventSelectors(ctx, client, ref, &ct, rec)

		snap.Trails = append(snap.Trails, ct)
	}
}

func fetchTrailStatus(ctx context.Context, client *awscloudtrail.Client, ref string, ct *findings.CTTrail, rec *errRecorder) {
	st, err := client.GetTrailStatus(ctx, &awscloudtrail.GetTrailStatusInput{Name: aws.String(ref)})
	if err != nil {
		rec.record("cloudtrail", err)
		return
	}
	ct.StatusKnown = true
	ct.IsLogging = aws.ToBool(st.IsLogging)
}

func fetchEventSelectors(ctx context.Context, client *awscloudtrail.Client, ref string, ct *findings.CTTrail, rec *errRecorder) {
	es, err := client.GetEventSelectors(ctx, &awscloudtrail.GetEventSelectorsInput{TrailName: aws.String(ref)})
	if err != nil {
		rec.record("cloudtrail", err)
		return
	}
	ct.SelectorsKnown = true
	ct.LogsAllManagementEvents = selectorsLogAllManagement(es)
}

// selectorsLogAllManagement reports whether a trail's event selectors capture
// all management (read and write) events — via either the classic
// EventSelectors or the AdvancedEventSelectors form.
func selectorsLogAllManagement(out *awscloudtrail.GetEventSelectorsOutput) bool {
	for _, s := range out.EventSelectors {
		if aws.ToBool(s.IncludeManagementEvents) && s.ReadWriteType == cttypes.ReadWriteTypeAll {
			return true
		}
	}
	for _, s := range out.AdvancedEventSelectors {
		if advancedSelectorAllManagement(s) {
			return true
		}
	}
	return false
}

// advancedSelectorAllManagement reports whether one advanced selector matches
// the Management event category without restricting readOnly (which would
// narrow it to read-only or write-only events, leaving a gap).
func advancedSelectorAllManagement(s cttypes.AdvancedEventSelector) bool {
	isManagement, restrictsReadOnly := false, false
	for _, f := range s.FieldSelectors {
		switch aws.ToString(f.Field) {
		case "eventCategory":
			for _, v := range f.Equals {
				if v == "Management" {
					isManagement = true
				}
			}
		case "readOnly":
			restrictsReadOnly = true
		}
	}
	return isManagement && !restrictsReadOnly
}
