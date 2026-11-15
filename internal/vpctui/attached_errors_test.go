package vpctui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// eksStub serves a minimal EKS REST-JSON API: ListClusters returns two
// clusters; "good" describes into vpc-1, "bad" fails with a 500. With
// denyList set, ListClusters itself is denied.
func eksStub(t *testing.T, denyList bool) *VPCClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/clusters":
			if denyList {
				w.Header().Set("X-Amzn-Errortype", "AccessDeniedException")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"not authorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"clusters":["good","bad"]}`))
		case "/clusters/good":
			_, _ = w.Write([]byte(`{"cluster":{"name":"good","status":"ACTIVE","resourcesVpcConfig":{"vpcId":"vpc-1"}}}`))
		default:
			w.Header().Set("X-Amzn-Errortype", "ServerException")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := eks.New(eks.Options{
		Region:           "us-east-1",
		BaseEndpoint:     aws.String(srv.URL),
		Credentials:      credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		RetryMaxAttempts: 1,
	})
	return &VPCClient{eks: client, ctx: context.Background()}
}

// A denied listing must surface as an error so the export flags it as partial
// data, not come back as an empty "nothing attached" result.
func TestListEKSClusters_DeniedIsAnError(t *testing.T) {
	c := eksStub(t, true)
	got, err := c.ListEKSClusters("vpc-1")
	if err == nil {
		t.Fatalf("expected the denied ListClusters to return an error, got %v, nil", got)
	}
}

// One cluster failing to describe keeps the others and reports the failure.
func TestListEKSClusters_DescribeFailureIsPartial(t *testing.T) {
	c := eksStub(t, false)
	got, err := c.ListEKSClusters("vpc-1")
	if err == nil {
		t.Fatal("expected an error for the cluster that failed to describe")
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("expected the described cluster to be kept, got %+v", got)
	}
}
