package tui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestRelatedTarget(t *testing.T) {
	cases := []struct {
		res  model.Resource
		want string
	}{
		{model.Resource{ARN: "arn:aws:lambda:us-east-1:1:function:f", ID: "f"}, "arn:aws:lambda:us-east-1:1:function:f"}, // ARN preferred
		{model.Resource{ID: "sg-0abc"}, "sg-0abc"}, // fall back to ID
		{model.Resource{}, ""},                     // nothing to look up
	}
	for _, c := range cases {
		if got := relatedTarget(c.res); got != c.want {
			t.Errorf("relatedTarget(%+v) = %q, want %q", c.res, got, c.want)
		}
	}
}
