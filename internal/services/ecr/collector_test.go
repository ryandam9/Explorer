package ecr

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "ecr" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapRepository(t *testing.T) {
	res := NewCollector().mapRepository(types.Repository{
		RepositoryName: aws.String("app"),
		RepositoryArn:  aws.String("arn:aws:ecr:us-east-1:1:repository/app"),
		RepositoryUri:  aws.String("1.dkr.ecr.us-east-1.amazonaws.com/app"),
	}, "us-east-1")
	if res.Service != "ecr" || res.Type != "repository" || res.Name != "app" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.Summary["uri"] == "" {
		t.Error("uri summary should be set")
	}
}
