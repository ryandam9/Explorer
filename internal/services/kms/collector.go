// Package kms collects customer-managed KMS keys. A typed collector is needed
// because the Resource Groups Tagging API only returns tagged resources; an
// untagged key is invisible to the broad discovery sweep. AWS-managed keys are
// excluded — they are not user-owned resources and would only add noise.
package kms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "kms" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := kms.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var firstErr error
	var marker *string
	for {
		page, err := client.ListKeys(ctx, &kms.ListKeysInput{Marker: marker})
		if err != nil {
			return resources, fmt.Errorf("failed to list KMS keys: %w", err)
		}
		var batch []model.Resource
		for _, k := range page.Keys {
			// ListKeys returns only IDs; DescribeKey is needed to tell
			// customer-managed keys from AWS-managed ones and to read state.
			out, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: k.KeyId})
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to describe KMS key %s: %w", aws.ToString(k.KeyId), err)
				}
				continue
			}
			md := out.KeyMetadata
			if md == nil || md.KeyManager != types.KeyManagerTypeCustomer {
				continue
			}
			batch = append(batch, c.mapKey(md))
		}
		resources = input.EmitOrAppend(resources, batch)
		if !page.Truncated {
			break
		}
		marker = page.NextMarker
	}
	return resources, firstErr
}

func (c *Collector) mapKey(md *types.KeyMetadata) model.Resource {
	id := aws.ToString(md.KeyId)
	arn := aws.ToString(md.Arn)
	res := model.Resource{
		Service: "kms",
		Type:    "key",
		// KeyMetadata carries no region, but the key ARN's region field is
		// authoritative.
		Region:    regionFromARN(arn),
		ID:        id,
		Name:      id,
		ARN:       arn,
		State:     string(md.KeyState),
		CreatedAt: md.CreationDate,
		Summary:   map[string]string{},
	}
	if d := aws.ToString(md.Description); d != "" {
		res.Summary["description"] = d
	}
	return res
}

// regionFromARN returns the region (4th colon-separated field) of an ARN, or ""
// when the ARN is malformed.
func regionFromARN(arn string) string {
	field, start := 0, 0
	for i := 0; i < len(arn); i++ {
		if arn[i] == ':' {
			if field == 3 {
				return arn[start:i]
			}
			field++
			start = i + 1
		}
	}
	return ""
}
