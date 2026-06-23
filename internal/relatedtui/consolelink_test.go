package relatedtui

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

func link(id, service, typ, region string) xref.Link {
	return xref.Link{Reference: xref.Reference{Service: service, Type: typ, Region: region, ID: id}}
}

func TestConsoleLinkFor(t *testing.T) {
	// Deep-linkable resource (EC2 instance) → precise console URL.
	if url, kind, ok := consoleLinkFor(model.Resource{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-0abc"}); !ok || kind != "console URL" || url == "" {
		t.Errorf("deep link: got (%q,%q,%v), want a console URL", url, kind, ok)
	}

	// No deep-link builder but an ARN is present → ARN search fallback, still usable.
	arn := "arn:aws:somesvc:us-east-1:111:thing/x"
	if url, kind, ok := consoleLinkFor(model.Resource{Service: "somesvc", Type: "thing", Region: "us-east-1", ID: arn, ARN: arn}); !ok || kind != "console search URL" || url == "" {
		t.Errorf("arn fallback: got (%q,%q,%v), want a console search URL", url, kind, ok)
	}

	// Bare name, unknown service, no ARN → nothing useful.
	if url, _, ok := consoleLinkFor(model.Resource{Service: "somesvc", Type: "thing", ID: "prod-subnets"}); ok || url != "" {
		t.Errorf("no-link case: got (%q,%v), want (\"\",false)", url, ok)
	}
}

func TestResourceOf_SetsARNOnlyForARNs(t *testing.T) {
	mm := &m{}
	// EC2 short id (post-#385 it carries service/type/region) but is not an ARN.
	r := mm.resourceOf(link("subnet-0abc", "ec2", "subnet", "us-east-1"))
	if r.ARN != "" {
		t.Errorf("short id must not be set as ARN: %+v", r)
	}
	r = mm.resourceOf(link("arn:aws:iam::1:role/app", "iam", "role", "global"))
	if r.ARN == "" || !strings.HasPrefix(r.ARN, "arn:") {
		t.Errorf("arn id should populate ARN: %+v", r)
	}
}
