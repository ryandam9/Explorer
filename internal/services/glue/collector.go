// Package glue collects Glue databases, jobs and crawlers. A typed collector is
// needed because the Resource Groups Tagging API only returns tagged resources;
// untagged Glue resources are invisible to the broad discovery sweep.
package glue

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "glue" }

func (c *Collector) IsGlobal() bool { return false }

// Collect gathers databases, jobs and crawlers. They are independent listings,
// so a failure in one is recorded but does not stop the others (partial results
// plus a joined error), matching the best-effort collector contract.
func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := glue.NewFromConfig(input.AWSConfig)
	region, account := input.Region, input.AccountID

	var resources []model.Resource
	var errs []error

	dbs, err := c.collectDatabases(ctx, client, input, region, account)
	resources = input.EmitOrAppend(resources, dbs)
	errs = append(errs, err)

	jobs, err := c.collectJobs(ctx, client, input, region, account)
	resources = input.EmitOrAppend(resources, jobs)
	errs = append(errs, err)

	crawlers, err := c.collectCrawlers(ctx, client, input, region, account)
	resources = input.EmitOrAppend(resources, crawlers)
	errs = append(errs, err)

	return resources, errors.Join(errs...)
}

func (c *Collector) collectDatabases(ctx context.Context, client *glue.Client, _ services.CollectInput, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetDatabases(ctx, &glue.GetDatabasesInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue databases: %w", err)
		}
		for _, db := range page.DatabaseList {
			name := aws.ToString(db.Name)
			res := model.Resource{
				Service: "glue", Type: "database", Region: region,
				ID: name, Name: name, CreatedAt: db.CreateTime,
				ARN: arn(region, account, "database/"+name),
			}
			if d := aws.ToString(db.Description); d != "" {
				res.Summary = map[string]string{"description": d}
			}
			out = append(out, res)
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectJobs(ctx context.Context, client *glue.Client, _ services.CollectInput, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetJobs(ctx, &glue.GetJobsInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue jobs: %w", err)
		}
		for _, j := range page.Jobs {
			name := aws.ToString(j.Name)
			out = append(out, model.Resource{
				Service: "glue", Type: "job", Region: region,
				ID: name, Name: name, CreatedAt: j.CreatedOn,
				ARN: arn(region, account, "job/"+name),
			})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectCrawlers(ctx context.Context, client *glue.Client, _ services.CollectInput, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetCrawlers(ctx, &glue.GetCrawlersInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue crawlers: %w", err)
		}
		for _, cr := range page.Crawlers {
			name := aws.ToString(cr.Name)
			res := model.Resource{
				Service: "glue", Type: "crawler", Region: region,
				ID: name, Name: name, State: string(cr.State),
				ARN: arn(region, account, "crawler/"+name),
			}
			if db := aws.ToString(cr.DatabaseName); db != "" {
				res.Summary = map[string]string{"database": db}
			}
			out = append(out, res)
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

// arn builds a Glue resource ARN. Glue's list APIs return no ARNs, so they are
// constructed to match the form the Tagging API emits so the two merge.
func arn(region, account, resource string) string {
	return fmt.Sprintf("arn:aws:glue:%s:%s:%s", region, account, resource)
}
