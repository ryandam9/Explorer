package auth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// TestVerify_SurfacesBuildError confirms Verify collapses a config-build
// failure into its single error, without ever reaching the network — so a
// misconfigured auth method is reported as "cannot authenticate" up front.
func TestVerify_SurfacesBuildError(t *testing.T) {
	clearAWSEnv(t)
	_, err := Verify(context.Background(), &config.AWSConfig{AuthMethod: "magic"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), `unknown authMethod "magic"`) {
		t.Fatalf("expected build error to surface, got %v", err)
	}
}

// callerIdentityXML is a canned, valid sts:GetCallerIdentity response.
const callerIdentityXML = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>arn:aws:iam::123456789012:user/test</Arn>
    <UserId>AIDAEXAMPLE</UserId>
    <Account>123456789012</Account>
  </GetCallerIdentityResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`

// stubHTTP returns the canned identity response for any request, recording the
// URL host so the test can assert which endpoint the SDK resolved.
type stubHTTP struct{ host string }

func (s *stubHTTP) Do(req *http.Request) (*http.Response, error) {
	s.host = req.URL.Host
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/xml"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(callerIdentityXML))),
	}, nil
}

// TestVerifyCallerIdentity_PinsRegionWhenEmpty confirms the verification call
// defaults to us-east-1 when the config carries no region, so the STS endpoint
// resolves and authentication is actually attempted (rather than failing with
// an endpoint-resolution error before the request is ever sent). It runs fully
// offline against a stubbed HTTP client.
func TestVerifyCallerIdentity_PinsRegionWhenEmpty(t *testing.T) {
	clearAWSEnv(t)

	stub := &stubHTTP{}
	cfg := aws.Config{
		HTTPClient:  stub,
		Credentials: credentialsStub{},
		// Region deliberately left empty: VerifyCallerIdentity must fill it in.
	}

	id, err := VerifyCallerIdentity(context.Background(), cfg)
	if err != nil {
		t.Fatalf("VerifyCallerIdentity: %v", err)
	}
	if id.Account != "123456789012" {
		t.Errorf("Account = %q, want 123456789012", id.Account)
	}
	if !strings.Contains(stub.host, "us-east-1") && stub.host != "sts.amazonaws.com" {
		t.Errorf("request went to %q; expected the defaulted us-east-1 STS endpoint", stub.host)
	}
}

// credentialsStub is a minimal static credentials provider so the SDK proceeds
// past credential resolution without touching real credentials.
type credentialsStub struct{}

func (credentialsStub) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		Source:          "credentialsStub",
	}, nil
}
