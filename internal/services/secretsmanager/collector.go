package secretsmanager

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "secretsmanager"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := secretsmanager.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := secretsmanager.NewListSecretsPaginator(client, &secretsmanager.ListSecretsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list secrets: %w", err)
		}

		for _, secret := range page.SecretList {
			resources = append(resources, c.mapSecret(input.Region, secret, input.DetailLevel))
		}
	}

	return resources, nil
}

func (c *Collector) mapSecret(region string, secret types.SecretListEntry, detail services.DetailLevel) model.Resource {
	id := aws.ToString(secret.ARN)
	name := aws.ToString(secret.Name)

	res := model.Resource{
		Service: "secretsmanager",
		Type:    "secret",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id,
		Summary: map[string]string{
			"kmsKeyId":               aws.ToString(secret.KmsKeyId),
			"rotationEnabled":        fmt.Sprintf("%t", aws.ToBool(secret.RotationEnabled)),
			"secretVersionsToStages": fmt.Sprintf("%d", len(secret.SecretVersionsToStages)),
		},
	}

	if secret.CreatedDate != nil {
		res.CreatedAt = secret.CreatedDate
	}

	if secret.LastRotatedDate != nil {
		res.Summary["lastRotatedDate"] = secret.LastRotatedDate.Format("2006-01-02 15:04:05")
	}

	return res
}
