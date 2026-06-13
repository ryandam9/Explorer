package loggroup

import "testing"

func TestFor(t *testing.T) {
	cases := []struct {
		name   string
		res    Resource
		want   string
		wantOK bool
	}{
		{"lambda by name", Resource{Service: "lambda", Type: "function", Name: "checkout", ID: "arn:aws:lambda:…:checkout"}, "/aws/lambda/checkout", true},
		{"lambda no name", Resource{Service: "lambda", Type: "function"}, "", false},
		{"rds prefix by name", Resource{Service: "rds", Type: "db-instance", Name: "prod-db", ID: "prod-db"}, "/aws/rds/instance/prod-db/", true},
		{"rds falls back to id", Resource{Service: "rds", Type: "db-instance", ID: "prod-db"}, "/aws/rds/instance/prod-db/", true},
		{"eks cluster", Resource{Service: "eks", Type: "cluster", Name: "prod"}, "/aws/eks/prod/cluster", true},
		{"ecs not derivable", Resource{Service: "ecs", Type: "service", Name: "web"}, "", false},
		{"unknown service", Resource{Service: "s3", Type: "bucket", Name: "b"}, "", false},
		{"case-insensitive service", Resource{Service: "Lambda", Name: "fn"}, "/aws/lambda/fn", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := For(c.res)
			if got != c.want || ok != c.wantOK {
				t.Errorf("For(%+v) = (%q, %v), want (%q, %v)", c.res, got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestSupported(t *testing.T) {
	for _, s := range []string{"lambda", "rds", "eks", "EKS"} {
		if !Supported(s) {
			t.Errorf("Supported(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"ecs", "s3", "ec2", ""} {
		if Supported(s) {
			t.Errorf("Supported(%q) = true, want false", s)
		}
	}
}
