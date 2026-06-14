// Package acm collects ACM certificates. A typed collector is needed because
// the Resource Groups Tagging API only returns tagged resources; an untagged
// certificate is invisible to the broad discovery sweep.
package acm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "acm" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := acm.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.ListCertificates(ctx, &acm.ListCertificatesInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to list ACM certificates: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.CertificateSummaryList))
		for _, cert := range page.CertificateSummaryList {
			batch = append(batch, c.mapCertificate(cert, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapCertificate(cert types.CertificateSummary, region string) model.Resource {
	domain := aws.ToString(cert.DomainName)
	res := model.Resource{
		Service:   "acm",
		Type:      "certificate",
		Region:    region,
		ID:        domain,
		Name:      domain,
		ARN:       aws.ToString(cert.CertificateArn),
		State:     string(cert.Status),
		CreatedAt: cert.CreatedAt,
		Summary: map[string]string{
			"type":  string(cert.Type),
			"inUse": fmt.Sprintf("%t", aws.ToBool(cert.InUse)),
		},
	}
	return res
}
