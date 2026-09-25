package lambdatui

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
)

type stubInvokeAPI struct {
	urlsErr  error
	asyncErr error
}

func (stubInvokeAPI) ListAliases(context.Context, *lambda.ListAliasesInput, ...func(*lambda.Options)) (*lambda.ListAliasesOutput, error) {
	return &lambda.ListAliasesOutput{Aliases: []lambdatypes.AliasConfiguration{
		{Name: aws.String("live"), FunctionVersion: aws.String("12"),
			RoutingConfig: &lambdatypes.AliasRoutingConfiguration{AdditionalVersionWeights: map[string]float64{"11": 0.1}}},
		{Name: aws.String("beta"), FunctionVersion: aws.String("13")},
	}}, nil
}

func (stubInvokeAPI) ListVersionsByFunction(context.Context, *lambda.ListVersionsByFunctionInput, ...func(*lambda.Options)) (*lambda.ListVersionsByFunctionOutput, error) {
	return &lambda.ListVersionsByFunctionOutput{Versions: []lambdatypes.FunctionConfiguration{
		{Version: aws.String("$LATEST")}, {Version: aws.String("11")}, {Version: aws.String("13"), Description: aws.String("new parser")}, {Version: aws.String("12")},
	}}, nil
}

func (s stubInvokeAPI) ListFunctionUrlConfigs(context.Context, *lambda.ListFunctionUrlConfigsInput, ...func(*lambda.Options)) (*lambda.ListFunctionUrlConfigsOutput, error) {
	if s.urlsErr != nil {
		return nil, s.urlsErr
	}
	return &lambda.ListFunctionUrlConfigsOutput{FunctionUrlConfigs: []lambdatypes.FunctionUrlConfig{
		{FunctionUrl: aws.String("https://abc.lambda-url.us-east-1.on.aws/"), AuthType: lambdatypes.FunctionUrlAuthTypeNone,
			FunctionArn: aws.String("arn:aws:lambda:us-east-1:111122223333:function:orders:live")},
	}}, nil
}

func (stubInvokeAPI) ListProvisionedConcurrencyConfigs(context.Context, *lambda.ListProvisionedConcurrencyConfigsInput, ...func(*lambda.Options)) (*lambda.ListProvisionedConcurrencyConfigsOutput, error) {
	return &lambda.ListProvisionedConcurrencyConfigsOutput{ProvisionedConcurrencyConfigs: []lambdatypes.ProvisionedConcurrencyConfigListItem{
		{FunctionArn: aws.String("arn:aws:lambda:us-east-1:111122223333:function:orders:live"),
			RequestedProvisionedConcurrentExecutions: aws.Int32(5), AllocatedProvisionedConcurrentExecutions: aws.Int32(5),
			Status: lambdatypes.ProvisionedConcurrencyStatusEnumReady},
	}}, nil
}

func (s stubInvokeAPI) GetFunctionEventInvokeConfig(context.Context, *lambda.GetFunctionEventInvokeConfigInput, ...func(*lambda.Options)) (*lambda.GetFunctionEventInvokeConfigOutput, error) {
	if s.asyncErr != nil {
		return nil, s.asyncErr
	}
	return &lambda.GetFunctionEventInvokeConfigOutput{MaximumRetryAttempts: aws.Int32(0),
		DestinationConfig: &lambdatypes.DestinationConfig{OnFailure: &lambdatypes.OnFailure{Destination: aws.String("arn:aws:sqs:us-east-1:111122223333:failed")}}}, nil
}

func TestInvokeDetailPanels(t *testing.T) {
	d := FunctionDetail{Name: "orders", ARN: "arn:aws:lambda:us-east-1:111122223333:function:orders"}
	loadInvokeDetail(context.Background(), stubInvokeAPI{}, "us-east-1", "orders", &d)

	if len(d.Versions) != 3 || d.Versions[0].Version != "13" {
		t.Errorf("versions newest first, $LATEST excluded: %+v", d.Versions)
	}
	v := versionsBody(d)
	for _, want := range []string{"live         → v12 (90%) + v11 (10%) · provisioned 5/5 (ready)", "beta         → v13", "v13", "new parser"} {
		if !strings.Contains(v, want) {
			t.Errorf("versions panel missing %q:\n%s", want, v)
		}
	}
	if u := urlBody(d); !strings.Contains(u, "anyone on the internet") || !strings.Contains(u, dkv("Points at", "live")) {
		t.Errorf("URL panel:\n%s", u)
	}
	a := asyncBody(d)
	if !strings.Contains(a, dkv("Retries", "0")) || !strings.Contains(a, "failed") || !strings.Contains(a, dkv("Max event age", "6h (default)")) {
		t.Errorf("async panel:\n%s", a)
	}
}

// A denied read says so; a missing async config is Lambda's defaults, not an
// error.
func TestInvokeDetailDeniedAndDefaults(t *testing.T) {
	denied := &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "no"}
	notFound := &smithy.GenericAPIError{Code: "ResourceNotFoundException", Message: "none"}
	d := FunctionDetail{Name: "orders"}
	loadInvokeDetail(context.Background(), stubInvokeAPI{urlsErr: denied, asyncErr: notFound}, "us-east-1", "orders", &d)
	if u := urlBody(d); !strings.Contains(u, "Access denied") || !strings.Contains(u, "lambda:ListFunctionUrlConfigs") {
		t.Errorf("denied URL read should say so, not 'no function URL': %q", u)
	}
	if d.Async == nil || !d.Async.Default || !strings.Contains(asyncBody(d), "2 (default)") {
		t.Errorf("no async config means Lambda's defaults: %+v", d.Async)
	}
}

func TestTriggersPanel(t *testing.T) {
	d := FunctionDetail{
		Name: "orders", ARN: "arn:aws:lambda:us-east-1:111122223333:function:orders",
		Triggers: []EventSource{{SourceLabel: "sqs:orders-queue", State: "Enabled", BatchSize: 10}},
		ResourcePolicy: `{"Statement":[
		  {"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"lambda:InvokeFunction",
		   "Condition":{"ArnLike":{"AWS:SourceArn":"arn:aws:s3:::uploads"}}},
		  {"Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"lambda:InvokeFunction"},
		  {"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::444455556666:root"},"Action":"lambda:InvokeFunction"}]}`,
		URLs: []URLInfo{{AuthType: "AWS_IAM"}},
	}
	body := triggersBody(d)
	for _, want := range []string{
		"polls sqs:orders-queue · enabled · batch 10",
		"S3 bucket — uploads  (arn:aws:s3:::uploads)",
		"EventBridge rule — any source — no SourceArn/SourceAccount condition",
		"account 444455556666 (cross-account)",
		"HTTPS function URL (auth AWS_IAM)",
		"need no policy entry",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("triggers panel missing %q:\n%s", want, body)
		}
	}

	// Unknown policy (denied) is said, not rendered as "no triggers".
	d2 := FunctionDetail{ResourcePolicyErr: "Access denied: not permitted to read the resource policy (lambda:GetPolicy)."}
	if b := triggersBody(d2); !strings.Contains(b, "Access denied") || strings.Contains(b, "No event-source mapping") {
		t.Errorf("denied policy:\n%s", b)
	}
}
