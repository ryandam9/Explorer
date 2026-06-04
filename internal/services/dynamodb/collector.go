package dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

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
	var resources []model.Resource

	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list DynamoDB tables: %w", err)
		}

		for _, tableName := range page.TableNames {
			desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to describe table %s: %w", tableName, err)
			}
			resources = append(resources, c.mapTable(input.Region, desc.Table, input.DetailLevel))
		}
	}

	return resources, nil
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
		res.Details = map[string]interface{}{
			"keySchema":          keySchemas,
			"attributeCount":     len(table.AttributeDefinitions),
			"deletionProtection": aws.ToBool(table.DeletionProtectionEnabled),
		}
	}

	return res
}
