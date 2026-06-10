package discovery

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	rgttypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
)

func TestMapResource_RegionalWithNameTag(t *testing.T) {
	m := rgttypes.ResourceTagMapping{
		ResourceARN: aws.String("arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0abc"),
		Tags: []rgttypes.Tag{
			{Key: aws.String("Name"), Value: aws.String("public-a")},
		},
	}

	r, ok := mapResource("us-east-1", m)
	if !ok {
		t.Fatal("mapResource returned ok=false")
	}
	if r.Service != "ec2" || r.Type != "subnet" {
		t.Errorf("service/type = %q/%q, want ec2/subnet", r.Service, r.Type)
	}
	if r.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", r.Region)
	}
	if r.Name != "public-a" {
		t.Errorf("Name = %q, want public-a (from Name tag)", r.Name)
	}
	if r.ID != "subnet-0abc" {
		t.Errorf("ID = %q, want subnet-0abc", r.ID)
	}
}

func TestMapResource_GlobalNoNameTag(t *testing.T) {
	// IAM ARNs carry no region; the resource falls back to "global" and the
	// name is derived from the ARN's resource id.
	m := rgttypes.ResourceTagMapping{
		ResourceARN: aws.String("arn:aws:iam::123456789012:role/my-app-role"),
	}

	r, ok := mapResource("us-east-1", m)
	if !ok {
		t.Fatal("mapResource returned ok=false")
	}
	if r.Region != "global" {
		t.Errorf("Region = %q, want global", r.Region)
	}
	if r.Name != "my-app-role" {
		t.Errorf("Name = %q, want my-app-role (from ARN)", r.Name)
	}
}

func TestMapResource_BadARN(t *testing.T) {
	if _, ok := mapResource("us-east-1", rgttypes.ResourceTagMapping{ResourceARN: aws.String("garbage")}); ok {
		t.Error("expected ok=false for unparseable ARN")
	}
}
