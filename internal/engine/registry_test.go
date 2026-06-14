package engine

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/summary"
)

// summaryServices are the services whose typed collectors must surface in the
// Summary screen. A typed collector gives complete, tag-independent coverage, so
// its resources always appear regardless of how they're tagged. This list is the
// contract: each entry must be registered in defaultRegistry and recognized as a
// typed (not merely tag-discovered) service by the summary coverage catalog.
var summaryServices = []string{
	"cloudwatch",
	"efs",
	"eks",
	"elbv2",
	"emr",
	"elasticache",
	"kms",
	"kinesis",
	"rds",
	"redshift",
	"route53",
	"sns",
	"sqs",
	"stepfunctions",
}

// TestDefaultRegistryRegistersSummaryServices guards that every service expected
// in the Summary screen has a typed collector wired into the registry. Without
// registration its resources would only show up when tagged (via the tag sweep),
// which is exactly the gap a typed collector closes.
func TestDefaultRegistryRegistersSummaryServices(t *testing.T) {
	registered := make(map[string]bool)
	for _, c := range defaultRegistry().GetAll() {
		registered[c.Name()] = true
	}

	for _, name := range summaryServices {
		if !registered[name] {
			t.Errorf("service %q has no registered typed collector; its resources would only appear in Summary when tagged", name)
		}
	}
}

// TestSummaryCoverageMarksServicesTyped checks the other half of the contract:
// the summary coverage catalog must classify each of these services as typed
// when the registry's collectors are passed in. That classification is what tells
// a user the service's inventory is complete rather than tag-limited.
func TestSummaryCoverageMarksServicesTyped(t *testing.T) {
	typedServices := make([]string, 0)
	for _, c := range defaultRegistry().GetAll() {
		typedServices = append(typedServices, c.Name())
	}

	cov := summary.Coverage(nil, typedServices, nil, nil)
	byKey := make(map[string]summary.ServiceCoverage, len(cov))
	for _, c := range cov {
		byKey[c.Key] = c
	}

	for _, name := range summaryServices {
		c, ok := byKey[name]
		if !ok {
			t.Errorf("service %q is missing from the summary coverage catalog", name)
			continue
		}
		if !c.Typed {
			t.Errorf("service %q is in the catalog but not marked typed; it would be reported as tag-discovered only", name)
		}
	}
}
