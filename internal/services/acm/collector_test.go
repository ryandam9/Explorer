package acm

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "acm" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapCertificate(t *testing.T) {
	res := NewCollector().mapCertificate(types.CertificateSummary{
		CertificateArn: aws.String("arn:aws:acm:us-east-1:1:certificate/abc"),
		DomainName:     aws.String("example.com"),
		Status:         types.CertificateStatusIssued,
		Type:           types.CertificateTypeAmazonIssued,
		InUse:          aws.Bool(true),
	}, "us-east-1")
	if res.Service != "acm" || res.Type != "certificate" || res.Name != "example.com" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.State != "ISSUED" || res.Summary["inUse"] != "true" {
		t.Errorf("unexpected state/inUse: %+v", res.Summary)
	}
}
