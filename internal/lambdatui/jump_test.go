package lambdatui

import (
	"strings"
	"testing"
)

func TestCWJumpArgs(t *testing.T) {
	got := cwJumpArgs("/aws/lambda/my-fn", "us-west-2", "prod", "/tmp/config.yaml")
	want := []string{"cw", "--group", "/aws/lambda/my-fn", "--region", "us-west-2", "--profile", "prod", "--config", "/tmp/config.yaml"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("cwJumpArgs = %v, want %v", got, want)
	}
}

func TestCWJumpArgsMinimal(t *testing.T) {
	got := cwJumpArgs("/aws/lambda/my-fn", "", "", "")
	want := []string{"cw", "--group", "/aws/lambda/my-fn"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("cwJumpArgs minimal = %v, want %v", got, want)
	}
}

func TestCWJumpArgsSkipsGlobalRegion(t *testing.T) {
	got := cwJumpArgs("/aws/lambda/my-fn", "global", "", "")
	for _, a := range got {
		if a == "--region" {
			t.Error("global region should not be passed as --region")
		}
	}
}
