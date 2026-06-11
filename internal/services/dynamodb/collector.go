package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"golang.org/x/sync/errgroup"

	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

// describeConcurrency bounds parallel DescribeTable calls so large accounts
// don't serialize on per-table round-trips or trip API throttling.
const describeConcurrency = 8

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "dynamodb"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := dynamodb.NewFromConfig(input.AWSConfig)

	var tableNames []string
	var errs []error
	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// Keep the tables listed so far and still describe them below.
			errs = append(errs, fmt.Errorf("failed to list DynamoDB tables: %w", err))
			break
		}
		tableNames = append(tableNames, page.TableNames...)
	}

	// Describe tables concurrently; indexed writes keep list order. A failed
	// describe drops only that table, not the whole region.
	described := make([]*model.Resource, len(tableNames))
	var mu sync.Mutex
	var g errgroup.Group
	g.SetLimit(describeConcurrency)
	for i, tableName := range tableNames {
		g.Go(func() error {
			desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("failed to describe table %s: %w", tableName, err))
				mu.Unlock()
				return nil
			}
			res := c.mapTable(input.Region, desc.Table, input.DetailLevel)
			described[i] = &res
			return nil
		})
	}
	_ = g.Wait()

	resources := make([]model.Resource, 0, len(described))
	for _, r := range described {
		if r != nil {
			resources = append(resources, *r)
		}
	}
	return resources, errors.Join(errs...)
}

func (c *Collector) mapTable(region string, table *types.TableDescription, detail services.DetailLevel) model.Resource {
	id := aws.ToString(table.TableId)
	name := aws.ToString(table.TableName)
	state := string(table.TableStatus)

	billingMode := "provisioned"
	if table.BillingModeSummary != nil {
		billingMode = string(table.BillingModeSummary.BillingMode)
	}

	res := model.Resource{
		Service: "dynamodb",
		Type:    "table",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     aws.ToString(table.TableArn),
		State:   state,
		Summary: map[string]string{
			"billingMode": billingMode,
			"itemCount":   fmt.Sprintf("%d", aws.ToInt64(table.ItemCount)),
			"tableSize":   fmt.Sprintf("%.1f MB", float64(aws.ToInt64(table.TableSizeBytes))/1048576.0),
		},
	}

	if table.CreationDateTime != nil {
		res.CreatedAt = table.CreationDateTime
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		keySchemas := make([]string, 0, len(table.KeySchema))
		for _, ks := range table.KeySchema {
			keySchemas = append(keySchemas, fmt.Sprintf("%s (%s)", aws.ToString(ks.AttributeName), string(ks.KeyType)))
		}
		res.Details = map[string]any{
			"keySchema":          keySchemas,
			"attributeCount":     len(table.AttributeDefinitions),
			"deletionProtection": aws.ToBool(table.DeletionProtectionEnabled),
		}
	}

	return res
}
