package route53

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/user/aws_explorer/internal/awsutil"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "route53"
}

func (c *Collector) IsGlobal() bool {
	return true
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := route53.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	var marker *string
	for {
		listInput := &route53.ListHostedZonesInput{
			Marker: marker,
		}
		output, err := client.ListHostedZones(ctx, listInput)
		if err != nil {
			return resources, fmt.Errorf("failed to list Route53 hosted zones: %w", err)
		}

		batch := make([]model.Resource, 0, len(output.HostedZones))
		for _, zone := range output.HostedZones {
			batch = append(batch, c.mapZone(zone, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)

		if !output.IsTruncated {
			break
		}
		marker = output.NextMarker
	}

	return resources, nil
}

func (c *Collector) mapZone(zone types.HostedZone, detail services.DetailLevel) model.Resource {
	id := aws.ToString(zone.Id)
	name := aws.ToString(zone.Name)

	res := model.Resource{
		Service: "route53",
		Type:    "hostedZone",
		Region:  "global",
		ID:      id,
		Name:    name,
		ARN:     awsutil.Route53ZoneARN(id),
		Summary: map[string]string{
			"privateZone": fmt.Sprintf("%t", zone.Config.PrivateZone),
			"recordCount": fmt.Sprintf("%d", aws.ToInt64(zone.ResourceRecordSetCount)),
		},
	}

	if zone.Config.Comment != nil {
		res.Summary["comment"] = aws.ToString(zone.Config.Comment)
	}

	return res
}
