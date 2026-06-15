package gluetui

import (
	"strings"
	"testing"
)

func TestCwJumpArgs(t *testing.T) {
	// Full set: explicit group + stream + region + profile + config.
	got := cwJumpArgs("/aws-glue/jobs/logs-v2", "jr_abc", "us-east-1", "prod", "/etc/aws.yaml")
	want := "cw --group /aws-glue/jobs/logs-v2 --stream jr_abc --region us-east-1 --profile prod --config /etc/aws.yaml"
	if strings.Join(got, " ") != want {
		t.Errorf("args = %q, want %q", strings.Join(got, " "), want)
	}

	// Empty group falls back to the Glue base group; no run ID → no --stream.
	got = cwJumpArgs("", "", "", "", "")
	if strings.Join(got, " ") != "cw --group "+defaultGlueLogGroup {
		t.Errorf("fallback args = %q", strings.Join(got, " "))
	}

	// "global" region is omitted (Glue is regional, but guard like the cw jump).
	got = cwJumpArgs("/aws-glue/jobs", "jr_1", "global", "", "")
	for _, a := range got {
		if a == "--region" {
			t.Errorf("global region should be omitted: %v", got)
		}
	}
}
