package awsutil

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

// CloudTrailEvent represents a parsed mutation event from CloudTrail.
type CloudTrailEvent struct {
	Time      time.Time
	Principal string
	EventName string
	SourceIP  string
}

type rawCTEvent struct {
	SourceIPAddress string `json:"sourceIPAddress"`
	UserIdentity    struct {
		Arn         string `json:"arn"`
		PrincipalID string `json:"principalId"`
	} `json:"userIdentity"`
}

// FetchCloudTrailEvents queries CloudTrail LookupEvents for mutations of a resource.
func FetchCloudTrailEvents(ctx context.Context, cfg aws.Config, region, resourceID string) ([]CloudTrailEvent, error) {
	if resourceID == "" {
		return nil, nil
	}
	ctCfg := cfg.Copy()
	if region != "" && region != "global" {
		ctCfg.Region = region
	}
	client := cloudtrail.NewFromConfig(ctCfg)

	input := &cloudtrail.LookupEventsInput{
		LookupAttributes: []types.LookupAttribute{
			{
				AttributeKey:   types.LookupAttributeKeyResourceName,
				AttributeValue: aws.String(resourceID),
			},
		},
		MaxResults: aws.Int32(20),
	}

	resp, err := client.LookupEvents(ctx, input)
	if err != nil {
		return nil, err
	}

	var events []CloudTrailEvent
	for _, e := range resp.Events {
		srcIP := "-"
		principal := aws.ToString(e.Username)
		if e.CloudTrailEvent != nil {
			var raw rawCTEvent
			if json.Unmarshal([]byte(*e.CloudTrailEvent), &raw) == nil {
				if raw.SourceIPAddress != "" {
					srcIP = raw.SourceIPAddress
				}
				if raw.UserIdentity.Arn != "" {
					principal = raw.UserIdentity.Arn
				} else if raw.UserIdentity.PrincipalID != "" {
					principal = raw.UserIdentity.PrincipalID
				}
			}
		}

		events = append(events, CloudTrailEvent{
			Time:      aws.ToTime(e.EventTime),
			Principal: principal,
			EventName: aws.ToString(e.EventName),
			SourceIP:  srcIP,
		})
	}

	return events, nil
}
