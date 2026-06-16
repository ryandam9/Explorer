// Package kms collects customer-managed KMS keys. A typed collector is needed
// because the Resource Groups Tagging API only returns tagged resources; an
// untagged key is invisible to the broad discovery sweep. AWS-managed keys are
// excluded — they are not user-owned resources and would only add noise.
package kms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// describeConcurrency bounds the in-flight DescribeKey calls per page so large
// accounts don't issue hundreds of serial round-trips.
const describeConcurrency = 10

// kmsAPI is the subset of the KMS client used by the collector, extracted so
// the describe/alias behavior can be exercised with a fake in tests.
type kmsAPI interface {
	kms.ListKeysAPIClient
	kms.ListAliasesAPIClient
	DescribeKey(context.Context, *kms.DescribeKeyInput, ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
}

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "kms" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	return c.collect(ctx, kms.NewFromConfig(input.AWSConfig), input)
}

func (c *Collector) collect(ctx context.Context, client kmsAPI, input services.CollectInput) ([]model.Resource, error) {
	var resources []model.Resource
	var errs []error

	// Aliases give keys human-readable names. They're enrichment: a failure
	// here is recorded but must not stop key collection.
	aliases, err := listAliases(ctx, client)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to list KMS aliases: %w", err))
	}

	// describePage resolves each key's metadata concurrently (bounded), keeping
	// the keys that describe successfully and recording per-key failures so one
	// denied key doesn't drop the rest.
	describePage := func(keys []types.KeyListEntry) ([]model.Resource, []error) {
		described := make([]*model.Resource, len(keys))
		var mu sync.Mutex
		var describeErrs []error
		var g errgroup.Group
		g.SetLimit(describeConcurrency)
		for i, k := range keys {
			i, k := i, k
			g.Go(func() error {
				// ListKeys returns only IDs; DescribeKey is needed to tell
				// customer-managed keys from AWS-managed ones and to read state.
				out, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: k.KeyId})
				if err != nil {
					mu.Lock()
					describeErrs = append(describeErrs, fmt.Errorf("failed to describe KMS key %s: %w", aws.ToString(k.KeyId), err))
					mu.Unlock()
					return nil
				}
				md := out.KeyMetadata
				if md == nil || md.KeyManager != types.KeyManagerTypeCustomer {
					return nil
				}
				res := c.mapKey(md, aliases[aws.ToString(md.KeyId)])
				described[i] = &res
				return nil
			})
		}
		_ = g.Wait()

		batch := make([]model.Resource, 0, len(described))
		for _, r := range described {
			if r != nil {
				batch = append(batch, *r)
			}
		}
		return batch, describeErrs
	}

	paginator := kms.NewListKeysPaginator(client, &kms.ListKeysInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// Keep everything described from earlier pages.
			errs = append(errs, fmt.Errorf("failed to list KMS keys: %w", err))
			break
		}
		batch, describeErrs := describePage(page.Keys)
		errs = append(errs, describeErrs...)
		resources = input.EmitOrAppend(resources, batch)
	}
	return resources, errors.Join(errs...)
}

// listAliases maps each target key ID to its alias name(s). Aliases without a
// target key (rare) are ignored.
func listAliases(ctx context.Context, client kmsAPI) (map[string][]string, error) {
	out := make(map[string][]string)
	paginator := kms.NewListAliasesPaginator(client, &kms.ListAliasesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return out, err
		}
		for _, a := range page.Aliases {
			key := aws.ToString(a.TargetKeyId)
			if key == "" {
				continue
			}
			out[key] = append(out[key], aws.ToString(a.AliasName))
		}
	}
	return out, nil
}

func (c *Collector) mapKey(md *types.KeyMetadata, aliases []string) model.Resource {
	id := aws.ToString(md.KeyId)
	arn := aws.ToString(md.Arn)

	// A bare key ID is opaque; prefer the first alias as the display name.
	name := id
	if len(aliases) > 0 {
		name = aliases[0]
	}

	res := model.Resource{
		Service: "kms",
		Type:    "key",
		// KeyMetadata carries no region, but the key ARN's region field is
		// authoritative.
		Region:    regionFromARN(arn),
		ID:        id,
		Name:      name,
		ARN:       arn,
		State:     string(md.KeyState),
		CreatedAt: md.CreationDate,
		Summary:   map[string]string{},
	}
	if len(aliases) > 0 {
		res.Summary["aliases"] = strings.Join(aliases, ", ")
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
