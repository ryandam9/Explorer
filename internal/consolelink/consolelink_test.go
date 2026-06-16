package consolelink

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestURL_DeepLinks(t *testing.T) {
	cases := []struct {
		name string
		r    model.Resource
		want string // substring the URL must contain
	}{
		{"ec2 instance", model.Resource{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-0abc"},
			"us-east-1.console.aws.amazon.com/ec2/home?region=us-east-1#InstanceDetails:instanceId=i-0abc"},
		{"security group", model.Resource{Service: "ec2", Type: "security-group", Region: "eu-west-1", ID: "sg-1"},
			"eu-west-1.console.aws.amazon.com/ec2/home?region=eu-west-1#SecurityGroup:groupId=sg-1"},
		{"vpc", model.Resource{Service: "ec2", Type: "vpc", Region: "us-east-1", ID: "vpc-1"},
			"/vpc/home?region=us-east-1#VpcDetails:VpcId=vpc-1"},
		{"subnet", model.Resource{Service: "ec2", Type: "subnet", Region: "us-east-1", ID: "subnet-1"},
			"#SubnetDetails:subnetId=subnet-1"},
		{"nat gateway", model.Resource{Service: "ec2", Type: "natgateway", Region: "us-east-1", ID: "nat-1"},
			"#NatGatewayDetails:natGatewayId=nat-1"},
		{"s3 bucket", model.Resource{Service: "s3", Type: "bucket", ID: "my-bucket"},
			"s3.console.aws.amazon.com/s3/buckets/my-bucket"},
		{"lambda", model.Resource{Service: "lambda", Type: "function", Region: "us-east-1", Name: "my-fn"},
			"/lambda/home?region=us-east-1#/functions/my-fn"},
		{"rds instance", model.Resource{Service: "rds", Type: "db", Region: "us-east-1", ID: "mydb"},
			"#database:id=mydb;is-cluster=false"},
		{"dynamodb", model.Resource{Service: "dynamodb", Type: "table", Region: "us-east-1", ID: "orders"},
			"dynamodbv2/home?region=us-east-1#table?name=orders"},
		{"iam role", model.Resource{Service: "iam", Type: "role", ID: "service-role/my-role"},
			"iam/home#/roles/my-role"},
		{"iam policy", model.Resource{Service: "iam", Type: "policy",
			ARN: "arn:aws:iam::123:policy/my-policy"},
			// PathEscape keeps ':' literal in the fragment path (QueryEscape
			// would emit %3A and turn any space into '+').
			"iam/home#/policies/details/arn:aws:iam::123:policy"},
		{"eks", model.Resource{Service: "eks", Type: "cluster", Region: "us-east-1", ID: "prod"},
			"/eks/home?region=us-east-1#/clusters/prod"},
		{"ecs cluster", model.Resource{Service: "ecs", Type: "cluster", Region: "us-east-1", ID: "prod"},
			"/ecs/v2/clusters/prod?region=us-east-1"},
		{"sns topic", model.Resource{Service: "sns", Type: "topic", Region: "us-east-1",
			ARN: "arn:aws:sns:us-east-1:123:alerts"},
			"#/topic/arn%3Aaws%3Asns%3Aus-east-1%3A123%3Aalerts"},
		{"cw alarm", model.Resource{Service: "cloudwatch", Type: "alarm", Region: "us-east-1", ID: "high-cpu"},
			"#alarmsV2:alarm/high-cpu"},
		{"route53 zone", model.Resource{Service: "route53", Type: "hosted-zone", ID: "Z123"},
			"route53/v2/hostedzones#ListRecordSets/Z123"},
		{"emr", model.Resource{Service: "emr", Type: "cluster", Region: "us-east-1", ID: "j-ABC"},
			"/emr/home?region=us-east-1#/clusterDetails/j-ABC"},
		{"glue job", model.Resource{Service: "glue", Type: "job", Region: "us-east-1", ID: "nightly-etl"},
			"gluestudio/home?region=us-east-1#/editor/job/nightly-etl/details"},
		{"glue crawler", model.Resource{Service: "glue", Type: "crawler", Region: "eu-west-1", ID: "orders-crawler"},
			"/glue/home?region=eu-west-1#/v2/data-catalog/crawlers/view/orders-crawler"},
		{"glue database", model.Resource{Service: "glue", Type: "database", Region: "us-east-1", ID: "sales"},
			"#/v2/data-catalog/databases/view/sales"},
		{"glue workflow", model.Resource{Service: "glue", Type: "workflow", Region: "us-east-1", ID: "orders-wf"},
			"#/v2/etl-configuration/workflows/view/orders-wf"},
	}
	for _, c := range cases {
		got, specific := URL(c.r)
		if !specific {
			t.Errorf("%s: expected a specific deep link, got fallback %q", c.name, got)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: URL = %q, want substring %q", c.name, got, c.want)
		}
	}
}

func TestURL_FromARNOnly(t *testing.T) {
	// A Tagging-API-style entry: ARN only, no typed fields.
	got, specific := FromARN("arn:aws:ec2:eu-west-1:123456789012:instance/i-0abc")
	if !specific {
		t.Fatalf("expected deep link, got %q", got)
	}
	if !strings.Contains(got, "eu-west-1.console.aws.amazon.com/ec2/home?region=eu-west-1#InstanceDetails:instanceId=i-0abc") {
		t.Errorf("URL = %q", got)
	}
}

func TestURL_SQSNeedsAccountFromARN(t *testing.T) {
	got, specific := FromARN("arn:aws:sqs:us-east-1:123456789012:my-queue")
	if !specific {
		t.Fatalf("expected deep link, got %q", got)
	}
	if !strings.Contains(got, "sqs/v3/home?region=us-east-1#/queues/") ||
		!strings.Contains(got, "https%3A%2F%2Fsqs.us-east-1.amazonaws.com%2F123456789012%2Fmy-queue") {
		t.Errorf("URL = %q", got)
	}
}

func TestURL_ELBv2UsesARNSearch(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/web/50dc6c"
	got, specific := FromARN(arn)
	if !specific || !strings.Contains(got, "#LoadBalancers:search=") {
		t.Errorf("URL = %q specific=%v", got, specific)
	}
	tg := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/web/abc"
	got, _ = FromARN(tg)
	if !strings.Contains(got, "#TargetGroups:search=") {
		t.Errorf("target group URL = %q", got)
	}
}

func TestURL_LogGroupDoubleEncoding(t *testing.T) {
	got, specific := FromARN("arn:aws:logs:us-east-1:123:log-group:/aws/lambda/my-fn")
	if !specific {
		t.Fatalf("expected deep link, got %q", got)
	}
	if !strings.Contains(got, "#logsV2:log-groups/log-group/$252Faws$252Flambda$252Fmy-fn") {
		t.Errorf("URL = %q", got)
	}
}

func TestURL_UnknownTypeFallsBackToARNSearch(t *testing.T) {
	arn := "arn:aws:kafka:us-east-1:123:cluster/my-cluster/uuid"
	got, specific := FromARN(arn)
	if specific {
		t.Fatalf("expected fallback for unknown service, got %q", got)
	}
	if !strings.Contains(got, "console.aws.amazon.com/go/view?arn=arn%3Aaws%3Akafka") {
		t.Errorf("fallback URL = %q", got)
	}
}

func TestURL_NoARNNoMatchFallsBackToConsoleHome(t *testing.T) {
	got, specific := URL(model.Resource{Service: "mystery", Region: "eu-west-1", ID: "x"})
	if specific {
		t.Fatalf("expected fallback, got %q", got)
	}
	if !strings.Contains(got, "eu-west-1.console.aws.amazon.com/console/home?region=eu-west-1") {
		t.Errorf("fallback URL = %q", got)
	}
}

func TestURL_GlobalRegionDefaults(t *testing.T) {
	got, _ := URL(model.Resource{Service: "ec2", Type: "instance", Region: "global", ID: "i-1"})
	if !strings.Contains(got, "us-east-1") {
		t.Errorf("global region did not default: %q", got)
	}
}
